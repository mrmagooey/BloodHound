//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

var testNodeKind = graph.StringKind("TestNode")
var testEdgeKind = graph.StringKind("TestEdge")
var altEdgeKind = graph.StringKind("AltEdge")

// TestShortestPathBasic creates A->B->C and queries shortestPath(A->C).
func TestShortestPathBasic(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)
	ids := createChain(ctx, t, db, testNodeKind, testEdgeKind, 3)

	cypher := fmt.Sprintf(
		`MATCH p = shortestPath((a)-[*1..10]->(c)) WHERE id(a) = %d AND id(c) = %d RETURN p`,
		ids[0], ids[2])

	rows := runQueryRowCount(ctx, t, db, cypher)
	require.Equal(t, 1, rows, "expected exactly one shortest path")
}

// TestShortestPathNoPath creates two disconnected nodes and verifies no path is found.
func TestShortestPathNoPath(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)
	ids := createNodes(ctx, t, db, testNodeKind, 2) // disconnected

	cypher := fmt.Sprintf(
		`MATCH p = shortestPath((a)-[*1..10]->(c)) WHERE id(a) = %d AND id(c) = %d RETURN p`,
		ids[0], ids[1])

	rows := runQueryRowCount(ctx, t, db, cypher)
	require.Equal(t, 0, rows, "expected no path between disconnected nodes")
}

// TestShortestPathMultiHop creates A->B->C->D->E and queries shortestPath(A->E).
func TestShortestPathMultiHop(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)
	ids := createChain(ctx, t, db, testNodeKind, testEdgeKind, 5)

	cypher := fmt.Sprintf(
		`MATCH p = shortestPath((a)-[*1..10]->(c)) WHERE id(a) = %d AND id(c) = %d RETURN p`,
		ids[0], ids[4])

	rows := runQueryRowCount(ctx, t, db, cypher)
	require.Equal(t, 1, rows, "expected exactly one shortest path")
}

// TestShortestPathWithKindFilter creates two paths A->B->C (via TestEdge) and A->C (via AltEdge),
// then queries with a kind filter to ensure only the filtered path is returned.
func TestShortestPathWithKindFilter(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	ids := createNodes(ctx, t, db, testNodeKind, 3)

	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		// A -> B via TestEdge
		if _, err := tx.CreateRelationshipByIDs(ids[0], ids[1], testEdgeKind, graph.NewProperties()); err != nil {
			return err
		}
		// B -> C via TestEdge
		if _, err := tx.CreateRelationshipByIDs(ids[1], ids[2], testEdgeKind, graph.NewProperties()); err != nil {
			return err
		}
		// A -> C via AltEdge (direct shortcut)
		if _, err := tx.CreateRelationshipByIDs(ids[0], ids[2], altEdgeKind, graph.NewProperties()); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)

	// Query with TestEdge filter only — should find the 2-hop path, not the AltEdge shortcut
	cypher := fmt.Sprintf(
		`MATCH p = shortestPath((a)-[:TestEdge*1..10]->(c)) WHERE id(a) = %d AND id(c) = %d RETURN p`,
		ids[0], ids[2])

	rows := runQueryRowCount(ctx, t, db, cypher)
	require.Equal(t, 1, rows, "expected path via TestEdge edges")
}

// TestVariableLengthPathBounded creates a chain and tests bounded variable-length paths.
func TestVariableLengthPathBounded(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)
	ids := createChain(ctx, t, db, testNodeKind, testEdgeKind, 5) // A->B->C->D->E

	// From A, find all nodes reachable in 1..2 hops
	cypher := fmt.Sprintf(
		`MATCH (a)-[:TestEdge*1..2]->(x) WHERE id(a) = %d RETURN count(x) AS c`, ids[0])
	got := runQueryInt64(ctx, t, db, cypher)
	require.Equal(t, int64(2), got, "expected 2 nodes reachable in 1-2 hops (B, C)")
}

// TestAllShortestPathsParsing verifies that allShortestPaths() syntax parses without error.
func TestAllShortestPathsParsing(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)
	ids := createChain(ctx, t, db, testNodeKind, testEdgeKind, 3)

	cypher := fmt.Sprintf(
		`MATCH p = allShortestPaths((a)-[*1..10]->(c)) WHERE id(a) = %d AND id(c) = %d RETURN p`,
		ids[0], ids[2])

	rows := runQueryRowCount(ctx, t, db, cypher)
	require.Equal(t, 1, rows, "allShortestPaths should parse and return a path")
}
