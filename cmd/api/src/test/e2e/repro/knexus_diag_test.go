//go:build e2e

// Diagnostic test for the k-nexus-global extra-nodes investigation.
// Ingests the k-nexusglobal dataset into a fresh kglite graph, runs analysis,
// and prints per-label counts, duplicate objectid counts, and breakdown of
// duplicates by label combination. This narrows down where the 706 extra nodes
// come from.
//
// Run with:
//
//	go test -v -tags e2e -timeout 15m -run TestKNexusDiagnostic ./cmd/api/src/test/e2e/repro/
package repro

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/specterops/bloodhound/cmd/api/src/analysis/ad"
	"github.com/specterops/bloodhound/cmd/api/src/analysis/azure"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/bloodhound/cmd/api/src/services/graphify"
	"github.com/specterops/bloodhound/cmd/api/src/services/graphify/endpoint"
	"github.com/specterops/bloodhound/cmd/api/src/services/upload"
	"github.com/specterops/bloodhound/packages/go/analysis"
	"github.com/specterops/bloodhound/packages/go/bomenc"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

// knexusDataPath returns the path to the k-nexus zip.
func knexusDataPath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata", "k-nexusglobal_sampledata.zip")
}

// ingestZipFull ingests the k-nexus zip correctly — extracts each entry to a
// temp file WITHOUT closing the zip entry reader before copying, then calls
// ReadFileForIngest on the temp file. Non-fatal per-file errors are logged.
// This mirrors what ingest_test.go's doIngestZip does.
func ingestZipFull(ctx context.Context, t *testing.T, db graph.Database, zipPath string) {
	t.Helper()

	schema, err := upload.LoadIngestSchema()
	require.NoError(t, err)

	resolver := endpoint.NewResolver(db)
	ic := graphify.NewIngestContext(ctx,
		graphify.WithIngestTime(time.Now().UTC()),
		graphify.WithEndpointResolver(resolver),
	)
	readOpts := graphify.ReadOptions{
		FileType:     model.FileTypeZip,
		IngestSchema: schema,
		RegisterSourceKind: func(kind graph.Kind) error {
			return db.RefreshKinds(ctx)
		},
	}

	batchErr := db.BatchOperation(ctx, func(batch graph.Batch) error {
		ic.BindBatchUpdater(batch)

		archive, err := zip.OpenReader(zipPath)
		if err != nil {
			return err
		}
		defer archive.Close()

		for _, f := range archive.File {
			if f.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(f.Name), ".json") {
				continue
			}
			if err := ingestZipEntry(ic, f, readOpts); err != nil {
				// Per-file errors are non-fatal — log and continue.
				slog.Warn("skip zip entry during ingest", "file", f.Name, "error", err)
			}
		}
		return nil
	})
	if batchErr != nil {
		t.Logf("WARN: batch error during k-nexus ingest (non-fatal): %v", batchErr)
	}
}

// ingestZipEntry extracts a single zip entry to a temp file and calls
// ReadFileForIngest. The zip entry reader is NOT closed before copying,
// ensuring the BOM normalizer can read it correctly.
func ingestZipEntry(ic *graphify.IngestContext, f *zip.File, readOpts graphify.ReadOptions) error {
	src, err := f.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %s: %w", f.Name, err)
	}
	defer src.Close() // close AFTER we're done reading

	normalized, err := bomenc.NormalizeToUTF8(src)
	if err != nil {
		return fmt.Errorf("normalize %s: %w", f.Name, err)
	}

	tmp, err := os.CreateTemp("", "knexus-diag-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", f.Name, err)
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	if _, err := io.Copy(tmp, normalized); err != nil {
		return fmt.Errorf("copy %s to temp: %w", f.Name, err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek temp for %s: %w", f.Name, err)
	}

	return graphify.ReadFileForIngest(ic, tmp, readOpts)
}

// runKNexusAnalysis runs AD and Azure post-processing on the graph.
// When BH_SKIP_ANALYSIS=1 is set, both phases are bypassed.
func runKNexusAnalysis(ctx context.Context, t *testing.T, db graph.Database) {
	t.Helper()
	if os.Getenv("BH_SKIP_ANALYSIS") == "1" {
		t.Log("BH_SKIP_ANALYSIS=1: skipping AD and Azure post-processing")
		return
	}
	counter := analysis.NewCompositionCounter()
	_, err := ad.Post(ctx, db, true, false, true, &counter)
	require.NoError(t, err, "AD post-processing")
	_, err = azure.Post(ctx, db)
	require.NoError(t, err, "Azure post-processing")
}

