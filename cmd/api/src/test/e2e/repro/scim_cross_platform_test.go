//go:build e2e

package repro

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/bloodhound/cmd/api/src/services/graphify"
	"github.com/specterops/bloodhound/cmd/api/src/services/graphify/endpoint"
	"github.com/specterops/bloodhound/cmd/api/src/services/upload"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

// TestSCIMCrossPlatformNodeCount reproduces the 706-extra-node mismatch seen
// in the KNexus golden tests using a minimal 5-user dataset extracted from the
// full k-nexusglobal zip.
//
// The dataset has 3 files ingested in order:
//   1. 01_okta_nodes.json  — 5 Okta_User nodes (sourceKind=Okta)
//   2. 02_hybrid_edges.json — hybrid edges referencing these users (no sourceKind, endpoint kind=Okta_User → identity "Base")
//   3. 03_scim_data.json   — SCIM_Provisioned edges + SCIM nodes (sourceKind=SCIM → identity "SCIM")
//
// The golden (Neo4j, 11203 nodes) was generated with the OLD endpointIdentityKind
// that returned "Base" for SCIM edges (before commit 95f32225). The current code
// returns "SCIM", creating separate :SCIM stubs — hence 701 extra nodes (11909).
//
// Options to resolve:
//   A. Regenerate the golden from Neo4j with the current code
//   B. Revert endpointIdentityKind to return "Base" for SCIM edge endpoints
//   C. Accept 3-copy as correct and update golden expectations
//
// Run with:
//
//	go test -v -tags e2e -timeout 5m -run TestSCIMCrossPlatformNodeCount ./cmd/api/src/test/e2e/repro/
func TestSCIMCrossPlatformNodeCount(t *testing.T) {
	ctx := context.Background()
	dataDir := reproDataDir() + "/scim_cross_platform"

	// Skip if test data doesn't exist
	zipPath := createZipFromDir(t, dataDir)
	db := openGraph(t)

	// Ingest the mini dataset
	ingestZip(ctx, t, db, zipPath)

	// Check total nodes
	total := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
	t.Logf("Total nodes: %d", total)

	// Check per-label counts
	labelInfo := queryString(ctx, t, db,
		"MATCH (n) UNWIND labels(n) AS lbl RETURN lbl, count(*) AS c ORDER BY c DESC")
	t.Logf("Per-label counts:\n%s", labelInfo)

	// Check nodes-per-objectid distribution
	dist := queryString(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL "+
			"WITH n.objectid AS oid, count(n) AS cnt "+
			"RETURN cnt AS copies, count(oid) AS num_oids ORDER BY cnt")
	t.Logf("Nodes-per-objectid distribution:\n%s", dist)

	// Check the 5 sample user OIDs specifically
	dupes := queryString(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL "+
			"WITH n.objectid AS oid, count(n) AS cnt "+
			"WHERE cnt > 2 "+
			"RETURN oid, cnt ORDER BY cnt DESC LIMIT 10")
	t.Logf("OIDs with >2 copies (should be 0):\n%s", dupes)

	// Show all nodes with their labels for each duplicated OID
	dupeOIDs := queryString(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL "+
			"WITH n.objectid AS oid, count(n) AS cnt "+
			"WHERE cnt > 2 RETURN oid LIMIT 5")
	for _, line := range strings.Split(dupeOIDs, "\n") {
		if line == "" {
			continue
		}
		labels := queryString(ctx, t, db,
			"MATCH (n {objectid: '"+line+"'}) RETURN id(n), labels(n)")
		t.Logf("OID %s labels:\n%s", line, labels)
	}

	// The key assertion: no objectid should have >2 physical nodes.
	// In Neo4j, the 5 sample users each have:
	//   1 Okta node (from IngestNode with identityKind=Okta)
	//   1 Base stub (from hybrid edge with identityKind=Base)
	// The SCIM edge should NOT create a 3rd node if it uses identityKind=SCIM,
	// because that's separate from Base. BUT in the golden (Neo4j), SCIM edges
	// with sourceKind="SCIM" use MERGE(:SCIM {objectid:X}), which DOES create a
	// separate node.
	//
	// The actual question: does kglite match Neo4j's node count for this dataset?
	tripleCopies := queryCount(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL "+
			"WITH n.objectid AS oid, count(n) AS cnt "+
			"WHERE cnt > 2 RETURN count(oid)")
	t.Logf("OIDs with >2 copies: %d", tripleCopies)
}

