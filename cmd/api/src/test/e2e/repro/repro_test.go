//go:build e2e

// Package repro contains minimal reproduction tests for kglite/Neo4j divergences
// observed in the k-nexus golden tests. These tests are self-contained and do NOT
// depend on the shared fixtures (TestMain) in the parent e2e package.
//
// Run with:
//
//	go test -v -tags e2e -timeout 10m ./cmd/api/src/test/e2e/repro/
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
	"github.com/specterops/bloodhound/cmd/api/src/services/graphify"
	"github.com/specterops/bloodhound/cmd/api/src/services/graphify/endpoint"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/bloodhound/cmd/api/src/services/upload"
	"github.com/specterops/bloodhound/packages/go/analysis"
	"github.com/specterops/bloodhound/packages/go/bomenc"
	kglitedawgs "github.com/specterops/bloodhound/packages/go/kglite/dawgs"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reproDataDir returns the path to the repro testdata directory.
func reproDataDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata", "repro")
}

// openGraph opens a fresh kglite graph.
func openGraph(t *testing.T) graph.Database {
	t.Helper()
	dir := t.TempDir()
	db, err := kglitedawgs.Open(filepath.Join(dir, "test.kgl"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close(context.Background()) })
	return db
}

// createZipFromDir creates a zip from all JSON files in a directory.
func createZipFromDir(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	tmp, err := os.CreateTemp("", "repro-*.zip")
	require.NoError(t, err)
	t.Cleanup(func() { os.Remove(tmp.Name()) })

	w := zip.NewWriter(tmp)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		require.NoError(t, err)
		fw, err := w.Create(entry.Name())
		require.NoError(t, err)
		_, err = fw.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	require.NoError(t, tmp.Close())
	return tmp.Name()
}

// ingestZip ingests a zip file into the graph (tolerant mode).
func ingestZip(ctx context.Context, t *testing.T, db graph.Database, zipPath string) {
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

	err = db.BatchOperation(ctx, func(batch graph.Batch) error {
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
			src, err := f.Open()
			if err != nil {
				slog.Warn("skip zip entry", "file", f.Name, "error", err)
				continue
			}
			normalized, err := bomenc.NormalizeToUTF8(src)
			src.Close()
			if err != nil {
				slog.Warn("skip zip entry", "file", f.Name, "error", err)
				continue
			}
			tmp, err := os.CreateTemp("", "repro-entry-*")
			if err != nil {
				return err
			}
			if _, err := io.Copy(tmp, normalized); err != nil {
				tmp.Close()
				os.Remove(tmp.Name())
				return err
			}
			tmp.Seek(0, io.SeekStart)
			if err := graphify.ReadFileForIngest(ic, tmp, readOpts); err != nil {
				slog.Warn("skip file during ingest", "file", f.Name, "error", err)
			}
			tmp.Close()
			os.Remove(tmp.Name())
		}
		return nil
	})
	if err != nil {
		t.Logf("WARN: batch errors during ingest (non-fatal): %v", err)
	}
}

// runAnalysis runs AD and Azure post-processing.
func runAnalysis(ctx context.Context, t *testing.T, db graph.Database) {
	t.Helper()
	counter := analysis.NewCompositionCounter()
	_, err := ad.Post(ctx, db, true, false, true, &counter)
	require.NoError(t, err, "AD post-processing")
	_, err = azure.Post(ctx, db)
	require.NoError(t, err, "Azure post-processing")
}

// queryCount runs a COUNT query and returns the integer result.
func queryCount(ctx context.Context, t *testing.T, db graph.Database, cypher string) int {
	t.Helper()
	var count int
	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw(cypher, nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		if result.Next() {
			vals := result.Values()
			if len(vals) > 0 {
				switch v := vals[0].(type) {
				case int:
					count = v
				case int64:
					count = int(v)
				case float64:
					count = int(v)
				}
			}
		}
		return result.Error()
	})
	require.NoError(t, err, "query: %s", cypher)
	return count
}

// queryString runs a query and returns all rows as a string.
func queryString(ctx context.Context, t *testing.T, db graph.Database, cypher string) string {
	t.Helper()
	var out strings.Builder
	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw(cypher, nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		for result.Next() {
			vals := result.Values()
			for i, v := range vals {
				if i > 0 {
					out.WriteString(", ")
				}
				out.WriteString(fmt.Sprintf("%v", v))
			}
			out.WriteString("\n")
		}
		return result.Error()
	})
	require.NoError(t, err, "query: %s", cypher)
	return strings.TrimRight(out.String(), "\n")
}

