//go:build e2e

package repro

import (
	"context"
	"fmt"
	"testing"

	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFlushBoundaryMergeDuplication tests whether MERGE creates duplicate nodes
// when the same label+objectid is referenced across a batch flush boundary.
//
// The kglite batch flushes every 5000 Cypher operations. If a MERGE for
// :SCIM {objectid: X} is issued in flush N, and another MERGE for the same
// label+objectid is issued in flush N+1, the second MERGE must find the node
// created by the first. If it doesn't, a duplicate is created.
//
// This test creates enough operations to force multiple flushes, with the same
// objectids referenced in separate flushes.
func TestFlushBoundaryMergeDuplication(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	const (
		// Create enough users to cross a batch flush boundary.
		// Each user generates ~3 operations (1 UpdateNodeBy + property SETs).
		// With flushSize=5000, we need ~2000 users to trigger a flush from
		// UpdateNodeBy alone. Then the UpdateRelationshipBy calls in the
		// second phase will be in a subsequent flush.
		numUsers = 2000
	)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		// Phase 1: Create Okta nodes for all users.
		// This will trigger one or more flushes at the 5000-op boundary.
		for i := 0; i < numUsers; i++ {
			oid := fmt.Sprintf("USER_%04d", i)
			if err := batch.UpdateNodeBy(graph.NodeUpdate{
				Node: graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": oid,
					"name":     fmt.Sprintf("User %d", i),
					"lastseen": "2025-01-01",
				}), graph.StringKind("Okta"), graph.StringKind("Okta_User")),
				IdentityKind:       graph.StringKind("Okta"),
				IdentityProperties: []string{"objectid"},
			}); err != nil {
				return err
			}
		}

		// Phase 2: Create relationships with Base identity kind stubs.
		// These will reference the same OIDs but with identity kind "Base".
		// Since Okta != Base, these create new :Base stub nodes.
		for i := 0; i < numUsers; i++ {
			oid := fmt.Sprintf("USER_%04d", i)
			if err := batch.UpdateRelationshipBy(graph.RelationshipUpdate{
				Start: graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": oid,
					"lastseen": "2025-01-01",
				}), graph.StringKind("Base"), graph.StringKind("Okta_User")),
				StartIdentityKind:       graph.StringKind("Base"),
				StartIdentityProperties: []string{"objectid"},
				End: graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": fmt.Sprintf("TARGET_%04d", i),
					"lastseen": "2025-01-01",
				}), graph.StringKind("Base")),
				EndIdentityKind:       graph.StringKind("Base"),
				EndIdentityProperties: []string{"objectid"},
				Relationship: graph.PrepareRelationship(graph.AsProperties(map[string]any{}), graph.StringKind("Hybrid_Edge")),
			}); err != nil {
				return err
			}
		}

		// Phase 3: Create SCIM relationships with SCIM identity kind.
		// These reference the SAME user OIDs with identity kind "SCIM".
		// Each should create exactly 1 new :SCIM stub node per user.
		// A second SCIM edge for the same user should find the existing stub.
		for i := 0; i < numUsers; i++ {
			oid := fmt.Sprintf("USER_%04d", i)
			if err := batch.UpdateRelationshipBy(graph.RelationshipUpdate{
				Start: graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": oid,
					"lastseen": "2025-01-01",
				}), graph.StringKind("SCIM")),
				StartIdentityKind:       graph.StringKind("SCIM"),
				StartIdentityProperties: []string{"objectid"},
				End: graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": fmt.Sprintf("SCIM_%04d", i),
					"lastseen": "2025-01-01",
				}), graph.StringKind("SCIM")),
				EndIdentityKind:       graph.StringKind("SCIM"),
				EndIdentityProperties: []string{"objectid"},
				Relationship: graph.PrepareRelationship(graph.AsProperties(map[string]any{}), graph.StringKind("SCIM_Provisioned")),
			}); err != nil {
				return err
			}
		}

		// Phase 4: A SECOND SCIM edge per user (simulating SCIM_MemberOf).
		// This must find the existing :SCIM stub — NOT create a duplicate.
		for i := 0; i < numUsers; i++ {
			oid := fmt.Sprintf("USER_%04d", i)
			if err := batch.UpdateRelationshipBy(graph.RelationshipUpdate{
				Start: graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": oid,
					"lastseen": "2025-01-01",
				}), graph.StringKind("SCIM")),
				StartIdentityKind:       graph.StringKind("SCIM"),
				StartIdentityProperties: []string{"objectid"},
				End: graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": fmt.Sprintf("SCIM_GROUP_%04d", i),
					"lastseen": "2025-01-01",
				}), graph.StringKind("SCIM")),
				EndIdentityKind:       graph.StringKind("SCIM"),
				EndIdentityProperties: []string{"objectid"},
				Relationship: graph.PrepareRelationship(graph.AsProperties(map[string]any{}), graph.StringKind("SCIM_MemberOf")),
			}); err != nil {
				return err
			}
		}

		return nil
	})
	require.NoError(t, err)

	// Each USER_* OID should have exactly 3 nodes: Okta + Base + SCIM
	// (NOT 4 — the second SCIM edge must reuse the existing SCIM stub)
	totalNodes := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
	tripleCopies := queryCount(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL "+
			"WITH n.objectid AS oid, count(n) AS cnt "+
			"WHERE cnt = 3 RETURN count(oid)")
	quadCopies := queryCount(ctx, t, db,
		"MATCH (n) WHERE n.objectid IS NOT NULL "+
			"WITH n.objectid AS oid, count(n) AS cnt "+
			"WHERE cnt > 3 RETURN count(oid)")

	// Expected: numUsers*3 (Okta+Base+SCIM) + numUsers targets + numUsers SCIM nodes + numUsers SCIM groups
	expectedTotal := numUsers*3 + numUsers + numUsers + numUsers
	t.Logf("Total nodes: %d (expected %d)", totalNodes, expectedTotal)
	t.Logf("OIDs with exactly 3 copies: %d (expected %d)", tripleCopies, numUsers)
	t.Logf("OIDs with >3 copies (BUGS): %d (expected 0)", quadCopies)

	assert.Equal(t, expectedTotal, totalNodes,
		"Total node count mismatch — SCIM stubs may be duplicated across flush boundaries")
	assert.Equal(t, 0, quadCopies,
		"No OID should have >3 copies. >3 means SCIM stubs were duplicated.")
}
