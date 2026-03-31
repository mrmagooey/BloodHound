//go:build e2e

package e2e_test

import (
	"context"
	"testing"

	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
	"github.com/stretchr/testify/require"
)

var driverNodeKind = graph.StringKind("DriverTestNode")
var driverEdgeKind = graph.StringKind("DriverTestEdge")
var driverAltKind = graph.StringKind("DriverAltKind")

// --- Database Interface ---

func TestDriverOpenClose(t *testing.T) {
	db := openGraph(t)
	require.NotNil(t, db)
	// Cleanup registered via t.Cleanup in openGraph
}

func TestDriverReadWriteTransaction(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Write a node
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateNode(
			graph.AsProperties(map[string]any{"name": "rw_test", "objectid": "RW-1"}),
			driverNodeKind,
		)
		return err
	})
	require.NoError(t, err)

	// Read it back by property
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:DriverTestNode) WHERE n.objectid = 'RW-1' RETURN n.name AS name", nil)
		defer result.Close()
		require.NoError(t, result.Error())
		require.True(t, result.Next(), "expected to find the created node")
		vals := result.Values()
		require.Equal(t, "rw_test", vals[0])
		return result.Error()
	})
	require.NoError(t, err)
}

func TestDriverBatchOperation(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		for i := 0; i < 10; i++ {
			node := graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "BATCH-" + string(rune('A'+i)),
			}), driverNodeKind)
			if err := batch.CreateNode(node); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db, "MATCH (n:DriverTestNode) RETURN count(n) AS c")
	require.Equal(t, int64(10), count)
}

// --- Transaction Interface ---

func TestTxCreateNode(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	var node *graph.Node
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		var err error
		node, err = tx.CreateNode(
			graph.AsProperties(map[string]any{
				"name":     "create_test",
				"objectid": "CT-1",
				"enabled":  true,
			}),
			driverNodeKind,
		)
		return err
	})
	require.NoError(t, err)
	require.NotNil(t, node)
	// Note: kglite CreateNode may return ID=0; verify node exists via query instead
	count := runQueryInt64(ctx, t, db, "MATCH (n:DriverTestNode) WHERE n.objectid = 'CT-1' RETURN count(n) AS c")
	require.Equal(t, int64(1), count)
}

func TestTxCreateRelationship(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	ids := createNodes(ctx, t, db, driverNodeKind, 2)

	var rel *graph.Relationship
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		var err error
		rel, err = tx.CreateRelationshipByIDs(ids[0], ids[1], driverEdgeKind,
			graph.AsProperties(map[string]any{"weight": 42}))
		return err
	})
	require.NoError(t, err)
	require.NotNil(t, rel)

	// Verify the edge exists (rel.ID may be 0 in kglite)
	count := runQueryInt64(ctx, t, db, "MATCH ()-[r:DriverTestEdge]->() RETURN count(r) AS c")
	require.Equal(t, int64(1), count)
}

func TestTxRawCypher(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)
	createNodes(ctx, t, db, driverNodeKind, 5)

	var total int64
	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:DriverTestNode) RETURN count(n) AS c", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next())
		vals := result.Values()
		switch v := vals[0].(type) {
		case int64:
			total = v
		case float64:
			total = int64(v)
		}
		return result.Error()
	})
	require.NoError(t, err)
	require.Equal(t, int64(5), total)
}

func TestTxNodesQuery(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)
	createNodes(ctx, t, db, driverNodeKind, 3)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		count, err := tx.Nodes().Filterf(func() graph.Criteria {
			return query.KindIn(query.Node(), driverNodeKind)
		}).Count()
		require.NoError(t, err)
		require.Equal(t, int64(3), count)
		return nil
	})
	require.NoError(t, err)
}

func TestTxRelationshipsQuery(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	ids := createChain(ctx, t, db, driverNodeKind, driverEdgeKind, 4) // 3 edges
	_ = ids

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		count, err := tx.Relationships().Filterf(func() graph.Criteria {
			return query.KindIn(query.Relationship(), driverEdgeKind)
		}).Count()
		require.NoError(t, err)
		require.Equal(t, int64(3), count)
		return nil
	})
	require.NoError(t, err)
}

// --- Batch Interface ---

func TestBatchUpdateNodeBy(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// First create a node
	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "UPSERT-1",
				"name":     "original",
			}), driverNodeKind),
			IdentityKind:       driverNodeKind,
			IdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err)

	// Upsert with updated name
	err = db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "UPSERT-1",
				"name":     "updated",
			}), driverNodeKind),
			IdentityKind:       driverNodeKind,
			IdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err)

	// Should still be 1 node, with updated name
	count := runQueryInt64(ctx, t, db, "MATCH (n:DriverTestNode) RETURN count(n) AS c")
	require.Equal(t, int64(1), count)

	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw(`MATCH (n:DriverTestNode) WHERE n.objectid = 'UPSERT-1' RETURN n.name AS name`, nil)
		defer result.Close()
		require.NoError(t, result.Error())
		require.True(t, result.Next())
		require.Equal(t, "updated", result.Values()[0])
		return result.Error()
	})
	require.NoError(t, err)
}
