//go:build e2e

package repro

import (
	"context"
	"testing"

	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMergeLabelIsolation verifies that MERGE with a specific label does NOT
// match nodes that have a different primary label and don't have the MERGE label.
func TestMergeLabelIsolation(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Create an :Okta node via raw Cypher
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		res := tx.Raw(
			"CREATE (n:Okta {objectid: 'TEST_001'}) SET n:Okta_User, n.__kinds = '[\"Okta\",\"Okta_User\"]' RETURN id(n), labels(n)",
			nil,
		)
		defer res.Close()
		if res.Error() != nil {
			return res.Error()
		}
		for res.Next() {
			t.Logf("Created node: %v", res.Values())
		}
		return res.Error()
	})
	require.NoError(t, err)

	// Now do MERGE (n:Base {objectid: 'TEST_001'}) — this should CREATE a new node
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		res := tx.Raw(
			"MERGE (n:Base {objectid: 'TEST_001'}) RETURN id(n), labels(n)",
			nil,
		)
		defer res.Close()
		if res.Error() != nil {
			return res.Error()
		}
		for res.Next() {
			t.Logf("MERGE result: %v", res.Values())
		}
		return res.Error()
	})
	require.NoError(t, err)

	// Should now have 2 nodes for TEST_001: one :Okta, one :Base
	totalCount := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
	t.Logf("Total nodes: %d (expected 2)", totalCount)
	assert.Equal(t, 2, totalCount)
}

// TestMergeIdempotencyWithUpdateNodeBy checks if MERGE correctly finds
// nodes created by UpdateNodeBy (which uses parameterized Cypher).
func TestMergeIdempotencyWithUpdateNodeBy(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Create :Okta node via UpdateNodeBy
	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "MID_001",
			}), graph.StringKind("Okta")),
			IdentityKind:       graph.StringKind("Okta"),
			IdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err)

	// Now try MERGE (n:Okta {objectid: 'MID_001'}) — should find existing node
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		res := tx.Raw("MERGE (n:Okta {objectid: 'MID_001'}) RETURN id(n), labels(n)", nil)
		defer res.Close()
		for res.Next() {
			t.Logf("MERGE result: %v", res.Values())
		}
		return res.Error()
	})
	require.NoError(t, err)

	count := queryCount(ctx, t, db, "MATCH (n {objectid: 'MID_001'}) RETURN count(n)")
	t.Logf("Count: %d (expected 1 — MERGE should find UpdateNodeBy-created node)", count)
	assert.Equal(t, 1, count, "MERGE should be idempotent with UpdateNodeBy")
}

// TestMergeIdempotencyUpdateNodeByThenSlowPath checks whether a slow-path
// triple-MERGE creates a duplicate :Okta node when a node already exists via UpdateNodeBy.
func TestMergeIdempotencyUpdateNodeByThenSlowPath(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Create :Okta node via UpdateNodeBy (parameterized)
	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "SLOW_001",
				"name":     "Test",
			}), graph.StringKind("Okta"), graph.StringKind("Okta_User")),
			IdentityKind:       graph.StringKind("Okta"),
			IdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err)

	// Now simulate slow-path: MERGE (s:Okta {objectid: $p}) — same label, same OID
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		res := tx.Raw(
			"MERGE (s:`Okta` {objectid: $si_objectid}) MERGE (e:`Base` {objectid: $ei_objectid}) "+
				"MERGE (s)-[r:`TestRel`]->(e)",
			map[string]any{
				"si_objectid": "SLOW_001",
				"ei_objectid": "END_001",
			},
		)
		defer res.Close()
		return res.Error()
	})
	require.NoError(t, err)

	count := queryCount(ctx, t, db, "MATCH (n {objectid: 'SLOW_001'}) RETURN count(n)")
	labels := queryString(ctx, t, db, "MATCH (n {objectid: 'SLOW_001'}) RETURN id(n), labels(n)")
	t.Logf("Count: %d (expected 1)", count)
	t.Logf("Labels:\n%s", labels)
	assert.Equal(t, 1, count, "MERGE :Okta should find the UpdateNodeBy-created :Okta node")
}

// TestMergeCreatedNodeFindable checks if a MERGE-created node can be found by
// a subsequent MERGE with the same label+objectid.
func TestMergeCreatedNodeFindable(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// First MERGE — creates a new :Base node
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		res := tx.Raw("MERGE (n:Base {objectid: 'MCF_001'})", nil)
		defer res.Close()
		return res.Error()
	})
	require.NoError(t, err)

	// Second MERGE — should find the existing :Base node
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		res := tx.Raw("MERGE (n:Base {objectid: 'MCF_001'})", nil)
		defer res.Close()
		return res.Error()
	})
	require.NoError(t, err)

	count := queryCount(ctx, t, db, "MATCH (n {objectid: 'MCF_001'}) RETURN count(n)")
	t.Logf("Count: %d (expected 1)", count)
	assert.Equal(t, 1, count, "Second MERGE should find the first MERGE's node")
}

