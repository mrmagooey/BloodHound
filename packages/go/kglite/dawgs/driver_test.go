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
	"os"
	"path/filepath"
	"testing"

	"github.com/specterops/dawgs/graph"
)

// toInt64 converts a numeric value (float64 or int64) to int64.
// kglite returns count() results as float64 so we must handle both.
func toInt64Val(v any) int64 {
	switch typed := v.(type) {
	case int64:
		return typed
	case float64:
		return int64(typed)
	}
	return 0
}

// ─── Open / Close lifecycle ────────────────────────────────────────────────────

func TestOpenInMemory(t *testing.T) {
	d, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\") returned error: %v", err)
	}
	if d == nil {
		t.Fatal("Open(\"\") returned nil driver")
	}
	if d.kg == nil {
		t.Fatal("driver.kg is nil after Open")
	}
	if err := d.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func TestOpenInMemoryGraphPathEmpty(t *testing.T) {
	d, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\") returned error: %v", err)
	}
	defer d.Close(context.Background())

	if d.graphPath != "" {
		t.Fatalf("expected graphPath to be empty, got %q", d.graphPath)
	}
}

func TestOpenFileBacked(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.kgl")

	d, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q) returned error: %v", dbPath, err)
	}
	if d == nil {
		t.Fatal("Open returned nil driver")
	}
	if d.graphPath != dbPath {
		t.Fatalf("expected graphPath=%q, got %q", dbPath, d.graphPath)
	}
	if err := d.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func TestCloseInMemoryNoSave(t *testing.T) {
	d, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\") returned error: %v", err)
	}
	// Close of in-memory graph should not produce any file
	if err := d.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if d.kg != nil {
		t.Fatal("expected kg to be nil after Close")
	}
}

func TestCloseFileSavesToDisk(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.kgl")

	d, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	// Write a node so the file is non-empty
	if err := d.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		_, err := tx.CreateNode(graph.NewProperties(), graph.StringKind("TestNode"))
		return err
	}); err != nil {
		t.Fatalf("WriteTransaction failed: %v", err)
	}
	if err := d.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	// File must exist on disk
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("expected file to exist after Close: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("expected file size > 0 after saving graph")
	}
}

func TestCloseNilKg(t *testing.T) {
	// Closing a driver that already has a nil kg should be a no-op
	d := &Driver{}
	if err := d.Close(context.Background()); err != nil {
		t.Fatalf("Close on nil kg should not error, got: %v", err)
	}
}

func TestDoubleClose(t *testing.T) {
	d, err := Open("")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if err := d.Close(context.Background()); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	// Second close should not panic
	if err := d.Close(context.Background()); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

// ─── Driver configuration ─────────────────────────────────────────────────────

func TestSetWriteFlushSize(t *testing.T) {
	d := openTestDriver(t)
	d.SetWriteFlushSize(500)
	if d.writeFlushSize != 500 {
		t.Fatalf("expected writeFlushSize=500, got %d", d.writeFlushSize)
	}
}

func TestSetBatchWriteSize(t *testing.T) {
	d := openTestDriver(t)
	d.SetBatchWriteSize(250)
	if d.batchWriteSize != 250 {
		t.Fatalf("expected batchWriteSize=250, got %d", d.batchWriteSize)
	}
}

func TestDefaultFlushSizes(t *testing.T) {
	d := openTestDriver(t)
	if d.writeFlushSize != 100_000 {
		t.Fatalf("expected default writeFlushSize=100000, got %d", d.writeFlushSize)
	}
	if d.batchWriteSize != 20_000 {
		t.Fatalf("expected default batchWriteSize=20000, got %d", d.batchWriteSize)
	}
}

// ─── AssertSchema / SetDefaultGraph / FetchKinds / RefreshKinds ───────────────

func TestAssertSchemaNoOp(t *testing.T) {
	d := openTestDriver(t)
	if err := d.AssertSchema(context.Background(), graph.Schema{}); err != nil {
		t.Fatalf("AssertSchema returned error: %v", err)
	}
}

func TestSetDefaultGraphNoOp(t *testing.T) {
	d := openTestDriver(t)
	if err := d.SetDefaultGraph(context.Background(), graph.Graph{}); err != nil {
		t.Fatalf("SetDefaultGraph returned error: %v", err)
	}
}

func TestFetchKindsEmpty(t *testing.T) {
	d := openTestDriver(t)
	kinds, err := d.FetchKinds(context.Background())
	if err != nil {
		t.Fatalf("FetchKinds returned error: %v", err)
	}
	if len(kinds) != 0 {
		t.Fatalf("expected empty kinds, got %v", kinds)
	}
}

func TestRefreshKindsNoOp(t *testing.T) {
	d := openTestDriver(t)
	if err := d.RefreshKinds(context.Background()); err != nil {
		t.Fatalf("RefreshKinds returned error: %v", err)
	}
}

// ─── Run ──────────────────────────────────────────────────────────────────────

func TestRunBasicQuery(t *testing.T) {
	d := openTestDriver(t)
	if err := d.Run(context.Background(), "MATCH (n) RETURN n", nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRunCreateNode(t *testing.T) {
	d := openTestDriver(t)
	if err := d.Run(context.Background(), "CREATE (n:`RunNode` {name: 'test'})", nil); err != nil {
		t.Fatalf("Run CREATE returned error: %v", err)
	}
}

// ─── ReadTransaction ──────────────────────────────────────────────────────────

func TestReadTransactionBasic(t *testing.T) {
	d := openTestDriver(t)
	var nodeCount int64
	err := d.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n) RETURN count(n) AS c", nil)
		defer result.Close()
		if result.Next() {
			vals := result.Values()
			if len(vals) > 0 {
				nodeCount = toInt64Val(vals[0])
			}
		}
		return result.Error()
	})
	if err != nil {
		t.Fatalf("ReadTransaction returned error: %v", err)
	}
	if nodeCount != 0 {
		t.Fatalf("expected 0 nodes, got %d", nodeCount)
	}
}

