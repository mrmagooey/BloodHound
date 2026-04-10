// Copyright 2025 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

//go:build standalone

package dawgs

import (
	"context"
	"fmt"
	"testing"

	"github.com/specterops/dawgs/graph"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

// fetchAllRelationshipIDs returns the real edge IDs from the graph using raw
// kglite Cypher. This is needed because transaction.CreateRelationshipByIDs
// always returns id=0; the actual IDs must be queried back from the engine.
func fetchAllRelationshipIDs(t *testing.T, db *Driver) []graph.ID {
	t.Helper()
	result, err := db.kg.Cypher("MATCH ()-[r]->() RETURN id(r)", nil)
	if err != nil {
		t.Fatalf("fetchAllRelationshipIDs: %v", err)
	}
	ids := make([]graph.ID, 0, len(result.Rows))
	for _, row := range result.Rows {
		if len(row) == 0 {
			continue
		}
		switch v := row[0].(type) {
		case float64:
			ids = append(ids, graph.ID(uint64(v)))
		case int64:
			ids = append(ids, graph.ID(v))
		case uint64:
			ids = append(ids, graph.ID(v))
		}
	}
	return ids
}

// countNodes returns the total node count via ReadTransaction.
func countNodes(t *testing.T, db *Driver) int64 {
	t.Helper()
	var count int64
	err := db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		var err error
		count, err = tx.Nodes().Count()
		return err
	})
	if err != nil {
		t.Fatalf("countNodes: %v", err)
	}
	return count
}

// countRelationships returns the total relationship count via ReadTransaction.
func countRelationships(t *testing.T, db *Driver) int64 {
	t.Helper()
	var count int64
	err := db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		var err error
		count, err = tx.Relationships().Count()
		return err
	})
	if err != nil {
		t.Fatalf("countRelationships: %v", err)
	}
	return count
}

// fetchAllNodes returns all nodes from the graph.
func fetchAllNodes(t *testing.T, db *Driver) []*graph.Node {
	t.Helper()
	var nodes []*graph.Node
	err := db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		return tx.Nodes().Fetch(func(cursor graph.Cursor[*graph.Node]) error {
			for node := range cursor.Chan() {
				nodes = append(nodes, node)
			}
			return cursor.Error()
		})
	})
	if err != nil {
		t.Fatalf("fetchAllNodes: %v", err)
	}
	return nodes
}

// ─── Batch.CreateNode ─────────────────────────────────────────────────────────

func TestBatchCreateNodeSingleKind(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		node := graph.NewNode(0, graph.AsProperties(map[string]any{"name": "alice"}), testNodeKind)
		return batch.CreateNode(node)
	})
	if err != nil {
		t.Fatalf("BatchOperation: %v", err)
	}

	count := countNodes(t, db)
	if count != 1 {
		t.Errorf("expected 1 node, got %d", count)
	}
}

func TestBatchCreateNodeNoKindReturnsError(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		// No kinds — should return an error
		node := graph.NewNode(0, graph.NewProperties())
		return batch.CreateNode(node)
	})
	if err == nil {
		t.Fatal("expected error when creating node with no kinds")
	}
}

func TestBatchCreateNodeNilProperties(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		node := &graph.Node{
			Kinds:      graph.Kinds{testNodeKind},
			Properties: nil,
		}
		return batch.CreateNode(node)
	})
	if err != nil {
		t.Fatalf("BatchOperation with nil properties: %v", err)
	}

	count := countNodes(t, db)
	if count != 1 {
		t.Errorf("expected 1 node with nil properties, got %d", count)
	}
}

func TestBatchCreateNodeMultipleKinds(t *testing.T) {
	db := openTestDriver(t)

	extraKind := graph.StringKind("ExtraKind")
	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		node := graph.NewNode(0,
			graph.AsProperties(map[string]any{"name": "multi"}),
			testNodeKind, extraKind,
		)
		return batch.CreateNode(node)
	})
	if err != nil {
		t.Fatalf("BatchOperation multi-kind: %v", err)
	}

	count := countNodes(t, db)
	if count != 1 {
		t.Errorf("expected 1 node, got %d", count)
	}
}

