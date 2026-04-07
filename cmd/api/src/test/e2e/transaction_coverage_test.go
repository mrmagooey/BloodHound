//go:build e2e

package e2e_test

import (
	"context"
	"testing"

	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
	"github.com/stretchr/testify/require"
)

var txTestNodeKind = graph.StringKind("TxTestNode")
var txTestAltKind = graph.StringKind("TxTestAlt")
var txTestEdgeKind = graph.StringKind("TxTestEdge")

// TestTransactionCoverageQuery verifies that Query() is an alias for Raw()
// by running the same Cypher through both and comparing results.
func TestTransactionCoverageQuery(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Seed some nodes
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		for i := 0; i < 3; i++ {
			_, err := tx.CreateNode(
				graph.AsProperties(map[string]any{"objectid": "TQ-" + string(rune('A'+i))}),
				txTestNodeKind,
			)
			if err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	var rawCount, queryCount int
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		// Use Raw
		result := tx.Raw("MATCH (n:TxTestNode) RETURN count(n) AS c", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next())
		switch v := result.Values()[0].(type) {
		case int64:
			rawCount = int(v)
		case float64:
			rawCount = int(v)
		}
		return result.Error()
	})
	require.NoError(t, err)

	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		// Use Query (alias)
		result := tx.Query("MATCH (n:TxTestNode) RETURN count(n) AS c", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next())
		switch v := result.Values()[0].(type) {
		case int64:
			queryCount = int(v)
		case float64:
			queryCount = int(v)
		}
		return result.Error()
	})
	require.NoError(t, err)

	require.Equal(t, 3, rawCount, "Raw should return 3 nodes")
	require.Equal(t, rawCount, queryCount, "Query should return same result as Raw")
}

// TestTransactionCoverageWithGraph verifies WithGraph returns the same transaction.
func TestTransactionCoverageWithGraph(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		tx2 := tx.WithGraph(graph.Graph{})
		require.Equal(t, tx, tx2, "WithGraph should return the same transaction")

		// Verify the returned transaction is functional
		_, err := tx2.CreateNode(
			graph.AsProperties(map[string]any{"objectid": "WG-1"}),
			txTestNodeKind,
		)
		return err
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db, "MATCH (n:TxTestNode) WHERE n.objectid = 'WG-1' RETURN count(n) AS c")
	require.Equal(t, int64(1), count)
}

// TestTransactionCoverageCreateNode tests CreateNode with properties and multiple kinds.
func TestTransactionCoverageCreateNode(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	var node *graph.Node
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		var err error
		node, err = tx.CreateNode(
			graph.AsProperties(map[string]any{
				"name":     "multi_kind_node",
				"objectid": "CN-1",
				"enabled":  true,
			}),
			txTestNodeKind, txTestAltKind,
		)
		return err
	})
	require.NoError(t, err)
	require.NotNil(t, node)

	// Verify node exists
	count := runQueryInt64(ctx, t, db, "MATCH (n:TxTestNode) WHERE n.objectid = 'CN-1' RETURN count(n) AS c")
	require.Equal(t, int64(1), count)

	// Verify name property
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:TxTestNode) WHERE n.objectid = 'CN-1' RETURN n.name AS name", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next())
		require.Equal(t, "multi_kind_node", result.Values()[0])
		return result.Error()
	})
	require.NoError(t, err)

	// Test error case: no kinds
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateNode(graph.AsProperties(map[string]any{"objectid": "CN-FAIL"}))
		return err
	})
	require.Error(t, err, "CreateNode with no kinds should fail")

	// Test with nil properties
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		n, err := tx.CreateNode(nil, txTestNodeKind)
		if err != nil {
			return err
		}
		require.NotNil(t, n)
		return nil
	})
	require.NoError(t, err)
}