func TestReadTransactionQueryKeys(t *testing.T) {
	d := openTestDriver(t)
	err := d.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		result := tx.Raw("RETURN 1 AS one, 2 AS two", nil)
		defer result.Close()
		keys := result.Keys()
		if len(keys) != 2 {
			t.Errorf("expected 2 keys, got %d: %v", len(keys), keys)
		}
		return result.Error()
	})
	if err != nil {
		t.Fatalf("ReadTransaction returned error: %v", err)
	}
}

func TestReadTransactionCommit(t *testing.T) {
	d := openTestDriver(t)
	err := d.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		return tx.Commit()
	})
	if err != nil {
		t.Fatalf("ReadTransaction Commit returned error: %v", err)
	}
}

func TestReadTransactionWithGraph(t *testing.T) {
	d := openTestDriver(t)
	err := d.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// WithGraph should return same transaction (no-op for kglite)
		tx2 := tx.WithGraph(graph.Graph{})
		if tx2 == nil {
			return nil
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ReadTransaction WithGraph returned error: %v", err)
	}
}

func TestReadTransactionGraphQueryMemoryLimit(t *testing.T) {
	d := openTestDriver(t)
	err := d.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		limit := tx.GraphQueryMemoryLimit()
		if limit == 0 {
			t.Error("expected non-zero memory limit")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ReadTransaction returned error: %v", err)
	}
}

// ─── WriteTransaction ─────────────────────────────────────────────────────────

func TestWriteTransactionCreateNode(t *testing.T) {
	d := openTestDriver(t)
	var createdID graph.ID
	err := d.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		node, err := tx.CreateNode(graph.NewProperties(), graph.StringKind("TestKind"))
		if err != nil {
			return err
		}
		createdID = node.ID
		return nil
	})
	if err != nil {
		t.Fatalf("WriteTransaction returned error: %v", err)
	}
	// Verify the node exists via a read
	var found bool
	err = d.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		result := tx.Raw(
			"MATCH (n) WHERE id(n) = $id RETURN n",
			map[string]any{"id": uint64(createdID)},
		)
		defer result.Close()
		found = result.Next()
		return result.Error()
	})
	if err != nil {
		t.Fatalf("ReadTransaction returned error: %v", err)
	}
	if !found {
		t.Fatalf("expected created node ID=%d to be found", createdID)
	}
}

func TestWriteTransactionCreateNodeNoKind(t *testing.T) {
	d := openTestDriver(t)
	err := d.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		_, err := tx.CreateNode(graph.NewProperties())
		return err
	})
	if err == nil {
		t.Fatal("expected error when creating node with no kind")
	}
}