func TestBatchCreateMultipleNodes(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		for i := 0; i < 5; i++ {
			node := graph.NewNode(0,
				graph.AsProperties(map[string]any{"idx": i}),
				testNodeKind,
			)
			if err := batch.CreateNode(node); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("BatchOperation: %v", err)
	}

	count := countNodes(t, db)
	if count != 5 {
		t.Errorf("expected 5 nodes, got %d", count)
	}
}

// ─── Batch.DeleteNode ─────────────────────────────────────────────────────────

func TestBatchDeleteNode(t *testing.T) {
	db := openTestDriver(t)
	n := createTestNode(t, db, testNodeKind, map[string]any{"name": "to-delete"})

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		return batch.DeleteNode(n.ID)
	})
	if err != nil {
		t.Fatalf("BatchOperation DeleteNode: %v", err)
	}

	count := countNodes(t, db)
	if count != 0 {
		t.Errorf("expected 0 nodes after delete, got %d", count)
	}
}

func TestBatchDeleteNodeNonExistent(t *testing.T) {
	db := openTestDriver(t)
	// Deleting a non-existent node should not error
	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		return batch.DeleteNode(graph.ID(99999))
	})
	if err != nil {
		t.Fatalf("DeleteNode on non-existent node should not error: %v", err)
	}
}

// ─── Batch.CreateRelationshipByIDs ────────────────────────────────────────────

func TestBatchCreateRelationshipByIDs(t *testing.T) {
	db := openTestDriver(t)
	n1 := createTestNode(t, db, testNodeKind, map[string]any{})
	n2 := createTestNode(t, db, testNodeKind, map[string]any{})

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		return batch.CreateRelationshipByIDs(n1.ID, n2.ID, testEdgeKind, nil)
	})
	if err != nil {
		t.Fatalf("BatchOperation CreateRelationshipByIDs: %v", err)
	}

	count := countRelationships(t, db)
	if count != 1 {
		t.Errorf("expected 1 relationship, got %d", count)
	}
}

func TestBatchCreateRelationshipByIDsWithProperties(t *testing.T) {
	db := openTestDriver(t)
	n1 := createTestNode(t, db, testNodeKind, map[string]any{})
	n2 := createTestNode(t, db, testNodeKind, map[string]any{})

	props := graph.AsProperties(map[string]any{"weight": int64(10)})
	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		return batch.CreateRelationshipByIDs(n1.ID, n2.ID, testEdgeKind, props)
	})
	if err != nil {
		t.Fatalf("BatchOperation CreateRelationshipByIDs with props: %v", err)
	}

	count := countRelationships(t, db)
	if count != 1 {
		t.Errorf("expected 1 relationship, got %d", count)
	}
}

func TestBatchCreateRelationshipNilKindReturnsError(t *testing.T) {
	db := openTestDriver(t)
	n1 := createTestNode(t, db, testNodeKind, map[string]any{})
	n2 := createTestNode(t, db, testNodeKind, map[string]any{})

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		return batch.CreateRelationshipByIDs(n1.ID, n2.ID, nil, nil)
	})
	if err == nil {
		t.Fatal("expected error for nil kind")
	}
}

func TestBatchCreateRelationship(t *testing.T) {
	db := openTestDriver(t)
	n1 := createTestNode(t, db, testNodeKind, map[string]any{})
	n2 := createTestNode(t, db, testNodeKind, map[string]any{})

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		rel := graph.NewRelationship(0, n1.ID, n2.ID, nil, testEdgeKind)
		return batch.CreateRelationship(rel)
	})
	if err != nil {
		t.Fatalf("BatchOperation CreateRelationship: %v", err)
	}

	count := countRelationships(t, db)
	if count != 1 {
		t.Errorf("expected 1 relationship, got %d", count)
	}
}