// TestSCIMCrossPlatformSingleBatch ingests all 3 files in a single BatchOperation,
// matching the real KNexus ingest flow. This is the critical difference from
// TestSCIMCrossPlatformStepByStep which uses separate BatchOperations.
func TestSCIMCrossPlatformSingleBatch(t *testing.T) {
	ctx := context.Background()
	dataDir := reproDataDir() + "/scim_cross_platform"

	db := openGraph(t)

	files := []string{
		"01_okta_nodes.json",
		"02_hybrid_edges.json",
		"03_scim_data.json",
	}

	schema, err := upload.LoadIngestSchema()
	require.NoError(t, err)

	resolver := endpoint.NewResolver(db)
	ic := graphify.NewIngestContext(ctx,
		graphify.WithIngestTime(time.Now().UTC()),
		graphify.WithEndpointResolver(resolver),
	)
	readOpts := graphify.ReadOptions{
		FileType:     model.FileTypeJson,
		IngestSchema: schema,
		RegisterSourceKind: func(kind graph.Kind) error {
			return db.RefreshKinds(ctx)
		},
	}

	// Ingest all files in a SINGLE BatchOperation (like the real zip ingest)
	err = db.BatchOperation(ctx, func(batch graph.Batch) error {
		ic.BindBatchUpdater(batch)

		for _, fname := range files {
			f, err := os.Open(dataDir + "/" + fname)
			if err != nil {
				return err
			}
			if err := graphify.ReadFileForIngest(ic, f, readOpts); err != nil {
				t.Logf("WARN: error ingesting %s: %v", fname, err)
			}
			f.Close()
		}
		return nil
	})
	require.NoError(t, err)

	total := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
	labels := queryString(ctx, t, db,
		"MATCH (n) UNWIND labels(n) AS lbl RETURN lbl, count(*) AS c ORDER BY c DESC")
	dist := queryString(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL "+
			"WITH n.objectid AS oid, count(n) AS cnt "+
			"RETURN cnt AS copies, count(oid) AS num_oids ORDER BY cnt")

	t.Logf("Total nodes: %d", total)
	t.Logf("Labels:\n%s", labels)
	t.Logf("Copies distribution:\n%s", dist)

	tripleCopies := queryCount(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL "+
			"WITH n.objectid AS oid, count(n) AS cnt "+
			"WHERE cnt > 2 RETURN count(oid)")
	t.Logf("OIDs with >2 copies: %d", tripleCopies)
}

// TestSCIMCrossPlatformStepByStep ingests the 3 files one at a time, checking
// node counts after each step. This pinpoints which file introduces extra nodes.
func TestSCIMCrossPlatformStepByStep(t *testing.T) {
	ctx := context.Background()
	dataDir := reproDataDir() + "/scim_cross_platform"

	db := openGraph(t)

	files := []struct {
		name     string
		expected string // description of what this file should create
	}{
		{"01_okta_nodes.json", "Okta_User nodes (1 per user)"},
		{"02_hybrid_edges.json", "Base stubs (1 per user from hybrid edges)"},
		{"03_scim_data.json", "SCIM stubs + SCIM nodes"},
	}

	for _, f := range files {
		t.Run(f.name, func(t *testing.T) {
			ingestSingleFile(ctx, t, db, dataDir+"/"+f.name)

			total := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
			rels := queryCount(ctx, t, db, "MATCH ()-[r]->() RETURN count(r)")

			labels := queryString(ctx, t, db,
				"MATCH (n) UNWIND labels(n) AS lbl RETURN lbl, count(*) AS c ORDER BY c DESC")

			dist := queryString(ctx, t, db,
				"MATCH (n) WHERE n.objectid IS NOT NULL "+
					"WITH n.objectid AS oid, count(n) AS cnt "+
					"RETURN cnt AS copies, count(oid) AS num_oids ORDER BY cnt")

			t.Logf("After %s (%s):", f.name, f.expected)
			t.Logf("  Total nodes: %d, relationships: %d", total, rels)
			t.Logf("  Labels:\n%s", labels)
			t.Logf("  Copies distribution:\n%s", dist)
		})
	}
}


// ingestSingleFile ingests a single OpenGraph JSON file into the graph.
func ingestSingleFile(ctx context.Context, t *testing.T, db graph.Database, filePath string) {
	t.Helper()

	schema, err := upload.LoadIngestSchema()
	require.NoError(t, err)

	resolver := endpoint.NewResolver(db)
	ic := graphify.NewIngestContext(ctx,
		graphify.WithIngestTime(time.Now().UTC()),
		graphify.WithEndpointResolver(resolver),
	)
	readOpts := graphify.ReadOptions{
		FileType:     model.FileTypeJson,
		IngestSchema: schema,
		RegisterSourceKind: func(kind graph.Kind) error {
			return db.RefreshKinds(ctx)
		},
	}

	err = db.BatchOperation(ctx, func(batch graph.Batch) error {
		ic.BindBatchUpdater(batch)

		f, err := os.Open(filePath)
		if err != nil {
			return err
		}
		defer f.Close()

		return graphify.ReadFileForIngest(ic, f, readOpts)
	})
	if err != nil {
		t.Logf("WARN: batch error during ingest of %s: %v", filePath, err)
	}
}
