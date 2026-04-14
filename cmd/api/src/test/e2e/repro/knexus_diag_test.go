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
func runKNexusAnalysis(ctx context.Context, t *testing.T, db graph.Database) {
	t.Helper()
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