// ─── Batch.DeleteRelationship ─────────────────────────────────────────────────

func TestBatchDeleteRelationship(t *testing.T) {
	db := openTestDriver(t)
	n1 := createTestNode(t, db, testNodeKind, map[string]any{})
	n2 := createTestNode(t, db, testNodeKind, map[string]any{})

	// Create an edge via WriteTransaction to get its ID
	var relID graph.ID
	err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		rel, err := tx.CreateRelationshipByIDs(n1.ID, n2.ID, testEdgeKind, nil)
		if err != nil {
			return err
		}
		relID = rel.ID
		return nil
	})
	if err != nil {
		t.Fatalf("CreateRelationshipByIDs: %v", err)
	}

	err = db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		return batch.DeleteRelationship(relID)
	})
	if err != nil {
		t.Fatalf("BatchOperation DeleteRelationship: %v", err)
	}

	count := countRelationships(t, db)
	if count != 0 {
		t.Errorf("expected 0 relationships after delete, got %d", count)
	}
}

// TestBatchDeleteRelationshipSingle verifies that a single DeleteRelationship
// call (below the flush threshold) is still flushed on Commit.
func TestBatchDeleteRelationshipSingle(t *testing.T) {
	db := openTestDriver(t)
	n1 := createTestNode(t, db, testNodeKind, map[string]any{})
	n2 := createTestNode(t, db, testNodeKind, map[string]any{})

	// Create the edge
	err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		_, err := tx.CreateRelationshipByIDs(n1.ID, n2.ID, testEdgeKind, nil)
		return err
	})
	if err != nil {
		t.Fatalf("CreateRelationshipByIDs: %v", err)
	}

	// Fetch real IDs (CreateRelationshipByIDs always returns id=0).
	relIDs := fetchAllRelationshipIDs(t, db)
	if len(relIDs) != 1 {
		t.Fatalf("expected 1 relationship, got %d", len(relIDs))
	}

	// One delete — below threshold, must flush on Commit.
	err = db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		return batch.DeleteRelationship(relIDs[0])
	})
	if err != nil {
		t.Fatalf("BatchOperation DeleteRelationship (single): %v", err)
	}

	count := countRelationships(t, db)
	if count != 0 {
		t.Errorf("expected 0 relationships after single delete, got %d", count)
	}
}

// createTestRelationships creates n relationships between src and dst using
// distinct edge types and returns their real IDs (fetched via raw Cypher).
func createTestRelationships(t *testing.T, db *Driver, src, dst *graph.Node, n int, prefix string) []graph.ID {
	t.Helper()
	for i := 0; i < n; i++ {
		kind := graph.StringKind(fmt.Sprintf("%s%d", prefix, i))
		err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
			_, err := tx.CreateRelationshipByIDs(src.ID, dst.ID, kind, nil)
			return err
		})
		if err != nil {
			t.Fatalf("createTestRelationships[%d]: %v", i, err)
		}
	}
	ids := fetchAllRelationshipIDs(t, db)
	if len(ids) != n {
		t.Fatalf("createTestRelationships: expected %d IDs, got %d", n, len(ids))
	}
	return ids
}