func TestWriteTransactionCreateNodeWithProperties(t *testing.T) {
	d := openTestDriver(t)
	var createdNode *graph.Node
	err := d.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{"name": "Alice", "enabled": true})
		node, err := tx.CreateNode(props, graph.StringKind("User"))
		if err != nil {
			return err
		}
		createdNode = node
		return nil
	})
	if err != nil {
		t.Fatalf("WriteTransaction returned error: %v", err)
	}
	if createdNode == nil {
		t.Fatal("expected created node to be non-nil")
	}
}

func TestWriteTransactionCreateNodeMultipleKinds(t *testing.T) {
	d := openTestDriver(t)
	err := d.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		_, err := tx.CreateNode(
			graph.NewProperties(),
			graph.StringKind("Base"),
			graph.StringKind("User"),
			graph.StringKind("Entity"),
		)
		return err
	})
	if err != nil {
		t.Fatalf("WriteTransaction multi-kind returned error: %v", err)
	}
}

func TestWriteTransactionCreateRelationship(t *testing.T) {
	d := openTestDriver(t)
	err := d.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		src, err := tx.CreateNode(graph.NewProperties(), graph.StringKind("Source"))
		if err != nil {
			return err
		}
		dst, err := tx.CreateNode(graph.NewProperties(), graph.StringKind("Dest"))
		if err != nil {
			return err
		}
		_, err = tx.CreateRelationshipByIDs(src.ID, dst.ID, graph.StringKind("Contains"), nil)
		return err
	})
	if err != nil {
		t.Fatalf("WriteTransaction create relationship returned error: %v", err)
	}
}

func TestWriteTransactionCreateRelationshipEmptyKind(t *testing.T) {
	d := openTestDriver(t)
	err := d.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		src, err := tx.CreateNode(graph.NewProperties(), graph.StringKind("A"))
		if err != nil {
			return err
		}
		dst, err := tx.CreateNode(graph.NewProperties(), graph.StringKind("B"))
		if err != nil {
			return err
		}
		_, err = tx.CreateRelationshipByIDs(src.ID, dst.ID, graph.StringKind(""), nil)
		return err
	})
	if err == nil {
		t.Fatal("expected error when creating relationship with empty kind")
	}
}

func TestWriteTransactionUpdateNode(t *testing.T) {
	d := openTestDriver(t)
	var nodeID graph.ID
	err := d.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		node, err := tx.CreateNode(graph.AsProperties(map[string]any{"name": "original"}), graph.StringKind("Thing"))
		if err != nil {
			return err
		}
		nodeID = node.ID
		// Update the name property
		node.Properties.Set("name", "updated")
		return tx.UpdateNode(node)
	})
	if err != nil {
		t.Fatalf("WriteTransaction UpdateNode returned error: %v", err)
	}

	// Verify the update was applied
	err = d.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		result := tx.Raw(
			"MATCH (n) WHERE id(n) = $id RETURN n.name",
			map[string]any{"id": uint64(nodeID)},
		)
		defer result.Close()
		if !result.Next() {
			t.Error("node not found after update")
			return nil
		}
		vals := result.Values()
		if len(vals) == 0 {
			t.Error("no values returned")
		}
		return result.Error()
	})
	if err != nil {
		t.Fatalf("ReadTransaction returned error: %v", err)
	}
}

func TestWriteTransactionUpdateNodeNilProperties(t *testing.T) {
	d := openTestDriver(t)
	err := d.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		node, err := tx.CreateNode(graph.NewProperties(), graph.StringKind("Thing2"))
		if err != nil {
			return err
		}
		node.Properties = nil
		// UpdateNode with nil properties should initialize them and proceed
		return tx.UpdateNode(node)
	})
	if err != nil {
		t.Fatalf("UpdateNode with nil properties returned error: %v", err)
	}
}

func TestWriteTransactionCount(t *testing.T) {
	d := openTestDriver(t)
	kind := graph.StringKind("CountTest")

	// Create 3 nodes
	for i := 0; i < 3; i++ {
		if err := d.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
			_, err := tx.CreateNode(graph.NewProperties(), kind)
			return err
		}); err != nil {
			t.Fatalf("WriteTransaction failed: %v", err)
		}
	}

	var count int64
	if err := d.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:`CountTest`) RETURN count(n)", nil)
		defer result.Close()
		if result.Next() {
			vals := result.Values()
			if len(vals) > 0 {
				count = toInt64Val(vals[0])
			}
		}
		return result.Error()
	}); err != nil {
		t.Fatalf("ReadTransaction returned error: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected count=3, got %d", count)
	}
}

// ─── BatchOperation ───────────────────────────────────────────────────────────

