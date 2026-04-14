//go:build e2e

package repro

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTripleCopyRepro reproduces the triple-copy issue seen in k-nexus.
// Scenario:
//   1. IngestNode creates :Okta node for OID "USER_001" (identity kind = Okta)
//   2. Hybrid edge (no source_kind) references USER_001 as start with kind=Okta_User
//      → endpointIdentityKind(EmptyKind, Okta_User) = "Base"
//      → MERGE (s:Base {objectid: "USER_001"}) creates a :Base stub (correct, matches Neo4j)
//   3. SAML edge (no source_kind) also references USER_001 as start with kind=Okta_User
//      → same logic → MERGE (s:Base {objectid: "USER_001"})
//      → SHOULD find existing :Base stub (from step 2), NOT create a 3rd copy
//
// Expected: 2 physical nodes for USER_001 (one :Okta, one :Base stub)
// Bug: kglite creates 3 physical nodes (two :Base stubs)
func TestTripleCopyRepro(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Step 1: Create the Okta node (simulating IngestNode with sourceKind=Okta)
	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		update := graph.NodeUpdate{
			Node: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "USER_001",
				"name":     "TEST USER",
				"lastseen": "2025-01-01",
			}), graph.StringKind("Okta"), graph.StringKind("Okta_User")),
			IdentityKind:       graph.StringKind("Okta"),
			IdentityProperties: []string{"objectid"},
		}
		return batch.UpdateNodeBy(update)
	})
	require.NoError(t, err)

	// Verify step 1: should have 1 node
	count1 := queryCount(ctx, t, db, "MATCH (n) WHERE n.objectid = 'USER_001' RETURN count(n)")
	t.Logf("After step 1 (Okta node): %d nodes for USER_001 (expected 1)", count1)
	assert.Equal(t, 1, count1)

	// Step 2: Hybrid edge references USER_001 with identity kind "Base"
	// (simulating okta-graph-hybrid.json with no source_kind)
	err = db.BatchOperation(ctx, func(batch graph.Batch) error {
		update := graph.RelationshipUpdate{
			Start: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "USER_001",
				"lastseen": "2025-01-01",
			}), graph.StringKind("Base"), graph.StringKind("Okta_User")),
			StartIdentityKind:       graph.StringKind("Base"),
			StartIdentityProperties: []string{"objectid"},
			End: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "TARGET_001",
				"lastseen": "2025-01-01",
			}), graph.StringKind("Base"), graph.StringKind("Okta_User")),
			EndIdentityKind:       graph.StringKind("Base"),
			EndIdentityProperties: []string{"objectid"},
			Relationship:          graph.PrepareRelationship(graph.AsProperties(map[string]any{}), graph.StringKind("Okta_OutboundSSO")),
		}
		return batch.UpdateRelationshipBy(update)
	})
	require.NoError(t, err)

	// Verify step 2: USER_001 should have 2 nodes (Okta + Base stub)
	count2 := queryCount(ctx, t, db, "MATCH (n) WHERE n.objectid = 'USER_001' RETURN count(n)")
	t.Logf("After step 2 (hybrid edge): %d nodes for USER_001 (expected 2)", count2)
	labels2 := queryString(ctx, t, db, "MATCH (n) WHERE n.objectid = 'USER_001' RETURN labels(n)")
	for _, l := range strings.Split(labels2, "\n") {
		t.Logf("  labels: %s", l)
	}
	assert.Equal(t, 2, count2,
		"USER_001 should have 2 nodes: original :Okta + :Base stub from hybrid edge")

	// Step 3: SAML edge also references USER_001 with identity kind "Base"
	// (simulating githound_enterprise_saml with no source_kind)
	err = db.BatchOperation(ctx, func(batch graph.Batch) error {
		update := graph.RelationshipUpdate{
			Start: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "USER_001",
				"lastseen": "2025-01-01",
			}), graph.StringKind("Base"), graph.StringKind("Okta_User")),
			StartIdentityKind:       graph.StringKind("Base"),
			StartIdentityProperties: []string{"objectid"},
			End: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "SAML_TARGET_001",
				"lastseen": "2025-01-01",
			}), graph.StringKind("Base")),
			EndIdentityKind:       graph.StringKind("Base"),
			EndIdentityProperties: []string{"objectid"},
			Relationship:          graph.PrepareRelationship(graph.AsProperties(map[string]any{}), graph.StringKind("GH_SyncedTo")),
		}
		return batch.UpdateRelationshipBy(update)
	})
	require.NoError(t, err)

	// Verify step 3: USER_001 should STILL have 2 nodes (Okta + Base stub)
	count3 := queryCount(ctx, t, db, "MATCH (n) WHERE n.objectid = 'USER_001' RETURN count(n)")
	t.Logf("After step 3 (SAML edge): %d nodes for USER_001 (expected 2)", count3)
	labels3 := queryString(ctx, t, db, "MATCH (n) WHERE n.objectid = 'USER_001' RETURN labels(n)")
	for _, l := range strings.Split(labels3, "\n") {
		t.Logf("  labels: %s", l)
	}
	assert.Equal(t, 2, count3,
		"USER_001 should still have 2 nodes. The SAML edge MERGE should reuse the :Base stub.")
}