// TestKNexusDiagnostic ingests the k-nexus dataset, runs analysis, and prints
// diagnostic information about the node/relationship counts and any duplicates.
func TestKNexusDiagnostic(t *testing.T) {
	ctx := context.Background()
	zipPath := knexusDataPath()
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		t.Skipf("k-nexus zip not found: %s", zipPath)
	}

	t.Log("=== Ingesting k-nexus-global dataset ===")
	db := openGraph(t)
	ingestZipFull(ctx, t, db, zipPath)

	preAnalysisNodes := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
	preAnalysisRels := queryCount(ctx, t, db, "MATCH ()-[r]->() RETURN count(r)")
	t.Logf("PRE-ANALYSIS: nodes=%d, relationships=%d", preAnalysisNodes, preAnalysisRels)

	t.Log("=== Running AD + Azure analysis ===")
	runKNexusAnalysis(ctx, t, db)

	t.Log("")
	t.Log("=== DIAGNOSTIC: Total counts ===")
	totalNodes := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
	totalRels := queryCount(ctx, t, db, "MATCH ()-[r]->() RETURN count(r)")
	t.Logf("  Total nodes:         %d  (golden=11203, delta=%+d)", totalNodes, totalNodes-11203)
	t.Logf("  Total relationships: %d  (golden=61746, delta=%+d)", totalRels, totalRels-61746)

	t.Log("")
	t.Log("=== DIAGNOSTIC: Per-label node counts (via UNWIND labels) ===")
	labelRows := queryString(ctx, t, db,
		"MATCH (n) UNWIND labels(n) AS lbl RETURN lbl, count(*) AS c ORDER BY c DESC")
	for _, line := range strings.Split(labelRows, "\n") {
		t.Logf("  %s", line)
	}

	t.Log("")
	t.Log("=== DIAGNOSTIC: Nodes without objectid ===")
	noOidCount := queryCount(ctx, t, db, "MATCH (n) WHERE n.objectid IS NULL RETURN count(n)")
	t.Logf("  Nodes without objectid: %d", noOidCount)
	if noOidCount > 0 {
		noOidLabels := queryString(ctx, t, db,
			"MATCH (n) WHERE n.objectid IS NULL UNWIND labels(n) AS lbl RETURN lbl, count(*) AS c ORDER BY c DESC")
		for _, line := range strings.Split(noOidLabels, "\n") {
			t.Logf("    label: %s", line)
		}
	}

	t.Log("")
	t.Log("=== DIAGNOSTIC: Duplicate objectids (same objectid on >1 node) ===")
	dupeOids := queryCount(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL WITH n.objectid AS oid, count(n) AS cnt WHERE cnt > 1 RETURN count(oid)")
	extraFromDupes := queryCount(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL WITH n.objectid AS oid, count(n) AS cnt WHERE cnt > 1 RETURN sum(cnt) - count(oid)")
	t.Logf("  Objectids appearing on >1 node: %d", dupeOids)
	t.Logf("  Extra nodes from duplicates:    %d", extraFromDupes)

	t.Log("")
	t.Log("=== DIAGNOSTIC: Distribution of nodes-per-objectid ===")
	dist := queryString(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL WITH n.objectid AS oid, count(n) AS cnt RETURN cnt AS nodes_per_oid, count(oid) AS num_oids ORDER BY cnt")
	for _, line := range strings.Split(dist, "\n") {
		t.Logf("  %s", line)
	}

	t.Log("")
	t.Log("=== DIAGNOSTIC: Top duplicate objectids with label sets ===")
	dupeOidRows := queryString(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL WITH n.objectid AS oid, count(n) AS cnt WHERE cnt > 1 RETURN oid, cnt ORDER BY cnt DESC LIMIT 20")
	t.Log("  (up to 20 most-duplicated objectids, with labels of each physical node)")
	for _, line := range strings.Split(dupeOidRows, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ", ", 2)
		if len(parts) < 2 {
			continue
		}
		oid := parts[0]
		cnt := parts[1]
		t.Logf("  OID=%s  cnt=%s", oid, cnt)
		labelRows2 := queryString(ctx, t, db,
			fmt.Sprintf("MATCH (n) WHERE n.objectid = '%s' RETURN labels(n)", oid))
		for _, lr := range strings.Split(labelRows2, "\n") {
			t.Logf("    labels: %s", lr)
		}
	}

	t.Log("")
	t.Log("=== DIAGNOSTIC: Relationship type inventory ===")
	relTypes := queryString(ctx, t, db,
		"MATCH ()-[r]->() RETURN DISTINCT type(r) AS rel_type ORDER BY rel_type")
	for _, line := range strings.Split(relTypes, "\n") {
		t.Logf("  %s", line)
	}

	// Sanity check: we must have ingested data
	require.Greater(t, totalNodes, 0, "should have ingested some nodes")
	require.Greater(t, totalRels, 0, "should have ingested some relationships")

	if totalNodes > 11203 {
		t.Logf("ATTENTION: kglite has %d extra nodes vs golden (kglite=%d, golden=11203)",
			totalNodes-11203, totalNodes)
	}
}

// TestSecondaryLabelMergeRepro is a minimal reproducer for the multi-label MERGE
// divergence: IngestNode is called with identityKind=A and Kinds=[A, B], then a
// later MERGE (:B {objectid:X}) is issued. Neo4j keeps a single node with both
// labels; kglite creates two nodes if the secondary label B isn't visible to
// :B-label MERGE matching.
//
// Run with:
//
//	go test -v -tags e2e -timeout 1m -run TestSecondaryLabelMergeRepro ./cmd/api/src/test/e2e/repro/
func TestSecondaryLabelMergeRepro(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Phase 1: IngestNode with identityKind=Foo, Kinds=[Foo, Bar]. This is what
	// BloodHound's graphify layer does for OpenGraph nodes that carry secondary
	// kinds in the input file (e.g. GH_ExternalIdentity nodes that should also
	// be labeled Base).
	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		props := graph.NewProperties()
		props.Set("objectid", "OID-1")
		props.Set("name", "alice")
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node: &graph.Node{
				Kinds:      graph.Kinds{graph.StringKind("Foo"), graph.StringKind("Bar")},
				Properties: props,
			},
			IdentityKind:       graph.StringKind("Foo"),
			IdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err, "Phase 1 IngestNode")

	count1 := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
	t.Logf("After Phase 1 (IngestNode Foo+Bar): %d nodes", count1)
	labels1 := queryString(ctx, t, db, "MATCH (n {objectid: 'OID-1'}) RETURN labels(n)")
	t.Logf("  Phase 1 labels for OID-1:\n%s", labels1)

	// Phase 2: emit a MERGE (n:Bar {objectid:'OID-1'}) directly via Cypher to
	// model the kind of MERGE statement an UpdateRelationshipBy slow-path issues
	// when an edge endpoint's identityKind=Bar references this node.
	err = db.BatchOperation(ctx, func(batch graph.Batch) error {
		// Use UpdateRelationshipBy with start=Bar, end=existing - we'll model
		// just the start MERGE via a relationship to a separate node.
		props := graph.NewProperties()
		props.Set("objectid", "OID-1")
		startNode := &graph.Node{
			Kinds:      graph.Kinds{graph.StringKind("Bar")},
			Properties: props,
		}
		endProps := graph.NewProperties()
		endProps.Set("objectid", "OID-end")
		endNode := &graph.Node{
			Kinds:      graph.Kinds{graph.StringKind("Other")},
			Properties: endProps,
		}
		relProps := graph.NewProperties()
		rel := &graph.Relationship{
			Kind:       graph.StringKind("Touches"),
			Properties: relProps,
		}
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Start:                   startNode,
			StartIdentityKind:       graph.StringKind("Bar"),
			StartIdentityProperties: []string{"objectid"},
			End:                     endNode,
			EndIdentityKind:         graph.StringKind("Other"),
			EndIdentityProperties:   []string{"objectid"},
			Relationship:            rel,
			IdentityProperties:      []string{},
		})
	})
	require.NoError(t, err, "Phase 2 UpdateRelationshipBy with start=Bar")

	count2 := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
	t.Logf("After Phase 2 (rel start=Bar refs OID-1): %d nodes (delta=%+d)", count2, count2-count1)
	labels2 := queryString(ctx, t, db, "MATCH (n {objectid: 'OID-1'}) RETURN labels(n)")
	t.Logf("  Phase 2 labels for OID-1:\n%s", labels2)

	// Distribution of OID-1 copies
	dist := queryString(ctx, t, db, "MATCH (n {objectid: 'OID-1'}) RETURN count(n)")
	t.Logf("  OID-1 node count: %s", strings.TrimSpace(dist))

	// THE KEY ASSERTION: there must still be exactly one node for OID-1 with
	// both Foo and Bar labels. If kglite splits, we'll see 2 nodes here.
	if strings.TrimSpace(dist) != "1" {
		t.Errorf("BUG: OID-1 has %s copies; expected 1 (Neo4j keeps it as a single Foo+Bar node)", strings.TrimSpace(dist))
	}
}

