//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

// runQueryInt64 executes a single-column count Cypher query and returns the int64 result.
// Fails the test if the query errors or returns no rows.
func runQueryInt64(ctx context.Context, t *testing.T, db graph.Database, cypher string) int64 {
	t.Helper()
	var value int64
	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw(cypher, nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		if !result.Next() {
			return fmt.Errorf("query returned no rows: %s", cypher)
		}
		vals := result.Values()
		if len(vals) == 0 {
			return fmt.Errorf("query returned row with no columns: %s", cypher)
		}
		switch v := vals[0].(type) {
		case int64:
			value = v
		case float64:
			value = int64(v)
		case int:
			value = int64(v)
		default:
			return fmt.Errorf("expected numeric result, got %T: %v", vals[0], vals[0])
		}
		return result.Error()
	})
	require.NoError(t, err, "query failed: %s", cypher)
	return value
}

// runQueryRowCount executes a Cypher query and returns the number of result rows.
func runQueryRowCount(ctx context.Context, t *testing.T, db graph.Database, cypher string) int {
	t.Helper()
	var count int
	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw(cypher, nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		for result.Next() {
			count++
		}
		return result.Error()
	})
	require.NoError(t, err, "query failed: %s", cypher)
	return count
}

// assertEdgeCount asserts that the count of relationships of the given type matches expected.
func assertEdgeCount(ctx context.Context, t *testing.T, db graph.Database, edgeType string, expected int64) {
	t.Helper()
	cypher := fmt.Sprintf("MATCH ()-[r:%s]->() RETURN count(r) AS c", edgeType)
	got := runQueryInt64(ctx, t, db, cypher)
	require.Equal(t, expected, got, "edge count mismatch for %s", edgeType)
}

// assertNodeCount asserts that the count of nodes of the given label matches expected.
func assertNodeCount(ctx context.Context, t *testing.T, db graph.Database, label string, expected int64) {
	t.Helper()
	cypher := fmt.Sprintf("MATCH (n:%s) RETURN count(n) AS c", label)
	got := runQueryInt64(ctx, t, db, cypher)
	require.Equal(t, expected, got, "node count mismatch for %s", label)
}

// createNodes creates n nodes of the given kind with sequential names, returns their IDs.
func createNodes(ctx context.Context, t *testing.T, db graph.Database, kind graph.Kind, n int) []graph.ID {
	t.Helper()
	ids := make([]graph.ID, n)
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		for i := 0; i < n; i++ {
			node, err := tx.CreateNode(
				graph.AsProperties(map[string]any{
					"name":     fmt.Sprintf("node_%d", i),
					"objectid": fmt.Sprintf("OID-%d", i),
				}),
				kind,
			)
			if err != nil {
				return err
			}
			ids[i] = node.ID
		}
		return nil
	})
	require.NoError(t, err)
	return ids
}

// createChain creates n nodes connected linearly by edgeKind, returns node IDs in order.
func createChain(ctx context.Context, t *testing.T, db graph.Database, nodeKind, edgeKind graph.Kind, n int) []graph.ID {
	t.Helper()
	ids := createNodes(ctx, t, db, nodeKind, n)
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		for i := 0; i < len(ids)-1; i++ {
			_, err := tx.CreateRelationshipByIDs(ids[i], ids[i+1], edgeKind, graph.NewProperties())
			if err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)
	return ids
}