// TestTripleCopySameBatch is the same test but all three steps happen in the SAME
// BatchOperation (which is how the actual k-nexus zip ingest works).
func TestTripleCopySameBatch(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		// Step 1: Create the Okta node
		update := graph.NodeUpdate{
			Node: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "USER_001",
				"name":     "TEST USER",
				"lastseen": "2025-01-01",
			}), graph.StringKind("Okta"), graph.StringKind("Okta_User")),
			IdentityKind:       graph.StringKind("Okta"),
			IdentityProperties: []string{"objectid"},
		}
		if err := batch.UpdateNodeBy(update); err != nil {
			return err
		}

		// Step 2: Hybrid edge references USER_001 with identity kind "Base"
		rel1 := graph.RelationshipUpdate{
			Start: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "USER_001",
				"lastseen": "2025-01-01",
			}), graph.StringKind("Base"), graph.StringKind("Okta_User")),
			StartIdentityKind:       graph.StringKind("Base"),
			StartIdentityProperties: []string{"objectid"},
			End: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "TARGET_001",
				"lastseen": "2025-01-01",
			}), graph.StringKind("Base"), graph.StringKind("Okta_User")),
			EndIdentityKind:       graph.StringKind("Base"),
			EndIdentityProperties: []string{"objectid"},
			Relationship:          graph.PrepareRelationship(graph.AsProperties(map[string]any{}), graph.StringKind("Okta_OutboundSSO")),
		}
		if err := batch.UpdateRelationshipBy(rel1); err != nil {
			return err
		}

		// Step 3: SAML edge also references USER_001 with identity kind "Base"
		rel2 := graph.RelationshipUpdate{
			Start: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "USER_001",
				"lastseen": "2025-01-01",
			}), graph.StringKind("Base"), graph.StringKind("Okta_User")),
			StartIdentityKind:       graph.StringKind("Base"),
			StartIdentityProperties: []string{"objectid"},
			End: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "SAML_TARGET_001",
				"lastseen": "2025-01-01",
			}), graph.StringKind("Base")),
			EndIdentityKind:       graph.StringKind("Base"),
			EndIdentityProperties: []string{"objectid"},
			Relationship:          graph.PrepareRelationship(graph.AsProperties(map[string]any{}), graph.StringKind("GH_SyncedTo")),
		}
		return batch.UpdateRelationshipBy(rel2)
	})
	require.NoError(t, err)

	count := queryCount(ctx, t, db, "MATCH (n) WHERE n.objectid = 'USER_001' RETURN count(n)")
	labels := queryString(ctx, t, db, "MATCH (n) WHERE n.objectid = 'USER_001' RETURN labels(n)")
	t.Logf("Nodes for USER_001: %d (expected 2)", count)
	for _, l := range strings.Split(labels, "\n") {
		t.Logf("  labels: %s", l)
	}
	assert.Equal(t, 2, count,
		"USER_001 should have 2 nodes: :Okta + :Base stub. NOT 3.")
}

