//go:build e2e

package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	kglitedawgs "github.com/specterops/bloodhound/packages/go/kglite/dawgs"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

// TestDriverCoverageReadTransaction opens a graph, writes data, then uses ReadTransaction to count.
func TestDriverCoverageReadTransaction(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Write some nodes
	createNodes(ctx, t, db, driverNodeKind, 3)

	// ReadTransaction with a count query
	count := runQueryInt64(ctx, t, db, "MATCH (n:DriverTestNode) RETURN count(n) AS c")
	require.Equal(t, int64(3), count)
}

// TestDriverCoverageWriteTransaction creates nodes via WriteTransaction and verifies they exist.
func TestDriverCoverageWriteTransaction(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		for i := 0; i < 5; i++ {
			_, err := tx.CreateNode(
				graph.AsProperties(map[string]any{
					"objectid": "WT-" + string(rune('A'+i)),
				}),
				driverNodeKind,
			)
			if err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db, "MATCH (n:DriverTestNode) RETURN count(n) AS c")
	require.Equal(t, int64(5), count)
}

// TestDriverCoverageCloseSave opens a graph with a file path, writes data, closes, and verifies the file exists.
func TestDriverCoverageCloseSave(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "close_test.kgl")

	db, err := kglitedawgs.Open(graphPath)
	require.NoError(t, err)

	// Write data
	err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateNode(
			graph.AsProperties(map[string]any{"objectid": "CLOSE-1"}),
			driverNodeKind,
		)
		return err
	})
	require.NoError(t, err)

	// Close should save to disk
	err = db.Close(ctx)
	require.NoError(t, err)

	// Verify the file exists on disk
	_, err = os.Stat(graphPath)
	require.NoError(t, err, "graph file should exist after Close")
}

// TestDriverCoverageSetWriteFlushSize calls SetWriteFlushSize and verifies no panic.
func TestDriverCoverageSetWriteFlushSize(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "flush_test.kgl")
	db, err := kglitedawgs.Open(graphPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close(context.Background()) })

	// Should not panic
	db.SetWriteFlushSize(50000)
}

// TestDriverCoverageSetBatchWriteSize calls SetBatchWriteSize and verifies no panic.
func TestDriverCoverageSetBatchWriteSize(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "batch_size_test.kgl")
	db, err := kglitedawgs.Open(graphPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close(context.Background()) })

	// Should not panic
	db.SetBatchWriteSize(10000)
}

// TestDriverCoverageSetDefaultGraph calls SetDefaultGraph and verifies no error.
func TestDriverCoverageSetDefaultGraph(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.SetDefaultGraph(ctx, graph.Graph{
		Name: "test_graph",
	})
	require.NoError(t, err)
}

// TestDriverCoverageAssertSchema calls AssertSchema with a schema and verifies no error.
func TestDriverCoverageAssertSchema(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	schema := graph.Schema{
		Graphs: []graph.Graph{
			{
				Name:  "default",
				Nodes: graph.Kinds{driverNodeKind},
				Edges: graph.Kinds{driverEdgeKind},
			},
		},
	}
	err := db.AssertSchema(ctx, schema)
	require.NoError(t, err)
}

// TestDriverCoverageFetchKinds calls FetchKinds and verifies it returns without error.
func TestDriverCoverageFetchKinds(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Write some typed nodes first
	createNodes(ctx, t, db, driverNodeKind, 2)

	kinds, err := db.FetchKinds(ctx)
	require.NoError(t, err)
	// FetchKinds returns empty kinds in kglite (tracks kinds dynamically)
	require.NotNil(t, kinds)
}

// TestDriverCoverageRefreshKinds calls RefreshKinds and verifies no error.
func TestDriverCoverageRefreshKinds(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Add some nodes first
	createNodes(ctx, t, db, driverNodeKind, 2)
	createNodes(ctx, t, db, driverAltKind, 3)

	err := db.RefreshKinds(ctx)
	require.NoError(t, err)
}

// TestDriverCoverageRun executes a raw Cypher query via Run.
func TestDriverCoverageRun(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Create nodes first
	createNodes(ctx, t, db, driverNodeKind, 3)

	// Run a raw query (discards results)
	err := db.Run(ctx, "MATCH (n:DriverTestNode) RETURN count(n) AS c", nil)
	require.NoError(t, err)
}

// TestDriverCoverageExplicitSave calls Save explicitly (not via Close).
func TestDriverCoverageExplicitSave(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "save_test.kgl")

	drv, err := kglitedawgs.Open(graphPath)
	require.NoError(t, err)
	t.Cleanup(func() { drv.Close(ctx) })

	// Write data
	err = drv.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateNode(
			graph.AsProperties(map[string]any{"objectid": "SAVE-1"}),
			driverNodeKind,
		)
		return err
	})
	require.NoError(t, err)

	// Explicit save via the concrete Driver type
	err = drv.Save()
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(graphPath)
	require.NoError(t, err, "graph file should exist after explicit Save")
}

// TestDriverCoverageSaveNoPath verifies Save is a no-op when no path is set.
func TestDriverCoverageSaveNoPath(t *testing.T) {
	drv, err := kglitedawgs.Open("")
	require.NoError(t, err)
	t.Cleanup(func() { drv.Close(context.Background()) })

	err = drv.Save()
	require.NoError(t, err, "Save with no path should be a no-op")
}

// TestDriverCoverageCloseNilKg verifies Close is safe to call on an already-closed driver.
func TestDriverCoverageCloseNilKg(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "double_close.kgl")

	db, err := kglitedawgs.Open(graphPath)
	require.NoError(t, err)

	// First close
	err = db.Close(ctx)
	require.NoError(t, err)

	// Second close should be safe (kg is nil)
	err = db.Close(ctx)
	require.NoError(t, err)
}