// TestWhereClauseBugMerge verifies WHERE works with MERGE-created nodes.
func TestWhereClauseBugMerge(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Create node via UpdateNodeBy, then a MERGE stub via raw Cypher
	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "WC_001",
				"name":     "Test",
			}), graph.StringKind("Okta"), graph.StringKind("Okta_User")),
			IdentityKind:       graph.StringKind("Okta"),
			IdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err)

	// Create a :Base stub via raw MERGE
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		res := tx.Raw("MERGE (n:Base {objectid: 'WC_001'})", nil)
		defer res.Close()
		return res.Error()
	})
	require.NoError(t, err)

	// Check counts
	inlineCount := queryCount(ctx, t, db, "MATCH (n {objectid: 'WC_001'}) RETURN count(n)")
	whereCount := queryCount(ctx, t, db, "MATCH (n) WHERE n.objectid = 'WC_001' RETURN count(n)")
	total := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")

	t.Logf("Inline: %d, WHERE: %d, Total: %d", inlineCount, whereCount, total)

	allInline := queryString(ctx, t, db, "MATCH (n {objectid: 'WC_001'}) RETURN id(n), labels(n)")
	allWhere := queryString(ctx, t, db, "MATCH (n) WHERE n.objectid = 'WC_001' RETURN id(n), labels(n)")

	t.Logf("Inline:\n%s", allInline)
	t.Logf("WHERE:\n%s", allWhere)

	assert.Equal(t, 2, inlineCount, "Should have 2 nodes: Okta + Base")
	assert.Equal(t, 2, whereCount, "WHERE should also find 2 nodes")
}

// TestWhereClauseBug verifies the WHERE clause bug doesn't affect MERGE-created nodes.
func TestWhereClauseBug(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Create nodes via MERGE with different labels but same objectid
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		res := tx.Raw("CREATE (a:Okta {objectid: 'X1'}) CREATE (b:Base {objectid: 'X1'}) CREATE (c:SCIM {objectid: 'X1'}) RETURN id(a), id(b), id(c)", nil)
		defer res.Close()
		for res.Next() {
			t.Logf("Created: %v", res.Values())
		}
		return res.Error()
	})
	require.NoError(t, err)

	// Count using inline property (correct)
	inlineCount := queryCount(ctx, t, db, "MATCH (n {objectid: 'X1'}) RETURN count(n)")
	// Count using WHERE (potentially buggy)
	whereCount := queryCount(ctx, t, db, "MATCH (n) WHERE n.objectid = 'X1' RETURN count(n)")
	// Count total
	totalCount := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")

	t.Logf("Inline prop count: %d (expected 3)", inlineCount)
	t.Logf("WHERE count: %d (expected 3)", whereCount)
	t.Logf("Total count: %d (expected 3)", totalCount)

	assert.Equal(t, 3, inlineCount, "Inline prop should find all 3 nodes")
	assert.Equal(t, 3, whereCount, "WHERE should find all 3 nodes")
	assert.Equal(t, inlineCount, whereCount, "WHERE and inline should return same count")
}

// TestMergeDuplicateInBatch checks if two MERGE statements for the same
// label+objectid in the same CypherBatchExec create one or two nodes.
func TestMergeDuplicateInBatch(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Run two triple-MERGEs for the same objectid "SHARED" in a single batch
	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		// First MERGE statement
		if err := batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Start: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "SHARED",
				"lastseen": "2025-01-01",
			}), graph.StringKind("Base")),
			StartIdentityKind:       graph.StringKind("Base"),
			StartIdentityProperties: []string{"objectid"},
			End: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "END_1",
				"lastseen": "2025-01-01",
			}), graph.StringKind("Base")),
			EndIdentityKind:       graph.StringKind("Base"),
			EndIdentityProperties: []string{"objectid"},
			Relationship: graph.PrepareRelationship(graph.AsProperties(map[string]any{}), graph.StringKind("Rel1")),
		}); err != nil {
			return err
		}
		// Second MERGE statement — same start objectid
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Start: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "SHARED",
				"lastseen": "2025-01-01",
			}), graph.StringKind("Base")),
			StartIdentityKind:       graph.StringKind("Base"),
			StartIdentityProperties: []string{"objectid"},
			End: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "END_2",
				"lastseen": "2025-01-01",
			}), graph.StringKind("Base")),
			EndIdentityKind:       graph.StringKind("Base"),
			EndIdentityProperties: []string{"objectid"},
			Relationship: graph.PrepareRelationship(graph.AsProperties(map[string]any{}), graph.StringKind("Rel2")),
		})
	})
	require.NoError(t, err)

	// SHARED should appear exactly once (both MERGEs target the same label + objectid)
	sharedCount := queryCount(ctx, t, db, "MATCH (n {objectid: 'SHARED'}) RETURN count(n)")
	allNodes := queryString(ctx, t, db, "MATCH (n) RETURN n.objectid, labels(n) ORDER BY n.objectid")
	t.Logf("SHARED count: %d (expected 1)", sharedCount)
	t.Logf("All nodes:\n%s", allNodes)
	assert.Equal(t, 1, sharedCount, "Two MERGEs for same label+objectid should create ONE node")
}