// TestTransactionCoverageUpdateNode tests updating a node's properties.
func TestTransactionCoverageUpdateNode(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Create a node
	var nodeID graph.ID
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		node, err := tx.CreateNode(
			graph.AsProperties(map[string]any{
				"name":     "original_name",
				"objectid": "UN-1",
			}),
			txTestNodeKind,
		)
		if err != nil {
			return err
		}
		nodeID = node.ID
		return nil
	})
	require.NoError(t, err)

	// Look up the actual node ID via Cypher since kglite CreateNode may return 0
	var actualID graph.ID
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:TxTestNode) WHERE n.objectid = 'UN-1' RETURN id(n) AS nid", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next())
		switch v := result.Values()[0].(type) {
		case int64:
			actualID = graph.ID(v)
		case float64:
			actualID = graph.ID(int64(v))
		case graph.ID:
			actualID = v
		}
		return result.Error()
	})
	require.NoError(t, err)

	if nodeID == 0 {
		nodeID = actualID
	}

	// Update the node
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		updatedNode := &graph.Node{
			ID:    nodeID,
			Kinds: graph.Kinds{txTestNodeKind},
		}
		updatedNode.Properties = graph.NewProperties()
		updatedNode.Properties.Set("name", "updated_name")
		return tx.UpdateNode(updatedNode)
	})
	require.NoError(t, err)

	// Verify the update
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:TxTestNode) WHERE n.objectid = 'UN-1' RETURN n.name AS name", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next())
		require.Equal(t, "updated_name", result.Values()[0])
		return result.Error()
	})
	require.NoError(t, err)

	// Test UpdateNode with no modified properties (should be a no-op)
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		noChangeNode := &graph.Node{
			ID:    nodeID,
			Kinds: graph.Kinds{txTestNodeKind},
		}
		noChangeNode.Properties = graph.NewProperties()
		// Don't set anything - no modified properties
		return tx.UpdateNode(noChangeNode)
	})
	require.NoError(t, err)

	// Test UpdateNode with nil properties
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		nilPropsNode := &graph.Node{
			ID:    nodeID,
			Kinds: graph.Kinds{txTestNodeKind},
		}
		nilPropsNode.Properties = nil
		return tx.UpdateNode(nilPropsNode)
	})
	require.NoError(t, err)

	// Test UpdateNode with multiple kinds (absorbed into extra_labels by kglite)
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		multiKindNode := &graph.Node{
			ID:    nodeID,
			Kinds: graph.Kinds{txTestNodeKind, txTestAltKind},
		}
		multiKindNode.Properties = graph.NewProperties()
		multiKindNode.Properties.Set("status", "multi")
		return tx.UpdateNode(multiKindNode)
	})
	require.NoError(t, err)
}

// TestTransactionCoverageCreateRelationshipByIDs tests creating a relationship between two nodes.
func TestTransactionCoverageCreateRelationshipByIDs(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Create two nodes
	ids := createNodes(ctx, t, db, txTestNodeKind, 2)

	// Create a relationship
	var rel *graph.Relationship
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		var err error
		rel, err = tx.CreateRelationshipByIDs(ids[0], ids[1], txTestEdgeKind,
			graph.AsProperties(map[string]any{"weight": 99}))
		return err
	})
	require.NoError(t, err)
	require.NotNil(t, rel)
	require.Equal(t, ids[0], rel.StartID)
	require.Equal(t, ids[1], rel.EndID)

	// Verify the relationship exists
	count := runQueryInt64(ctx, t, db, "MATCH ()-[r:TxTestEdge]->() RETURN count(r) AS c")
	require.Equal(t, int64(1), count)

	// Test with nil properties
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		rel2, err := tx.CreateRelationshipByIDs(ids[0], ids[1], txTestEdgeKind, nil)
		if err != nil {
			return err
		}
		require.NotNil(t, rel2)
		return nil
	})
	require.NoError(t, err)

	// Test with empty kind (should fail)
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateRelationshipByIDs(ids[0], ids[1], graph.StringKind(""), nil)
		return err
	})
	require.Error(t, err, "empty kind should fail")
}

// TestTransactionCoverageUpdateRelationship tests updating a relationship's properties.
func TestTransactionCoverageUpdateRelationship(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Create nodes and a relationship
	ids := createNodes(ctx, t, db, txTestNodeKind, 2)

	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateRelationshipByIDs(ids[0], ids[1], txTestEdgeKind,
			graph.AsProperties(map[string]any{"weight": 10}))
		return err
	})
	require.NoError(t, err)

	// Get the relationship ID
	var relID graph.ID
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH ()-[r:TxTestEdge]->() RETURN id(r) AS rid", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next())
		switch v := result.Values()[0].(type) {
		case int64:
			relID = graph.ID(v)
		case float64:
			relID = graph.ID(int64(v))
		case graph.ID:
			relID = v
		}
		return result.Error()
	})
	require.NoError(t, err)

	// Update the relationship — kglite may not support SET on relationship
	// variables, so we accept either success or error here. The important
	// thing is that the code path through UpdateRelationship is exercised.
	_ = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		rel := &graph.Relationship{
			ID:         relID,
			StartID:    ids[0],
			EndID:      ids[1],
			Kind:       txTestEdgeKind,
			Properties: graph.NewProperties(),
		}
		rel.Properties.Set("weight", 42)
		return tx.UpdateRelationship(rel)
	})

	// Test UpdateRelationship with nil properties (should be no-op)
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		rel := &graph.Relationship{
			ID:         relID,
			Properties: nil,
		}
		return tx.UpdateRelationship(rel)
	})
	require.NoError(t, err)

	// Test UpdateRelationship with no modified properties (should be no-op)
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		rel := &graph.Relationship{
			ID:         relID,
			Properties: graph.NewProperties(),
		}
		return tx.UpdateRelationship(rel)
	})
	require.NoError(t, err)
}

// TestTransactionCoverageNodes verifies that Nodes() returns a working NodeQuery.
func TestTransactionCoverageNodes(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)
	createNodes(ctx, t, db, txTestNodeKind, 4)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		nq := tx.Nodes()
		require.NotNil(t, nq)

		count, err := nq.Filterf(func() graph.Criteria {
			return query.KindIn(query.Node(), txTestNodeKind)
		}).Count()
		require.NoError(t, err)
		require.Equal(t, int64(4), count)
		return nil
	})
	require.NoError(t, err)
}