// ingestReproDir creates a kglite graph and ingests all JSON files from a directory.
func ingestReproDir(ctx context.Context, t *testing.T, dir string) graph.Database {
	t.Helper()
	db := openGraph(t)
	zipPath := createZipFromDir(t, dir)
	ingestZip(ctx, t, db, zipPath)
	return db
}

// logNodes prints all nodes with their labels for diagnostics.
func logNodes(ctx context.Context, t *testing.T, db graph.Database) {
	t.Helper()
	t.Log("--- All nodes ---")
	allNodes := queryString(ctx, t, db,
		"MATCH (n) RETURN n.objectid, labels(n) ORDER BY n.objectid")
	if allNodes == "" {
		t.Log("  (none)")
	} else {
		for _, line := range strings.Split(allNodes, "\n") {
			t.Logf("  %s", line)
		}
	}
}

// logDuplicates prints objectids that appear on more than one node.
func logDuplicates(ctx context.Context, t *testing.T, db graph.Database) {
	t.Helper()
	t.Log("--- Duplicate objectids ---")
	dupes := queryString(ctx, t, db,
		"MATCH (n) WITH n.objectid AS oid, count(n) AS cnt WHERE cnt > 1 RETURN oid, cnt ORDER BY oid")
	if dupes == "" {
		t.Log("  (none)")
	} else {
		for _, line := range strings.Split(dupes, "\n") {
			t.Logf("  DUPLICATE: %s", line)
		}
	}
}

// logRelTypes prints all distinct relationship types.
func logRelTypes(ctx context.Context, t *testing.T, db graph.Database) {
	t.Helper()
	t.Log("--- Relationship types ---")
	relTypes := queryString(ctx, t, db,
		"MATCH ()-[r]->() RETURN DISTINCT type(r) ORDER BY type(r)")
	if relTypes == "" {
		t.Log("  (none)")
	} else {
		for _, line := range strings.Split(relTypes, "\n") {
			t.Logf("  %s", line)
		}
	}
}

// ---------------------------------------------------------------------------
// Repro 1: Cross-platform duplicate nodes (Okta + SCIM)
//
// When Okta nodes are ingested first (source_kind=Okta), then SCIM edges
// reference those same objectids (source_kind=SCIM), Neo4j's label-specific
// MERGE creates NEW :SCIM stub nodes for the start endpoints — it does NOT
// reuse the existing :Okta nodes. kglite must match this behavior.
//
// Expected nodes (8 total, matching Neo4j):
//   OKTA_USER_001   (Okta, Okta_User)          — from Okta ingest
//   OKTA_USER_001   (SCIM)                      — intentional stub from SCIM edge
//   OKTA_USER_002   (Okta, Okta_User)          — from Okta ingest
//   OKTA_USER_002   (SCIM)                      — intentional stub from SCIM edge
//   OKTA_USER_003   (Okta, Okta_User)
//   OKTA_GROUP_001  (Okta, Okta_Group)
//   SCIM_ID_AAA     (SCIM, SCIM_User)
//   SCIM_ID_BBB     (SCIM, SCIM_User)
//
// OKTA_USER_001 and OKTA_USER_002 intentionally appear twice (once as Okta,
// once as a SCIM stub) — this matches Neo4j's label-specific MERGE semantics.
// ---------------------------------------------------------------------------

func TestReproCrossPlatformDupes(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(reproDataDir(), "cross_platform_dupes")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skipf("repro data not found: %s", dir)
	}

	db := ingestReproDir(ctx, t, dir)

	totalNodes := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
	oktaUserNodes := queryCount(ctx, t, db, "MATCH (n:Okta_User) RETURN count(n)")
	scimUserNodes := queryCount(ctx, t, db, "MATCH (n:SCIM_User) RETURN count(n)")
	totalEdges := queryCount(ctx, t, db, "MATCH ()-[r]->() RETURN count(r)")
	provEdges := queryCount(ctx, t, db, "MATCH ()-[r:SCIM_Provisioned]->() RETURN count(r)")
	memberEdges := queryCount(ctx, t, db, "MATCH ()-[r:Okta_MemberOf]->() RETURN count(r)")

	logNodes(ctx, t, db)
	logDuplicates(ctx, t, db)

	t.Logf("Total nodes: %d (expected 8)", totalNodes)
	t.Logf("Okta_User nodes: %d (expected 3)", oktaUserNodes)
	t.Logf("SCIM_User nodes: %d (expected 2)", scimUserNodes)
	t.Logf("Total edges: %d (expected 4)", totalEdges)
	t.Logf("SCIM_Provisioned edges: %d (expected 2)", provEdges)
	t.Logf("Okta_MemberOf edges: %d (expected 2)", memberEdges)

	// OKTA_USER_001 and OKTA_USER_002 appear on 2 physical nodes each (Okta + SCIM stub).
	// This matches Neo4j's label-specific MERGE behavior — these stubs are intentional.
	dupeOids := queryCount(ctx, t, db,
		"MATCH (n) WITH n.objectid AS oid, count(n) AS cnt WHERE cnt > 1 RETURN count(oid)")
	assert.Equal(t, 2, dupeOids,
		"OKTA_USER_001 and OKTA_USER_002 should each appear on 2 nodes (Okta + SCIM stub), "+
			"matching Neo4j label-specific MERGE behavior.")

	assert.Equal(t, 8, totalNodes,
		"Expected 8 nodes: 3 Okta_User + 1 Okta_Group + 2 SCIM_User + 2 SCIM stubs "+
			"(one each for OKTA_USER_001 and OKTA_USER_002, matching Neo4j).")
	assert.Equal(t, 4, totalEdges, "Expected 4 edges: 2 Okta_MemberOf + 2 SCIM_Provisioned")
}