// TestKNexusEqualityProbe checks whether kglite's `MATCH (n) WHERE n.objectid = X`
// returns ALL nodes sharing that objectid, or only the first label's match. The
// 2-copy pattern aggregation gave suspicious output suggesting only one node is
// returned even when two exist.
//
// Run with:
//
//	BH_SKIP_ANALYSIS=1 go test -v -tags e2e -timeout 5m -run TestKNexusEqualityProbe ./cmd/api/src/test/e2e/repro/
func TestKNexusEqualityProbe(t *testing.T) {
	ctx := context.Background()
	zipPath := knexusDataPath()
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		t.Skipf("k-nexus zip not found: %s", zipPath)
	}

	db := openGraph(t)
	ingestZipFull(ctx, t, db, zipPath)
	runKNexusAnalysis(ctx, t, db)

	// Pick the first objectid that has exactly 2 copies (per the count query)
	oid := strings.TrimSpace(queryString(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL "+
			"WITH n.objectid AS oid, count(n) AS cnt "+
			"WHERE cnt = 2 RETURN oid LIMIT 1"))
	t.Logf("probing 2-copy objectid: %q", oid)

	// Approach 1: bare WHERE equality
	r1 := queryString(ctx, t, db,
		fmt.Sprintf("MATCH (n) WHERE n.objectid = '%s' RETURN labels(n) AS labels", oid))
	t.Logf("Approach 1 (WHERE n.objectid='X' RETURN labels): rows=\n%s", r1)
	t.Logf("  row count: %d", strings.Count(r1, "\n")+1)

	// Approach 2: label-agnostic with explicit count
	r2 := queryString(ctx, t, db,
		fmt.Sprintf("MATCH (n) WHERE n.objectid = '%s' RETURN count(n)", oid))
	t.Logf("Approach 2 (RETURN count(n)): %s", strings.TrimSpace(r2))

	// Approach 3: use property pattern
	r3 := queryString(ctx, t, db,
		fmt.Sprintf("MATCH (n {objectid: '%s'}) RETURN labels(n) AS labels", oid))
	t.Logf("Approach 3 ({objectid: 'X'} RETURN labels): rows=\n%s", r3)
	t.Logf("  row count: %d", strings.Count(r3, "\n")+1)

	// Approach 4: id and labels together
	r4 := queryString(ctx, t, db,
		fmt.Sprintf("MATCH (n {objectid: '%s'}) RETURN id(n), labels(n)", oid))
	t.Logf("Approach 4 (id + labels): rows=\n%s", r4)
}