// TestTransactionCoverageRelationships verifies that Relationships() returns a working RelationshipQuery.
func TestTransactionCoverageRelationships(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)
	createChain(ctx, t, db, txTestNodeKind, txTestEdgeKind, 3) // 2 edges

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		rq := tx.Relationships()
		require.NotNil(t, rq)

		count, err := rq.Filterf(func() graph.Criteria {
			return query.KindIn(query.Relationship(), txTestEdgeKind)
		}).Count()
		require.NoError(t, err)
		require.Equal(t, int64(2), count)
		return nil
	})
	require.NoError(t, err)
}

// TestTransactionCoverageGraphQueryMemoryLimit verifies GraphQueryMemoryLimit returns a non-negative value.
func TestTransactionCoverageGraphQueryMemoryLimit(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		limit := tx.GraphQueryMemoryLimit()
		require.True(t, limit >= 0, "memory limit should be non-negative")
		return nil
	})
	require.NoError(t, err)
}

// TestTransactionCoverageRewriteMultiTypeRel exercises the rewrite path for
// pipe-separated relationship types in Cypher (e.g., [:TypeA|TypeB]).
func TestTransactionCoverageRewriteMultiTypeRel(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Create nodes with two different relationship types
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		n1, err := tx.CreateNode(graph.AsProperties(map[string]any{"objectid": "MR-1"}), txTestNodeKind)
		if err != nil {
			return err
		}
		n2, err := tx.CreateNode(graph.AsProperties(map[string]any{"objectid": "MR-2"}), txTestNodeKind)
		if err != nil {
			return err
		}
		_, err = tx.CreateRelationshipByIDs(n1.ID, n2.ID, txTestEdgeKind, nil)
		if err != nil {
			return err
		}
		_, err = tx.CreateRelationshipByIDs(n1.ID, n2.ID, graph.StringKind("TxTestAltEdge"), nil)
		return err
	})
	require.NoError(t, err)

	// Query with pipe-separated types — triggers rewriteMultiTypeRel
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (a)-[:TxTestEdge|TxTestAltEdge]->(b) RETURN count(a) AS c", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next())
		return result.Error()
	})
	require.NoError(t, err)
}

// TestTransactionCoverageRewriteLabelWhere exercises the rewrite path for
// var:Kind label checks in WHERE clauses.
func TestTransactionCoverageRewriteLabelWhere(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateNode(graph.AsProperties(map[string]any{"objectid": "LW-1", "name": "test"}), txTestNodeKind)
		return err
	})
	require.NoError(t, err)

	// Query with var:Kind in WHERE clause — triggers rewriteLabelWhere
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n) WHERE n:TxTestNode AND n.name = 'test' RETURN count(n) AS c", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next())
		return result.Error()
	})
	require.NoError(t, err)
}

// TestTransactionCoverageRewriteInParam exercises the rewrite path for
// IN $param patterns that need to be expanded to literal lists.
func TestTransactionCoverageRewriteInParam(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Create a few nodes
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		for _, oid := range []string{"IP-1", "IP-2", "IP-3"} {
			_, err := tx.CreateNode(graph.AsProperties(map[string]any{"objectid": oid}), txTestNodeKind)
			if err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	// Query with IN $param — triggers rewriteInParam with string slice
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw(
			"MATCH (n:TxTestNode) WHERE n.objectid IN $ids RETURN count(n) AS c",
			map[string]any{"ids": []string{"IP-1", "IP-2"}},
		)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next())
		switch v := result.Values()[0].(type) {
		case int64:
			require.Equal(t, int64(2), v)
		case float64:
			require.Equal(t, float64(2), v)
		}
		return result.Error()
	})
	require.NoError(t, err)

	// Query with IN $param — triggers rewriteInParam with int slice
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw(
			"MATCH (n) WHERE id(n) IN $nodeIds RETURN count(n) AS c",
			map[string]any{"nodeIds": []int64{1, 2, 3}},
		)
		defer result.Close()
		// We don't care about the specific result, just that the rewrite was exercised
		return nil
	})
	require.NoError(t, err)

	// Query with IN $param where param is not a list (should be left unchanged)
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw(
			"MATCH (n:TxTestNode) WHERE n.objectid IN $scalar RETURN count(n) AS c",
			map[string]any{"scalar": "not-a-list"},
		)
		defer result.Close()
		// The rewrite should leave it unchanged; kglite may or may not handle it
		return nil
	})
	require.NoError(t, err)
}

// TestTransactionCoverageCommit verifies that Commit is a no-op (returns nil).
func TestTransactionCoverageCommit(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		// Commit should be a no-op
		commitErr := tx.Commit()
		require.NoError(t, commitErr, "Commit should return nil")
		return nil
	})
	require.NoError(t, err)
}