// TestTripleCopySameBatchManyUsers tests the scenario with many users to ensure
// the batch flush boundaries don't cause duplicates.
func TestTripleCopySameBatchManyUsers(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	const numUsers = 100

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		// Step 1: Create all Okta nodes
		for i := 0; i < numUsers; i++ {
			oid := fmt.Sprintf("USER_%03d", i)
			update := graph.NodeUpdate{
				Node: graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": oid,
					"name":     fmt.Sprintf("Test User %d", i),
					"lastseen": "2025-01-01",
				}), graph.StringKind("Okta"), graph.StringKind("Okta_User")),
				IdentityKind:       graph.StringKind("Okta"),
				IdentityProperties: []string{"objectid"},
			}
			if err := batch.UpdateNodeBy(update); err != nil {
				return err
			}
		}

		// Step 2: Hybrid edges reference all users with identity kind "Base"
		for i := 0; i < numUsers; i++ {
			oid := fmt.Sprintf("USER_%03d", i)
			rel := graph.RelationshipUpdate{
				Start: graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": oid,
					"lastseen": "2025-01-01",
				}), graph.StringKind("Base"), graph.StringKind("Okta_User")),
				StartIdentityKind:       graph.StringKind("Base"),
				StartIdentityProperties: []string{"objectid"},
				End: graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": fmt.Sprintf("HYBRID_TARGET_%03d", i),
					"lastseen": "2025-01-01",
				}), graph.StringKind("Base")),
				EndIdentityKind:       graph.StringKind("Base"),
				EndIdentityProperties: []string{"objectid"},
				Relationship:          graph.PrepareRelationship(graph.AsProperties(map[string]any{}), graph.StringKind("Okta_OutboundSSO")),
			}
			if err := batch.UpdateRelationshipBy(rel); err != nil {
				return err
			}
		}

		// Step 3: SAML edges also reference all users with identity kind "Base"
		for i := 0; i < numUsers; i++ {
			oid := fmt.Sprintf("USER_%03d", i)
			rel := graph.RelationshipUpdate{
				Start: graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": oid,
					"lastseen": "2025-01-01",
				}), graph.StringKind("Base"), graph.StringKind("Okta_User")),
				StartIdentityKind:       graph.StringKind("Base"),
				StartIdentityProperties: []string{"objectid"},
				End: graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": fmt.Sprintf("SAML_TARGET_%03d", i),
					"lastseen": "2025-01-01",
				}), graph.StringKind("Base")),
				EndIdentityKind:       graph.StringKind("Base"),
				EndIdentityProperties: []string{"objectid"},
				Relationship:          graph.PrepareRelationship(graph.AsProperties(map[string]any{}), graph.StringKind("GH_SyncedTo")),
			}
			if err := batch.UpdateRelationshipBy(rel); err != nil {
				return err
			}
		}

		return nil
	})
	require.NoError(t, err)

	// Check for duplicates
	totalNodes := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
	tripleCopies := queryCount(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL "+
			"WITH n.objectid AS oid, count(n) AS cnt "+
			"WHERE cnt > 2 RETURN count(oid)")
	dupes := queryCount(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL "+
			"WITH n.objectid AS oid, count(n) AS cnt "+
			"WHERE cnt > 1 RETURN count(oid)")

	t.Logf("Total nodes: %d", totalNodes)
	t.Logf("Objectids with >1 copy: %d (expected %d — the %d USER_* OIDs)", dupes, numUsers, numUsers)
	t.Logf("Objectids with >2 copies: %d (expected 0 — these are BUGS)", tripleCopies)

	// Each USER_* should have exactly 2 copies (Okta + Base stub)
	// The HYBRID_TARGET_* and SAML_TARGET_* should have 1 copy each
	// Total: 100*2 + 100 + 100 = 400
	assert.Equal(t, numUsers*2+numUsers+numUsers, totalNodes,
		"Expected %d total nodes: %d USER_* x2 + %d HYBRID_TARGET + %d SAML_TARGET",
		numUsers*2+numUsers+numUsers, numUsers, numUsers, numUsers)
	assert.Equal(t, 0, tripleCopies,
		"No objectid should have >2 copies. >2 means duplicate :Base stubs.")
}