// TestBatchMergeLabel tests that UpdateRelationshipBy creates the correct :Base
// stub nodes when a cross-platform edge references a node that was ingested with
// a different identity kind (e.g., Okta).
func TestBatchMergeLabel(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Step 1: Create an :Okta node via UpdateNodeBy
	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "USER_A",
				"name":     "User A",
			}), graph.StringKind("Okta"), graph.StringKind("Okta_User")),
			IdentityKind:       graph.StringKind("Okta"),
			IdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err)

	allBefore := queryString(ctx, t, db, "MATCH (n) RETURN id(n), n.objectid, labels(n)")
	t.Logf("After step 1:\n%s", allBefore)

	// Step 2: Raw triple-MERGE simulating what the slow path generates
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		res := tx.Raw(
			"MERGE (s:`Base` {objectid: $si_objectid}) "+
				"MERGE (e:`Base` {objectid: $ei_objectid}) "+
				"MERGE (s)-[r:`TestRel`]->(e) "+
				"SET s.lastseen = '2025-01-01', e.lastseen = '2025-01-01' "+
				"RETURN id(s), labels(s), id(e), labels(e)",
			map[string]any{
				"si_objectid": "USER_A",
				"ei_objectid": "TARGET_A",
			},
		)
		defer res.Close()
		if res.Error() != nil {
			return res.Error()
		}
		for res.Next() {
			t.Logf("MERGE result: %v", res.Values())
		}
		return res.Error()
	})
	require.NoError(t, err)

	allAfter := queryString(ctx, t, db, "MATCH (n) RETURN id(n), n.objectid, labels(n)")
	t.Logf("After step 2 (raw triple-MERGE):\n%s", allAfter)

	userACount := queryCount(ctx, t, db, "MATCH (n) WHERE n.objectid = 'USER_A' RETURN count(n)")
	t.Logf("USER_A count (WHERE =): %d (expected 2: Okta + Base stub)", userACount)

	// Try different query forms to isolate the bug
	userACount2 := queryCount(ctx, t, db, "MATCH (n {objectid: 'USER_A'}) RETURN count(n)")
	t.Logf("USER_A count (inline prop): %d (expected 2)", userACount2)

	userACount3 := queryCount(ctx, t, db, "MATCH (n:Base) WHERE n.objectid = 'USER_A' RETURN count(n)")
	t.Logf("USER_A count (Base WHERE =): %d (expected 1)", userACount3)

	userACount4 := queryCount(ctx, t, db, "MATCH (n:Okta) WHERE n.objectid = 'USER_A' RETURN count(n)")
	t.Logf("USER_A count (Okta WHERE =): %d (expected 1)", userACount4)

	// Check all nodes matching USER_A with various strategies
	allUserA := queryString(ctx, t, db, "MATCH (n) WHERE n.objectid = 'USER_A' RETURN id(n), labels(n)")
	t.Logf("All USER_A (WHERE =):\n%s", allUserA)

	allUserA2 := queryString(ctx, t, db, "MATCH (n {objectid: 'USER_A'}) RETURN id(n), labels(n)")
	t.Logf("All USER_A (inline prop):\n%s", allUserA2)

	totalCount := queryCount(ctx, t, db, "MATCH (n) RETURN count(n)")
	t.Logf("Total nodes: %d (expected 3)", totalCount)

	// In Neo4j: MERGE (s:Base {objectid: 'USER_A'}) would create a new :Base node
	// because the existing node is :Okta, not :Base.
	assert.Equal(t, 2, userACount,
		"USER_A should have 2 nodes: :Okta (from step 1) + :Base stub (from MERGE)")
}