// ---------------------------------------------------------------------------
// Repro 2: AZExecuteCommand edge creation
//
// Azure tenant + Windows devices + Intune Service Administrator role +
// role assignment → analysis should create AZExecuteCommand edges from
// the Intune admin to each Windows device. Linux devices are excluded.
//
// Expected AZExecuteCommand edges: 2 (one per Windows device)
// ---------------------------------------------------------------------------

func TestReproAZExecuteCommand(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(reproDataDir(), "az_execute_command")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skipf("repro data not found: %s", dir)
	}

	db := ingestReproDir(ctx, t, dir)
	runAnalysis(ctx, t, db)

	devices := queryCount(ctx, t, db, "MATCH (n:AZDevice) RETURN count(n)")
	winDevices := queryCount(ctx, t, db,
		"MATCH (n:AZDevice) WHERE n.operatingsystem CONTAINS 'indow' OR n.operatingsystem CONTAINS 'INDOW' RETURN count(n)")
	executeCmd := queryCount(ctx, t, db,
		"MATCH ()-[r:AZExecuteCommand]->() RETURN count(r)")

	logNodes(ctx, t, db)
	logRelTypes(ctx, t, db)

	t.Logf("AZDevice nodes: %d (expected 3)", devices)
	t.Logf("Windows devices: %d (expected 2)", winDevices)
	t.Logf("AZExecuteCommand edges: %d (expected 2)", executeCmd)

	assert.Equal(t, 3, devices, "Expected 3 AZDevice nodes (2 Windows + 1 Linux)")
	assert.Equal(t, 2, winDevices, "Expected 2 Windows devices")
	assert.Equal(t, 2, executeCmd,
		"Expected 2 AZExecuteCommand edges (Intune admin -> each Windows device). "+
			"If 0, analysis failed to create AZExecuteCommand edges.")
}

// ---------------------------------------------------------------------------
// Repro 3: Combined cross-platform scenario (AD + Azure + Okta + SCIM)
//
// Exercises both issues together in a single graph, as k-nexus does.
// ---------------------------------------------------------------------------

func TestReproCombined(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(reproDataDir(), "combined")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skipf("repro data not found: %s", dir)
	}

	db := ingestReproDir(ctx, t, dir)
	runAnalysis(ctx, t, db)

	oktaUserCount := queryCount(ctx, t, db, "MATCH (n:Okta_User) RETURN count(n)")
	scimUserCount := queryCount(ctx, t, db, "MATCH (n:SCIM_User) RETURN count(n)")
	azDeviceCount := queryCount(ctx, t, db, "MATCH (n:AZDevice) RETURN count(n)")
	executeCmd := queryCount(ctx, t, db,
		"MATCH ()-[r:AZExecuteCommand]->() RETURN count(r)")

	logNodes(ctx, t, db)
	logDuplicates(ctx, t, db)
	logRelTypes(ctx, t, db)

	// Per-label counts
	t.Log("--- Per-label node counts ---")
	labels := queryString(ctx, t, db,
		"MATCH (n) UNWIND labels(n) AS lbl RETURN lbl, count(*) AS c ORDER BY c DESC")
	for _, line := range strings.Split(labels, "\n") {
		t.Logf("  %s", line)
	}

	t.Logf("Okta_User: %d (expected 2)", oktaUserCount)
	t.Logf("SCIM_User: %d (expected 2)", scimUserCount)
	t.Logf("AZDevice: %d (expected 1)", azDeviceCount)
	t.Logf("AZExecuteCommand edges: %d", executeCmd)

	// Cross-platform stub check: OKTA_USER_001 and OKTA_USER_002 are referenced by SCIM
	// edges (source_kind=SCIM), so label-specific MERGE creates SCIM stub nodes for them.
	// This matches Neo4j behavior — exactly 2 objectids should appear on 2 physical nodes each.
	dupeCount := queryCount(ctx, t, db,
		"MATCH (n) WITH n.objectid AS oid, count(n) AS cnt WHERE cnt > 1 RETURN count(oid)")
	assert.Equal(t, 2, dupeCount,
		"OKTA_USER_001 and OKTA_USER_002 should each appear on 2 nodes (Okta + SCIM stub), "+
			"matching Neo4j label-specific MERGE behavior.")

	assert.Equal(t, 2, oktaUserCount, "Expected 2 Okta_User nodes (alice, bob)")
	assert.Equal(t, 2, scimUserCount, "Expected 2 SCIM_User nodes")
	assert.Equal(t, 1, azDeviceCount, "Expected 1 AZDevice node")
}