// TestKNexus2CopyPatterns ingests the k-nexus dataset (no analysis required) and
// prints, for every objectid that appears on exactly N=2 or N=3 nodes, the
// aggregated patterns of label sets. This pinpoints which (label-set, label-set)
// pair drives the 705-objectid divergence between kglite and Neo4j.
//
// Run with:
//
//	BH_SKIP_ANALYSIS=1 go test -v -tags e2e -timeout 10m -run TestKNexus2CopyPatterns ./cmd/api/src/test/e2e/repro/
func TestKNexus2CopyPatterns(t *testing.T) {
	ctx := context.Background()
	zipPath := knexusDataPath()
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		t.Skipf("k-nexus zip not found: %s", zipPath)
	}

	t.Log("=== Ingesting k-nexus-global dataset (no analysis) ===")
	db := openGraph(t)
	ingestZipFull(ctx, t, db, zipPath)
	runKNexusAnalysis(ctx, t, db) // no-op under BH_SKIP_ANALYSIS=1

	totalNodes := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
	t.Logf("Total nodes: %d", totalNodes)

	// Helper that, given a copy count N, finds all objectids appearing on exactly
	// N nodes, fetches each node's labels, sorts them within and across, and
	// aggregates by the resulting pattern signature.
	aggregateCopies := func(copyCount int64) map[string]int {
		patterns := make(map[string]int)

		// Collect objectids with exactly `copyCount` nodes
		var oids []string
		oidRows := queryString(ctx, t, db, fmt.Sprintf(
			"MATCH (n) WHERE n.objectid IS NOT NULL "+
				"WITH n.objectid AS oid, count(n) AS cnt "+
				"WHERE cnt = %d RETURN oid", copyCount))
		for _, line := range strings.Split(strings.TrimSpace(oidRows), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				oids = append(oids, line)
			}
		}
		t.Logf("  found %d objectids with exactly %d copies", len(oids), copyCount)

		for _, oid := range oids {
			// NOTE: kglite has a bug where `WHERE n.objectid = '<literal>'` returns
			// only 1 row when N>1 nodes share the objectid. The property-pattern form
			// `(n {objectid:'X'})` is correct. See TestKNexusEqualityProbe.
			labelLine := queryString(ctx, t, db, fmt.Sprintf(
				"MATCH (n {objectid: '%s'}) RETURN labels(n) AS labels", oid))
			// labelLine is one row per copy. Each row is a Go slice format like
			// "[Label1 Label2]" (space-separated, bracket-wrapped).
			var labelSets []string
			for _, row := range strings.Split(strings.TrimSpace(labelLine), "\n") {
				row = strings.TrimSpace(row)
				if row == "" {
					continue
				}
				// Strip leading "[" and trailing "]"
				row = strings.TrimPrefix(row, "[")
				row = strings.TrimSuffix(row, "]")
				parts := strings.Fields(row)
				sortedParts := make([]string, len(parts))
				copy(sortedParts, parts)
				// canonicalise label order
				for i := 0; i < len(sortedParts); i++ {
					for j := i + 1; j < len(sortedParts); j++ {
						if sortedParts[i] > sortedParts[j] {
							sortedParts[i], sortedParts[j] = sortedParts[j], sortedParts[i]
						}
					}
				}
				labelSets = append(labelSets, "{"+strings.Join(sortedParts, "+")+"}")
			}
			// canonicalise inter-set order
			for i := 0; i < len(labelSets); i++ {
				for j := i + 1; j < len(labelSets); j++ {
					if labelSets[i] > labelSets[j] {
						labelSets[i], labelSets[j] = labelSets[j], labelSets[i]
					}
				}
			}
			pattern := strings.Join(labelSets, " | ")
			patterns[pattern]++
		}
		return patterns
	}

	t.Log("")
	t.Log("=== 2-copy label-pair patterns (kglite) ===")
	patterns2 := aggregateCopies(2)
	type kv struct {
		pattern string
		count   int
	}
	var sorted2 []kv
	for k, v := range patterns2 {
		sorted2 = append(sorted2, kv{k, v})
	}
	for i := 0; i < len(sorted2); i++ {
		for j := i + 1; j < len(sorted2); j++ {
			if sorted2[i].count < sorted2[j].count {
				sorted2[i], sorted2[j] = sorted2[j], sorted2[i]
			}
		}
	}
	totalPattern2 := 0
	for _, p := range sorted2 {
		t.Logf("  %5d   %s", p.count, p.pattern)
		totalPattern2 += p.count
	}
	t.Logf("  ----- TOTAL 2-copy objectids: %d", totalPattern2)

	t.Log("")
	t.Log("=== 3-copy label-pair patterns (kglite) ===")
	patterns3 := aggregateCopies(3)
	var sorted3 []kv
	for k, v := range patterns3 {
		sorted3 = append(sorted3, kv{k, v})
	}
	for i := 0; i < len(sorted3); i++ {
		for j := i + 1; j < len(sorted3); j++ {
			if sorted3[i].count < sorted3[j].count {
				sorted3[i], sorted3[j] = sorted3[j], sorted3[i]
			}
		}
	}
	totalPattern3 := 0
	for _, p := range sorted3 {
		t.Logf("  %5d   %s", p.count, p.pattern)
		totalPattern3 += p.count
	}
	t.Logf("  ----- TOTAL 3-copy objectids: %d", totalPattern3)
}

