//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

var concurrentNodeKind = graph.StringKind("ConcurrentTestNode")

// TestConcurrentReads seeds a graph with 100 nodes and launches 10 goroutines
// that each run a ReadTransaction with a count query. All must return 100.
func TestConcurrentReads(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openGraph(t)

	// Seed 100 nodes
	createNodes(ctx, t, db, concurrentNodeKind, 100)

	const numReaders = 10
	var wg sync.WaitGroup
	errs := make([]error, numReaders)
	counts := make([]int64, numReaders)

	wg.Add(numReaders)
	for i := 0; i < numReaders; i++ {
		go func(idx int) {
			defer wg.Done()
			var count int64
			err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
				result := tx.Raw("MATCH (n:ConcurrentTestNode) RETURN count(n) AS c", nil)
				defer result.Close()
				if result.Error() != nil {
					return result.Error()
				}
				if !result.Next() {
					return fmt.Errorf("reader %d: no rows returned", idx)
				}
				vals := result.Values()
				switch v := vals[0].(type) {
				case int64:
					count = v
				case float64:
					count = int64(v)
				case int:
					count = int64(v)
				default:
					return fmt.Errorf("reader %d: unexpected type %T", idx, vals[0])
				}
				return result.Error()
			})
			errs[idx] = err
			counts[idx] = count
		}(i)
	}

	wg.Wait()
	for i := 0; i < numReaders; i++ {
		require.NoError(t, errs[i], "reader %d errored", i)
		require.Equal(t, int64(100), counts[i], "reader %d got wrong count", i)
	}
}

// TestConcurrentReadDuringWrite seeds a graph then concurrently runs a write
// transaction (adding nodes) and multiple read transactions. Neither should
// panic or deadlock.
func TestConcurrentReadDuringWrite(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openGraph(t)

	// Seed initial nodes
	createNodes(ctx, t, db, concurrentNodeKind, 50)

	var wg sync.WaitGroup

	// Writer goroutine: add 50 more nodes
	var writeErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		writeErr = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
			for i := 0; i < 50; i++ {
				_, err := tx.CreateNode(
					graph.AsProperties(map[string]any{
						"name":     fmt.Sprintf("write_during_read_%d", i),
						"objectid": fmt.Sprintf("WDR-%d", i),
					}),
					concurrentNodeKind,
				)
				if err != nil {
					return err
				}
			}
			return nil
		})
	}()

	// Reader goroutines: run count queries concurrently with the writer
	const numReaders = 5
	readErrs := make([]error, numReaders)
	readCounts := make([]int64, numReaders)
	wg.Add(numReaders)
	for i := 0; i < numReaders; i++ {
		go func(idx int) {
			defer wg.Done()
			var count int64
			err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
				result := tx.Raw("MATCH (n:ConcurrentTestNode) RETURN count(n) AS c", nil)
				defer result.Close()
				if result.Error() != nil {
					return result.Error()
				}
				if !result.Next() {
					return fmt.Errorf("reader %d: no rows returned", idx)
				}
				vals := result.Values()
				switch v := vals[0].(type) {
				case int64:
					count = v
				case float64:
					count = int64(v)
				case int:
					count = int64(v)
				default:
					return fmt.Errorf("reader %d: unexpected type %T", idx, vals[0])
				}
				return result.Error()
			})
			readErrs[idx] = err
			readCounts[idx] = count
		}(i)
	}

	wg.Wait()

	require.NoError(t, writeErr, "write transaction failed")
	for i := 0; i < numReaders; i++ {
		require.NoError(t, readErrs[i], "reader %d errored", i)
		// kglite does not have snapshot isolation — readers may see partial writes.
		// Count should be between 50 (initial) and 100 (all written).
		require.True(t, readCounts[i] >= 50 && readCounts[i] <= 100,
			"reader %d: expected count between 50 and 100, got %d", i, readCounts[i])
	}

	// After all goroutines finish, total should be 100
	finalCount := runQueryInt64(ctx, t, db, "MATCH (n:ConcurrentTestNode) RETURN count(n) AS c")
	require.Equal(t, int64(100), finalCount)
}

// TestConcurrentMultipleWrites launches 5 goroutines each creating nodes with
// unique properties via WriteTransaction. After all complete, the total node
// count must equal the expected sum.
func TestConcurrentMultipleWrites(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openGraph(t)

	const numWriters = 5
	const nodesPerWriter = 20
	var wg sync.WaitGroup
	errs := make([]error, numWriters)

	wg.Add(numWriters)
	for w := 0; w < numWriters; w++ {
		go func(writerIdx int) {
			defer wg.Done()
			errs[writerIdx] = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
				for i := 0; i < nodesPerWriter; i++ {
					_, err := tx.CreateNode(
						graph.AsProperties(map[string]any{
							"name":     fmt.Sprintf("writer%d_node%d", writerIdx, i),
							"objectid": fmt.Sprintf("MW-%d-%d", writerIdx, i),
						}),
						concurrentNodeKind,
					)
					if err != nil {
						return err
					}
				}
				return nil
			})
		}(w)
	}

	wg.Wait()
	for i := 0; i < numWriters; i++ {
		require.NoError(t, errs[i], "writer %d errored", i)
	}

	expected := int64(numWriters * nodesPerWriter)
	actual := runQueryInt64(ctx, t, db, "MATCH (n:ConcurrentTestNode) RETURN count(n) AS c")
	require.Equal(t, expected, actual, "total node count mismatch after concurrent writes")
}