func TestBatchOperationCreateNodes(t *testing.T) {
	d := openTestDriver(t)
	batchKind := graph.StringKind("BatchNode")

	err := d.BatchOperation(context.Background(), func(batch graph.Batch) error {
		for i := 0; i < 5; i++ {
			node := &graph.Node{
				Kinds:      graph.Kinds{batchKind},
				Properties: graph.AsProperties(map[string]any{"idx": i}),
			}
			if err := batch.CreateNode(node); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("BatchOperation returned error: %v", err)
	}

	// Verify all 5 nodes were created
	var count int64
	if err := d.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:`BatchNode`) RETURN count(n)", nil)
		defer result.Close()
		if result.Next() {
			count = toInt64Val(result.Values()[0])
		}
		return result.Error()
	}); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 5 {
		t.Fatalf("expected 5 batch nodes, got %d", count)
	}
}

func TestBatchOperationCreateNodeNoKind(t *testing.T) {
	d := openTestDriver(t)
	err := d.BatchOperation(context.Background(), func(batch graph.Batch) error {
		return batch.CreateNode(&graph.Node{})
	})
	if err == nil {
		t.Fatal("expected error when creating batch node with no kind")
	}
}

func TestBatchOperationCreateRelationships(t *testing.T) {
	d := openTestDriver(t)
	var srcID, dstID graph.ID

	// Create source and destination nodes first
	if err := d.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		src, err := tx.CreateNode(graph.NewProperties(), graph.StringKind("BatchSrc"))
		if err != nil {
			return err
		}
		srcID = src.ID
		dst, err := tx.CreateNode(graph.NewProperties(), graph.StringKind("BatchDst"))
		if err != nil {
			return err
		}
		dstID = dst.ID
		return nil
	}); err != nil {
		t.Fatalf("setup WriteTransaction failed: %v", err)
	}

	err := d.BatchOperation(context.Background(), func(batch graph.Batch) error {
		return batch.CreateRelationshipByIDs(srcID, dstID, graph.StringKind("BatchEdge"), nil)
	})
	if err != nil {
		t.Fatalf("BatchOperation create relationship failed: %v", err)
	}
}

func TestBatchOperationCommit(t *testing.T) {
	d := openTestDriver(t)
	err := d.BatchOperation(context.Background(), func(batch graph.Batch) error {
		return batch.Commit()
	})
	if err != nil {
		t.Fatalf("BatchOperation Commit returned error: %v", err)
	}
}

func TestBatchOperationWithGraph(t *testing.T) {
	d := openTestDriver(t)
	err := d.BatchOperation(context.Background(), func(batch graph.Batch) error {
		b2 := batch.WithGraph(graph.Graph{})
		if b2 == nil {
			t.Error("WithGraph returned nil")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("BatchOperation returned error: %v", err)
	}
}

func TestBatchOperationNodesRelationships(t *testing.T) {
	d := openTestDriver(t)
	err := d.BatchOperation(context.Background(), func(batch graph.Batch) error {
		// Just ensure Nodes() and Relationships() don't panic
		_ = batch.Nodes()
		_ = batch.Relationships()
		return nil
	})
	if err != nil {
		t.Fatalf("BatchOperation returned error: %v", err)
	}
}

func TestBatchOperationDeleteNode(t *testing.T) {
	d := openTestDriver(t)
	var nodeID graph.ID

	// Create a node first
	if err := d.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		node, err := tx.CreateNode(graph.NewProperties(), graph.StringKind("ToDelete"))
		if err != nil {
			return err
		}
		nodeID = node.ID
		return nil
	}); err != nil {
		t.Fatalf("create node failed: %v", err)
	}

	// Delete via batch
	if err := d.BatchOperation(context.Background(), func(batch graph.Batch) error {
		return batch.DeleteNode(nodeID)
	}); err != nil {
		t.Fatalf("BatchOperation DeleteNode returned error: %v", err)
	}
}

func TestBatchOperationDeleteRelationship(t *testing.T) {
	d := openTestDriver(t)
	err := d.BatchOperation(context.Background(), func(batch graph.Batch) error {
		// Deleting non-existent relationship should not error
		return batch.DeleteRelationship(graph.ID(9999))
	})
	if err != nil {
		t.Fatalf("BatchOperation DeleteRelationship returned error: %v", err)
	}
}

// ─── Save ─────────────────────────────────────────────────────────────────────