// TestBatchDeleteRelationshipBatchFlush verifies that when more than
// defaultDeleteFlushSize relationships are deleted the batch auto-flushes
// mid-operation and all relationships are removed.
func TestBatchDeleteRelationshipBatchFlush(t *testing.T) {
	const total = defaultDeleteFlushSize + 10 // just over the threshold

	db := openTestDriver(t)
	src := createTestNode(t, db, testNodeKind, map[string]any{})
	dst := createTestNode(t, db, testNodeKind, map[string]any{})

	relIDs := createTestRelationships(t, db, src, dst, total, "DelBatchEdge")

	if got := countRelationships(t, db); int(got) != total {
		t.Fatalf("setup: expected %d relationships, got %d", total, got)
	}

	// Delete all relationships in a single BatchOperation.
	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		for _, id := range relIDs {
			if err := batch.DeleteRelationship(id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("BatchOperation DeleteRelationship (batch): %v", err)
	}

	count := countRelationships(t, db)
	if count != 0 {
		t.Errorf("expected 0 relationships after batch delete, got %d", count)
	}
}

// TestBatchDeleteRelationshipExactlyAtThreshold verifies correct behaviour when
// exactly defaultDeleteFlushSize relationships are deleted (i.e. the flush fires
// on the last element and no remainder is left for Commit to drain).
func TestBatchDeleteRelationshipExactlyAtThreshold(t *testing.T) {
	const total = defaultDeleteFlushSize

	db := openTestDriver(t)
	src := createTestNode(t, db, testNodeKind, map[string]any{})
	dst := createTestNode(t, db, testNodeKind, map[string]any{})

	relIDs := createTestRelationships(t, db, src, dst, total, "DelExactEdge")

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		for _, id := range relIDs {
			if err := batch.DeleteRelationship(id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("BatchOperation DeleteRelationship (exact threshold): %v", err)
	}

	count := countRelationships(t, db)
	if count != 0 {
		t.Errorf("expected 0 relationships after exact-threshold delete, got %d", count)
	}
}

// TestBatchDeleteRelationshipEmpty verifies that calling flush with no pending
// deletes is a no-op and does not error.
func TestBatchDeleteRelationshipEmpty(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		// No deletes at all — Commit should not error.
		return nil
	})
	if err != nil {
		t.Fatalf("BatchOperation with zero deletes: %v", err)
	}
}

// TestBatchDeleteRelationshipNonExistent verifies that deleting a relationship
// that does not exist does not return an error.
func TestBatchDeleteRelationshipNonExistent(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		return batch.DeleteRelationship(graph.ID(99999))
	})
	if err != nil {
		t.Fatalf("DeleteRelationship on non-existent ID should not error: %v", err)
	}
}

// TestBatchDeleteRelationshipFlushedOnCommit verifies that deletes below the
// threshold are still committed when Commit() is called explicitly mid-batch.
func TestBatchDeleteRelationshipFlushedOnCommit(t *testing.T) {
	db := openTestDriver(t)
	n1 := createTestNode(t, db, testNodeKind, map[string]any{})
	n2 := createTestNode(t, db, testNodeKind, map[string]any{})

	err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		_, err := tx.CreateRelationshipByIDs(n1.ID, n2.ID, testEdgeKind, nil)
		return err
	})
	if err != nil {
		t.Fatalf("CreateRelationshipByIDs: %v", err)
	}

	// Fetch the real relationship ID.
	relIDs := fetchAllRelationshipIDs(t, db)
	if len(relIDs) != 1 {
		t.Fatalf("expected 1 relationship, got %d", len(relIDs))
	}

	err = db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		if err := batch.DeleteRelationship(relIDs[0]); err != nil {
			return err
		}
		// Explicit mid-batch commit should flush the pending delete.
		return batch.Commit()
	})
	if err != nil {
		t.Fatalf("BatchOperation with explicit Commit: %v", err)
	}

	count := countRelationships(t, db)
	if count != 0 {
		t.Errorf("expected 0 relationships after explicit Commit, got %d", count)
	}
}

// ─── Batch.UpdateNodeBy ───────────────────────────────────────────────────────

func TestBatchUpdateNodeByCreatesNode(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		node := graph.NewNode(0,
			graph.AsProperties(map[string]any{
				"objectid": "test-obj-1",
				"name":     "Alice",
			}),
			testNodeKind,
		)
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node:               node,
			IdentityKind:       testNodeKind,
			IdentityProperties: []string{"objectid"},
		})
	})
	if err != nil {
		t.Fatalf("UpdateNodeBy: %v", err)
	}

	count := countNodes(t, db)
	if count != 1 {
		t.Errorf("expected 1 node after UpdateNodeBy, got %d", count)
	}
}