// ---------------------------------------------------------------------------
// Repro 4: Intentional cross-platform stub nodes
//
// Neo4j MERGE is label-specific: MERGE (:SCIM {objectid: "X"}) will NOT match
// an existing (:Okta {objectid: "X"}) node. It creates a NEW :SCIM node.
// These "intentional stubs" are expected in the graph — they represent
// cross-platform identity links.
//
// This test verifies that kglite does NOT over-collapse cross-platform nodes.
// When a SCIM edge references an objectid that doesn't exist as a :SCIM node
// (only as :Okta), the MERGE should create a new :SCIM stub — matching Neo4j.
//
// Scenario:
//   - Okta ingest: SHARED_USER_001 (Okta_User), OKTA_ONLY_001 (Okta_User)
//   - SCIM ingest: SCIM_NODE_001 (SCIM_User) + edges:
//       SHARED_USER_001 → SCIM_NODE_001 (SCIM_Provisioned)
//       SCIM_NODE_001 → SCIM_GROUP_001 (SCIM_MemberOf) — creates stub
//
// Expected nodes (5 total):
//   SHARED_USER_001  — 2 physical nodes: [Okta, Okta_User] AND [SCIM] stub
//   OKTA_ONLY_001    — 1 node: [Okta, Okta_User]
//   SCIM_NODE_001    — 1 node: [SCIM, SCIM_User]
//   SCIM_GROUP_001   — 1 node: [SCIM] stub from SCIM_MemberOf edge
//
// The SCIM stub for SHARED_USER_001 is INTENTIONAL — it matches Neo4j's
// label-specific MERGE behavior. Without it, cross-platform traversals break.
// ---------------------------------------------------------------------------

func TestReproIntentionalStubs(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(reproDataDir(), "cross_platform_intentional")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skipf("repro data not found: %s", dir)
	}

	db := ingestReproDir(ctx, t, dir)

	totalNodes := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
	totalEdges := queryCount(ctx, t, db, "MATCH ()-[r]->() RETURN count(r)")
	oktaUserNodes := queryCount(ctx, t, db, "MATCH (n:Okta_User) RETURN count(n)")
	scimUserNodes := queryCount(ctx, t, db, "MATCH (n:SCIM_User) RETURN count(n)")

	logNodes(ctx, t, db)
	logDuplicates(ctx, t, db)

	t.Logf("Total nodes: %d (expected 5)", totalNodes)
	t.Logf("Total edges: %d (expected 2)", totalEdges)
	t.Logf("Okta_User nodes: %d (expected 2)", oktaUserNodes)
	t.Logf("SCIM_User nodes: %d (expected 1)", scimUserNodes)

	// The SCIM_Provisioned edge's start endpoint SHARED_USER_001 should create
	// a :SCIM stub node (matching Neo4j label-specific MERGE), NOT reuse the
	// existing :Okta node. Additionally, SCIM_GROUP_001 is a stub from
	// the SCIM_MemberOf edge's end endpoint.
	assert.Equal(t, 5, totalNodes,
		"Expected 5 nodes: 2 Okta_User + 1 SCIM_User + 1 SCIM stub (SHARED_USER_001) + 1 SCIM stub (SCIM_GROUP_001). "+
			"If fewer, correctKindStr is over-collapsing cross-platform nodes.")

	assert.Equal(t, 2, totalEdges, "Expected 2 edges: SCIM_Provisioned + SCIM_MemberOf")
	assert.Equal(t, 2, oktaUserNodes, "Expected 2 Okta_User nodes")
	assert.Equal(t, 1, scimUserNodes, "Expected 1 SCIM_User node")
}