func TestSaveInMemoryNoError(t *testing.T) {
	d := openTestDriver(t)
	if err := d.Save(); err != nil {
		t.Fatalf("Save() on in-memory graph returned error: %v", err)
	}
}

func TestSaveFileBacked(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "save_test.kgl")

	d, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close(context.Background())

	if err := d.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	// File must exist
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected file to exist after Save: %v", err)
	}
}

// ─── Transaction.Nodes() / Relationships() queries ────────────────────────────

func TestTransactionNodesQuery(t *testing.T) {
	d := openTestDriver(t)

	// Create a node to query
	if err := d.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		_, err := tx.CreateNode(graph.NewProperties(), graph.StringKind("QueryKind"))
		return err
	}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err := d.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		nodeQuery := tx.Nodes()
		if nodeQuery == nil {
			t.Error("Nodes() returned nil")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ReadTransaction returned error: %v", err)
	}
}

func TestTransactionRelationshipsQuery(t *testing.T) {
	d := openTestDriver(t)
	err := d.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		relQuery := tx.Relationships()
		if relQuery == nil {
			t.Error("Relationships() returned nil")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ReadTransaction returned error: %v", err)
	}
}

// ─── UpdateRelationship ────────────────────────────────────────────────────────

func TestUpdateRelationshipNilProperties(t *testing.T) {
	d := openTestDriver(t)
	err := d.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		src, err := tx.CreateNode(graph.NewProperties(), graph.StringKind("RelSrc"))
		if err != nil {
			return err
		}
		dst, err := tx.CreateNode(graph.NewProperties(), graph.StringKind("RelDst"))
		if err != nil {
			return err
		}
		rel, err := tx.CreateRelationshipByIDs(src.ID, dst.ID, graph.StringKind("RelKind"), nil)
		if err != nil {
			return err
		}
		// UpdateRelationship with nil properties should be a no-op
		return tx.UpdateRelationship(rel)
	})
	if err != nil {
		t.Fatalf("UpdateRelationship with nil properties returned error: %v", err)
	}
}

// ─── Raw query / Query alias ──────────────────────────────────────────────────

func TestTransactionQueryAliasForRaw(t *testing.T) {
	d := openTestDriver(t)
	err := d.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// Query is an alias for Raw
		result := tx.Query("MATCH (n) RETURN count(n)", nil)
		defer result.Close()
		return result.Error()
	})
	if err != nil {
		t.Fatalf("Query alias returned error: %v", err)
	}
}

func TestTransactionRawBadCypher(t *testing.T) {
	d := openTestDriver(t)
	err := d.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		result := tx.Raw("THIS IS NOT CYPHER !!!!", nil)
		defer result.Close()
		// Should have an error set
		if result.Error() == nil {
			t.Error("expected error from bad Cypher, got nil")
		}
		return nil // don't propagate to fail gracefully
	})
	if err != nil {
		t.Fatalf("ReadTransaction returned unexpected error: %v", err)
	}
}

// ─── Context cancellation ─────────────────────────────────────────────────────

func TestReadTransactionCancelledContext(t *testing.T) {
	d := openTestDriver(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// ReadTransaction may or may not check the context, but must not panic
	_ = d.ReadTransaction(ctx, func(tx graph.Transaction) error {
		return nil
	})
}

func TestWriteTransactionCancelledContext(t *testing.T) {
	d := openTestDriver(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_ = d.WriteTransaction(ctx, func(tx graph.Transaction) error {
		return nil
	})
}

// ─── Round-trip persistence ───────────────────────────────────────────────────

func TestRoundTripFilePersistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "roundtrip.kgl")

	// Phase 1: write nodes
	d1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open (phase 1) failed: %v", err)
	}
	if err := d1.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		for i := 0; i < 3; i++ {
			if _, err := tx.CreateNode(
				graph.AsProperties(map[string]any{"idx": i}),
				graph.StringKind("Persist"),
			); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("write phase failed: %v", err)
	}
	if err := d1.Close(context.Background()); err != nil {
		t.Fatalf("close phase 1 failed: %v", err)
	}

	// Phase 2: reload and count
	d2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open (phase 2) failed: %v", err)
	}
	defer d2.Close(context.Background())

	var count int64
	if err := d2.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:`Persist`) RETURN count(n)", nil)
		defer result.Close()
		if result.Next() {
			count = toInt64Val(result.Values()[0])
		}
		return result.Error()
	}); err != nil {
		t.Fatalf("read phase failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 persisted nodes, got %d", count)
	}
}