func TestBatchUpdateNodeByIdempotent(t *testing.T) {
	// Calling UpdateNodeBy twice with the same identity should yield only one node
	db := openTestDriver(t)

	upsertFn := func(batch graph.Batch) error {
		node := graph.NewNode(0,
			graph.AsProperties(map[string]any{
				"objectid": "upsert-id-1",
				"name":     "Bob",
			}),
			testNodeKind,
		)
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node:               node,
			IdentityKind:       testNodeKind,
			IdentityProperties: []string{"objectid"},
		})
	}

	// First upsert
	if err := db.BatchOperation(context.Background(), upsertFn); err != nil {
		t.Fatalf("First UpdateNodeBy: %v", err)
	}
	// Second upsert with same objectid
	if err := db.BatchOperation(context.Background(), upsertFn); err != nil {
		t.Fatalf("Second UpdateNodeBy: %v", err)
	}

	count := countNodes(t, db)
	if count != 1 {
		t.Errorf("expected 1 node (idempotent upsert), got %d", count)
	}
}

func TestBatchUpdateNodeByNoKindReturnsError(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		node := &graph.Node{
			Properties: graph.AsProperties(map[string]any{"objectid": "x"}),
			Kinds:      graph.Kinds{}, // no kinds
		}
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node:               node,
			IdentityProperties: []string{"objectid"},
		})
	})
	if err == nil {
		t.Fatal("expected error for UpdateNodeBy with no kinds")
	}
}

func TestBatchUpdateNodeByNilNodeReturnsError(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node: nil,
		})
	})
	if err == nil {
		t.Fatal("expected error for UpdateNodeBy with nil node")
	}
}

func TestBatchUpdateNodeByMultipleKinds(t *testing.T) {
	db := openTestDriver(t)
	extraKind := graph.StringKind("SecondaryKind")

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		node := graph.NewNode(0,
			graph.AsProperties(map[string]any{
				"objectid": "multi-kind-obj",
			}),
			testNodeKind, extraKind,
		)
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node:               node,
			IdentityKind:       testNodeKind,
			IdentityProperties: []string{"objectid"},
		})
	})
	if err != nil {
		t.Fatalf("UpdateNodeBy multi-kind: %v", err)
	}

	count := countNodes(t, db)
	if count != 1 {
		t.Errorf("expected 1 node, got %d", count)
	}
}

// ─── Batch.Commit ─────────────────────────────────────────────────────────────

func TestBatchCommitNoOperations(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		// Explicitly commit with no operations
		return batch.Commit()
	})
	if err != nil {
		t.Fatalf("Commit with no operations: %v", err)
	}
}

func TestBatchCommitFlushesData(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		node := graph.NewNode(0,
			graph.AsProperties(map[string]any{"name": "commit-test"}),
			testNodeKind,
		)
		if err := batch.CreateNode(node); err != nil {
			return err
		}
		// Explicit mid-batch commit
		return batch.Commit()
	})
	if err != nil {
		t.Fatalf("BatchOperation with explicit Commit: %v", err)
	}

	count := countNodes(t, db)
	if count != 1 {
		t.Errorf("expected 1 node after explicit commit, got %d", count)
	}
}

// ─── Batch.WithGraph ──────────────────────────────────────────────────────────

func TestBatchWithGraphNoOp(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		// WithGraph is a no-op; should return the same batch
		batch2 := batch.WithGraph(graph.Graph{})
		if batch2 == nil {
			return fmt.Errorf("WithGraph returned nil")
		}
		// Should still be usable
		node := graph.NewNode(0, graph.AsProperties(map[string]any{"name": "x"}), testNodeKind)
		return batch2.CreateNode(node)
	})
	if err != nil {
		t.Fatalf("WithGraph: %v", err)
	}

	count := countNodes(t, db)
	if count != 1 {
		t.Errorf("expected 1 node via batch from WithGraph, got %d", count)
	}
}