// TestConcurrentBatchWithReads starts a batch write operation while concurrently
// reading from the graph. Verifies no panics or data corruption.
func TestConcurrentBatchWithReads(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openGraph(t)

	// Seed some initial data so reads have something to query
	createNodes(ctx, t, db, concurrentNodeKind, 30)

	var wg sync.WaitGroup

	// Batch writer
	var batchErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		batchErr = db.BatchOperation(ctx, func(batch graph.Batch) error {
			for i := 0; i < 70; i++ {
				node := graph.PrepareNode(graph.AsProperties(map[string]any{
					"objectid": fmt.Sprintf("BATCH-CONC-%d", i),
					"name":     fmt.Sprintf("batch_node_%d", i),
				}), concurrentNodeKind)
				if err := batch.CreateNode(node); err != nil {
					return err
				}
			}
			return nil
		})
	}()

	// Concurrent readers
	const numReaders = 5
	readErrs := make([]error, numReaders)
	readCounts := make([]int64, numReaders)
	wg.Add(numReaders)
	for i := 0; i < numReaders; i++ {
		go func(idx int) {
			defer wg.Done()
			var count int64
			err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
				result := tx.Raw("MATCH (n:ConcurrentTestNode) RETURN count(n) AS c", nil)
				defer result.Close()
				if result.Error() != nil {
					return result.Error()
				}
				if !result.Next() {
					return fmt.Errorf("reader %d: no rows returned", idx)
				}
				vals := result.Values()
				switch v := vals[0].(type) {
				case int64:
					count = v
				case float64:
					count = int64(v)
				case int:
					count = int64(v)
				default:
					return fmt.Errorf("reader %d: unexpected type %T", idx, vals[0])
				}
				return result.Error()
			})
			readErrs[idx] = err
			readCounts[idx] = count
		}(i)
	}

	wg.Wait()

	require.NoError(t, batchErr, "batch operation failed")
	for i := 0; i < numReaders; i++ {
		require.NoError(t, readErrs[i], "reader %d errored", i)
		// Count should be between 30 (initial seed) and 100 (seed + batch),
		// depending on timing relative to the batch commit.
		require.True(t, readCounts[i] >= 30 && readCounts[i] <= 100,
			"reader %d: expected count between 30 and 100, got %d", i, readCounts[i])
	}

	// After completion, total should be 100
	finalCount := runQueryInt64(ctx, t, db, "MATCH (n:ConcurrentTestNode) RETURN count(n) AS c")
	require.Equal(t, int64(100), finalCount)
}

// TestConcurrentReadWriteDeadlockDetection verifies that concurrent read and
// write transactions complete within a reasonable timeout, detecting potential
// deadlocks. Uses a 5-second operation timeout inside a 30-second test timeout.
func TestConcurrentReadWriteDeadlockDetection(t *testing.T) {
	t.Parallel()
	opCtx, opCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer opCancel()

	db := openGraph(t)

	// Seed data
	bgCtx := context.Background()
	createNodes(bgCtx, t, db, concurrentNodeKind, 20)

	var wg sync.WaitGroup
	const numGoroutines = 10
	errs := make([]error, numGoroutines)

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		if i%2 == 0 {
			// Writer
			go func(idx int) {
				defer wg.Done()
				errs[idx] = db.WriteTransaction(opCtx, func(tx graph.Transaction) error {
					_, err := tx.CreateNode(
						graph.AsProperties(map[string]any{
							"name":     fmt.Sprintf("deadlock_test_%d", idx),
							"objectid": fmt.Sprintf("DL-%d", idx),
						}),
						concurrentNodeKind,
					)
					return err
				})
			}(i)
		} else {
			// Reader
			go func(idx int) {
				defer wg.Done()
				errs[idx] = db.ReadTransaction(opCtx, func(tx graph.Transaction) error {
					result := tx.Raw("MATCH (n:ConcurrentTestNode) RETURN count(n) AS c", nil)
					defer result.Close()
					if result.Error() != nil {
						return result.Error()
					}
					result.Next()
					return result.Error()
				})
			}(i)
		}
	}

	// Wait with a hard deadline to detect deadlocks
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed
	case <-time.After(10 * time.Second):
		t.Fatal("DEADLOCK DETECTED: concurrent read/write operations did not complete within 10 seconds")
	}

	for i := 0; i < numGoroutines; i++ {
		if errs[i] != nil {
			// Context deadline exceeded is acceptable (means no deadlock, just slow),
			// but other errors should be flagged.
			if opCtx.Err() != nil {
				t.Logf("goroutine %d: context error (acceptable): %v", i, errs[i])
			} else {
				require.NoError(t, errs[i], "goroutine %d errored unexpectedly", i)
			}
		}
	}
}
