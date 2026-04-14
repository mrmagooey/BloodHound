//go:build e2e

package repro

import (
	"context"
	"testing"

	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMergeIdempotency tests that MERGE (n:Okta {objectid: "X"}) is idempotent
// within a single BatchOperation and across multiple flushes.
func TestMergeIdempotency(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	const N = 10 // repeat MERGE N times

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		for i := 0; i < N; i++ {
			update := graph.NodeUpdate{
				Node: graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": "TEST-OID-001",
					"lastseen": "2025-01-01",
				}), graph.StringKind("Okta"), graph.StringKind("Okta_User")),
				IdentityKind:       graph.StringKind("Okta"),
				IdentityProperties: []string{"objectid"},
			}
			if err := batch.UpdateNodeBy(update); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	count := queryCount(ctx, t, db, "MATCH (n:Okta) WHERE n.objectid = 'TEST-OID-001' RETURN count(n)")
	t.Logf("Node count for TEST-OID-001: %d (expected 1)", count)
	assert.Equal(t, 1, count, "MERGE should be idempotent — only 1 node should exist")
}

// TestMergeIdempotencyAcrossFlushes tests MERGE idempotency when nodes are
// created in separate batch flushes (simulating multi-file ingest).
func TestMergeIdempotencyAcrossFlushes(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// First BatchOperation: create the node
	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		update := graph.NodeUpdate{
			Node: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "TEST-OID-002",
			}), graph.StringKind("Okta"), graph.StringKind("Okta_User")),
			IdentityKind:       graph.StringKind("Okta"),
			IdentityProperties: []string{"objectid"},
		}
		return batch.UpdateNodeBy(update)
	})
	require.NoError(t, err)

	// Second BatchOperation: same MERGE for same objectid
	err = db.BatchOperation(ctx, func(batch graph.Batch) error {
		update := graph.NodeUpdate{
			Node: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "TEST-OID-002",
			}), graph.StringKind("Okta"), graph.StringKind("Okta_User")),
			IdentityKind:       graph.StringKind("Okta"),
			IdentityProperties: []string{"objectid"},
		}
		return batch.UpdateNodeBy(update)
	})
	require.NoError(t, err)

	count := queryCount(ctx, t, db, "MATCH (n:Okta) WHERE n.objectid = 'TEST-OID-002' RETURN count(n)")
	t.Logf("Node count for TEST-OID-002 across BatchOperations: %d (expected 1)", count)
	assert.Equal(t, 1, count, "MERGE should be idempotent across BatchOperations")
}

// TestMergeIdempotencyWithSET tests that MERGE + SET is idempotent.
func TestMergeIdempotencyWithSET(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		for i := 0; i < 5; i++ {
			update := graph.NodeUpdate{
				Node: graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": "TEST-OID-003",
					"name":     "Test User",
					"enabled":  true,
				}), graph.StringKind("Okta"), graph.StringKind("Okta_User")),
				IdentityKind:       graph.StringKind("Okta"),
				IdentityProperties: []string{"objectid"},
			}
			if err := batch.UpdateNodeBy(update); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	count := queryCount(ctx, t, db, "MATCH (n:Okta) WHERE n.objectid = 'TEST-OID-003' RETURN count(n)")
	t.Logf("Node count for TEST-OID-003 (with SET): %d (expected 1)", count)
	assert.Equal(t, 1, count)
}