// ─── Auto-flush threshold ─────────────────────────────────────────────────────

func TestBatchAutoFlushNodeThreshold(t *testing.T) {
	db := openTestDriver(t)

	// Insert enough nodes to trigger an auto-flush (default is 2000)
	// Use exactly flushSize + 1 to ensure at least one flush happens during the batch
	const nodesToCreate = defaultBatchFlushSize + 1

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		for i := 0; i < nodesToCreate; i++ {
			node := graph.NewNode(0,
				graph.AsProperties(map[string]any{"idx": i}),
				testNodeKind,
			)
			if err := batch.CreateNode(node); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("BatchOperation auto-flush: %v", err)
	}

	count := countNodes(t, db)
	if count != nodesToCreate {
		t.Errorf("expected %d nodes after auto-flush, got %d", nodesToCreate, count)
	}
}

func TestBatchAutoFlushEdgeThreshold(t *testing.T) {
	db := openTestDriver(t)

	// Create two nodes to serve as endpoints for all edges
	n1 := createTestNode(t, db, testNodeKind, map[string]any{})
	n2 := createTestNode(t, db, testNodeKind, map[string]any{})

	// We need distinct edges to trigger the edge flush (default 5000).
	// Use multiple edge types or create many edges between different (src,dst,type) combos.
	// Creating defaultEdgeFlushSize + 1 distinct edge types avoids dedup.
	const edgesToCreate = defaultEdgeFlushSize + 1

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		for i := 0; i < edgesToCreate; i++ {
			kind := graph.StringKind(fmt.Sprintf("EdgeType%d", i))
			if err := batch.CreateRelationshipByIDs(n1.ID, n2.ID, kind, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("BatchOperation edge auto-flush: %v", err)
	}

	count := countRelationships(t, db)
	if count != edgesToCreate {
		t.Errorf("expected %d relationships after edge auto-flush, got %d", edgesToCreate, count)
	}
}

// ─── Batch mixed node and edge operations ────────────────────────────────────

func TestBatchMixedOperations(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		// Create nodes in this batch via UpdateNodeBy (MERGE)
		n1 := graph.NewNode(0,
			graph.AsProperties(map[string]any{"objectid": "mixed-1"}),
			testNodeKind,
		)
		n2 := graph.NewNode(0,
			graph.AsProperties(map[string]any{"objectid": "mixed-2"}),
			testNodeKind,
		)
		if err := batch.UpdateNodeBy(graph.NodeUpdate{
			Node:               n1,
			IdentityKind:       testNodeKind,
			IdentityProperties: []string{"objectid"},
		}); err != nil {
			return err
		}
		if err := batch.UpdateNodeBy(graph.NodeUpdate{
			Node:               n2,
			IdentityKind:       testNodeKind,
			IdentityProperties: []string{"objectid"},
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("mixed batch: %v", err)
	}

	count := countNodes(t, db)
	if count != 2 {
		t.Errorf("expected 2 nodes from mixed batch, got %d", count)
	}
}

// ─── Batch.UpdateRelationshipBy ───────────────────────────────────────────────

func TestBatchUpdateRelationshipByCreatesEdge(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		start := graph.NewNode(0,
			graph.AsProperties(map[string]any{"objectid": "src-obj-1"}),
			testNodeKind,
		)
		end := graph.NewNode(0,
			graph.AsProperties(map[string]any{"objectid": "dst-obj-1"}),
			testNodeKind,
		)
		rel := graph.NewRelationship(0, 0, 0, graph.NewProperties(), testEdgeKind)
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Relationship:            rel,
			Start:                   start,
			StartIdentityKind:       testNodeKind,
			StartIdentityProperties: []string{"objectid"},
			End:                     end,
			EndIdentityKind:         testNodeKind,
			EndIdentityProperties:   []string{"objectid"},
		})
	})
	if err != nil {
		t.Fatalf("UpdateRelationshipBy: %v", err)
	}

	// Should have created 2 nodes and 1 edge
	nodeCount := countNodes(t, db)
	if nodeCount != 2 {
		t.Errorf("expected 2 nodes from UpdateRelationshipBy, got %d", nodeCount)
	}
	edgeCount := countRelationships(t, db)
	if edgeCount != 1 {
		t.Errorf("expected 1 edge from UpdateRelationshipBy, got %d", edgeCount)
	}
}