// TestEmptyIdentityKindLabelLessMerge verifies that when UpdateNodeBy is called
// with IdentityKind=graph.EmptyKind, kglite emits a label-less MERGE so the
// upsert matches an existing :Base stub (created by a prior UpdateRelationshipBy)
// rather than creating a separate (:GH_ExternalIdentity) node.
//
// This mirrors Neo4j's dawgs cypher.go behaviour: an empty IdentityKind produces
// MERGE (n {objectid: $id}) SET ..., n:GH_ExternalIdentity. Without the fix,
// kglite falls back to Node.Kinds[0] for the MERGE label and ends up creating
// a second node, producing 2 nodes for the same objectid.
//
// Run with:
//
//	go test -v -tags e2e -timeout 1m -run TestEmptyIdentityKindLabelLessMerge ./cmd/api/src/test/e2e/repro/
func TestEmptyIdentityKindLabelLessMerge(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Phase 1: an UpdateRelationshipBy that mirrors the real ingest flow's
	// "cross-reference / edge-only" path — start node has identityKind=Base,
	// Kinds=[Base], creating a (:Base {objectid:'OID-X'}) stub. The end node
	// is an unrelated harmless stub.
	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		startProps := graph.NewProperties()
		startProps.Set("objectid", "OID-X")
		startNode := &graph.Node{
			Kinds:      graph.Kinds{graph.StringKind("Base")},
			Properties: startProps,
		}
		endProps := graph.NewProperties()
		endProps.Set("objectid", "OID-end")
		endNode := &graph.Node{
			Kinds:      graph.Kinds{graph.StringKind("Other")},
			Properties: endProps,
		}
		relProps := graph.NewProperties()
		rel := &graph.Relationship{
			Kind:       graph.StringKind("Touches"),
			Properties: relProps,
		}
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Start:                   startNode,
			StartIdentityKind:       graph.StringKind("Base"),
			StartIdentityProperties: []string{"objectid"},
			End:                     endNode,
			EndIdentityKind:         graph.StringKind("Other"),
			EndIdentityProperties:   []string{"objectid"},
			Relationship:            rel,
			IdentityProperties:      []string{},
		})
	})
	require.NoError(t, err, "Phase 1 UpdateRelationshipBy create :Base stub")

	// Phase 2: the authoritative node ingest with IdentityKind=EmptyKind and
	// Kinds=[GH_ExternalIdentity]. This should match the existing :Base stub
	// via a label-less MERGE pattern and add the GH_ExternalIdentity label.
	err = db.BatchOperation(ctx, func(batch graph.Batch) error {
		props := graph.NewProperties()
		props.Set("objectid", "OID-X")
		props.Set("name", "alice")
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node: &graph.Node{
				Kinds:      graph.Kinds{graph.StringKind("GH_ExternalIdentity")},
				Properties: props,
			},
			IdentityKind:       graph.EmptyKind,
			IdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err, "Phase 2 UpdateNodeBy with EmptyKind identity")

	// Use property-pattern form ({objectid:'X'}) — kglite has a separate bug
	// where WHERE n.objectid='X' returns only 1 of N matching nodes.
	count := queryCount(ctx, t, db, "MATCH (n {objectid: 'OID-X'}) RETURN count(n)")
	labels := queryString(ctx, t, db, "MATCH (n {objectid: 'OID-X'}) RETURN labels(n)")
	t.Logf("OID-X count = %d, labels = %q", count, labels)

	if count != 1 {
		t.Errorf("BUG: expected exactly 1 node for OID-X, got %d (kglite split the node)", count)
	}
	if !strings.Contains(labels, "Base") {
		t.Errorf("expected labels to contain Base, got %q", labels)
	}
	if !strings.Contains(labels, "GH_ExternalIdentity") {
		t.Errorf("expected labels to contain GH_ExternalIdentity, got %q", labels)
	}
}