func TestBatchUpdateRelationshipByNilRelationshipReturnsError(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Relationship: nil,
		})
	})
	if err == nil {
		t.Fatal("expected error for UpdateRelationshipBy with nil relationship")
	}
}

func TestBatchUpdateRelationshipByNilKindReturnsError(t *testing.T) {
	db := openTestDriver(t)

	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		rel := graph.NewRelationship(0, 0, 0, graph.NewProperties(), nil)
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Relationship: rel,
		})
	})
	if err == nil {
		t.Fatal("expected error for UpdateRelationshipBy with nil kind")
	}
}

// ─── All nodes present after complex batch ────────────────────────────────────

func TestBatchAllNodesAccessibleAfterCommit(t *testing.T) {
	db := openTestDriver(t)

	const n = 10
	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		for i := 0; i < n; i++ {
			node := graph.NewNode(0,
				graph.AsProperties(map[string]any{
					"objectid": fmt.Sprintf("node-%d", i),
					"name":     fmt.Sprintf("Node %d", i),
				}),
				testNodeKind,
			)
			if err := batch.UpdateNodeBy(graph.NodeUpdate{
				Node:               node,
				IdentityKind:       testNodeKind,
				IdentityProperties: []string{"objectid"},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("BatchOperation: %v", err)
	}

	nodes := fetchAllNodes(t, db)
	if len(nodes) != n {
		t.Errorf("expected %d nodes after batch, got %d", n, len(nodes))
	}

	// Verify objectids are accessible
	seen := make(map[string]bool, n)
	for _, node := range nodes {
		oid, _ := node.Properties.Get("objectid").String()
		seen[oid] = true
	}
	for i := 0; i < n; i++ {
		expected := fmt.Sprintf("node-%d", i)
		if !seen[expected] {
			t.Errorf("missing node with objectid=%q", expected)
		}
	}
}

// ─── Batch.UpdateRelationshipBy with multi-kind nodes ───────────────────────

func TestBatchUpdateRelationshipByMultiKind(t *testing.T) {
	db := openTestDriver(t)

	extraKind := graph.StringKind("ExtraKind")
	err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		start := graph.NewNode(0,
			graph.AsProperties(map[string]any{"objectid": "multi-src", "name": "source"}),
			testNodeKind, extraKind,
		)
		end := graph.NewNode(0,
			graph.AsProperties(map[string]any{"objectid": "multi-dst", "name": "destination"}),
			testNodeKind, extraKind,
		)
		rel := graph.NewRelationship(0, 0, 0, graph.AsProperties(map[string]any{"link": "test"}), testEdgeKind)
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Relationship:            rel,
			Start:                   start,
			StartIdentityKind:       testNodeKind,
			StartIdentityProperties: []string{"objectid"},
			End:                     end,
			EndIdentityKind:         testNodeKind,
			EndIdentityProperties:   []string{"objectid"},
		})
	})
	if err != nil {
		t.Fatalf("UpdateRelationshipBy with multi-kind: %v", err)
	}

	// Should have created 2 multi-kind nodes and 1 edge
	nodeCount := countNodes(t, db)
	if nodeCount != 2 {
		t.Errorf("expected 2 nodes from UpdateRelationshipBy, got %d", nodeCount)
	}
	edgeCount := countRelationships(t, db)
	if edgeCount != 1 {
		t.Errorf("expected 1 edge from UpdateRelationshipBy, got %d", edgeCount)
	}
}
