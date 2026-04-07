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

//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
	"github.com/stretchr/testify/require"
)

var batchNodeKind = graph.StringKind("BatchTestNode")
var batchEdgeKind = graph.StringKind("BatchTestEdge")

// TestBatchCommit verifies that calling Commit() flushes buffered nodes to the graph.
func TestBatchCommit(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		for i := 0; i < 5; i++ {
			node := graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": fmt.Sprintf("COMMIT-%d", i),
			}), batchNodeKind)
			if err := batch.CreateNode(node); err != nil {
				return err
			}
		}
		// Explicitly commit mid-batch
		if err := batch.Commit(); err != nil {
			return err
		}

		// Add more nodes after commit
		for i := 5; i < 8; i++ {
			node := graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": fmt.Sprintf("COMMIT-%d", i),
			}), batchNodeKind)
			if err := batch.CreateNode(node); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db, "MATCH (n:BatchTestNode) RETURN count(n) AS c")
	require.Equal(t, int64(8), count)
}

// TestBatchCreateNode verifies creating nodes with various kinds and properties.
func TestBatchCreateNode(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		// Node with multiple properties
		node := graph.PrepareNode(graph.AsProperties(map[string]any{
			"objectid": "CN-1",
			"name":     "test_node",
			"enabled":  true,
		}), batchNodeKind)
		return batch.CreateNode(node)
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db, "MATCH (n:BatchTestNode) WHERE n.objectid = 'CN-1' RETURN count(n) AS c")
	require.Equal(t, int64(1), count)

	// Verify properties were set
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:BatchTestNode) WHERE n.objectid = 'CN-1' RETURN n.name AS name", nil)
		defer result.Close()
		require.NoError(t, result.Error())
		require.True(t, result.Next())
		require.Equal(t, "test_node", result.Values()[0])
		return result.Error()
	})
	require.NoError(t, err)
}

// TestBatchCreateNodeNoKind verifies that creating a node without a kind returns an error.
func TestBatchCreateNodeNoKind(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		node := &graph.Node{
			Properties: graph.AsProperties(map[string]any{"objectid": "NO-KIND"}),
		}
		return batch.CreateNode(node)
	})
	require.Error(t, err)
}

// TestBatchDeleteNode verifies that deleting a node via batch removes it.
func TestBatchDeleteNode(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Create a node via transaction to get its ID
	ids := createNodes(ctx, t, db, batchNodeKind, 3)

	// Delete the middle node via batch
	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.DeleteNode(ids[1])
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db, "MATCH (n:BatchTestNode) RETURN count(n) AS c")
	require.Equal(t, int64(2), count)
}

// TestBatchCreateRelationship verifies creating relationships via batch.
func TestBatchCreateRelationship(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	ids := createNodes(ctx, t, db, batchNodeKind, 3)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		// Create edges: 0->1, 1->2
		rel1 := &graph.Relationship{
			StartID:    ids[0],
			EndID:      ids[1],
			Kind:       batchEdgeKind,
			Properties: graph.NewProperties(),
		}
		if err := batch.CreateRelationship(rel1); err != nil {
			return err
		}

		rel2 := &graph.Relationship{
			StartID:    ids[1],
			EndID:      ids[2],
			Kind:       batchEdgeKind,
			Properties: graph.NewProperties(),
		}
		return batch.CreateRelationship(rel2)
	})
	require.NoError(t, err)

	assertEdgeCount(ctx, t, db, "BatchTestEdge", 2)
}

// TestBatchCreateRelationshipByIDs verifies creating relationships with properties via batch.
func TestBatchCreateRelationshipByIDs(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	ids := createNodes(ctx, t, db, batchNodeKind, 2)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.CreateRelationshipByIDs(ids[0], ids[1], batchEdgeKind,
			graph.AsProperties(map[string]any{"weight": 99}))
	})
	require.NoError(t, err)

	assertEdgeCount(ctx, t, db, "BatchTestEdge", 1)
}

// TestBatchCreateRelationshipNilKind verifies error on nil kind.
func TestBatchCreateRelationshipNilKind(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.CreateRelationshipByIDs(1, 2, nil, nil)
	})
	require.Error(t, err)
}

// TestBatchDeleteRelationship verifies that deleting a relationship via batch removes it.
func TestBatchDeleteRelationship(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	ids := createNodes(ctx, t, db, batchNodeKind, 2)

	// Create relationship via transaction to get its ID
	var relID graph.ID
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		rel, err := tx.CreateRelationshipByIDs(ids[0], ids[1], batchEdgeKind, graph.NewProperties())
		if err != nil {
			return err
		}
		relID = rel.ID
		return nil
	})
	require.NoError(t, err)

	// Verify it exists
	assertEdgeCount(ctx, t, db, "BatchTestEdge", 1)

	// Delete via batch
	err = db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.DeleteRelationship(relID)
	})
	require.NoError(t, err)

	assertEdgeCount(ctx, t, db, "BatchTestEdge", 0)
}

// TestBatchNodes verifies that Batch.Nodes() returns a working NodeQuery.
func TestBatchNodes(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	createNodes(ctx, t, db, batchNodeKind, 4)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		nq := batch.Nodes()
		require.NotNil(t, nq)

		count, err := nq.Filterf(func() graph.Criteria {
			return query.KindIn(query.Node(), batchNodeKind)
		}).Count()
		require.NoError(t, err)
		require.Equal(t, int64(4), count)
		return nil
	})
	require.NoError(t, err)
}

// TestBatchRelationships verifies that Batch.Relationships() returns a working RelationshipQuery.
func TestBatchRelationships(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	ids := createChain(ctx, t, db, batchNodeKind, batchEdgeKind, 4) // 3 edges
	_ = ids

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		rq := batch.Relationships()
		require.NotNil(t, rq)

		count, err := rq.Filterf(func() graph.Criteria {
			return query.KindIn(query.Relationship(), batchEdgeKind)
		}).Count()
		require.NoError(t, err)
		require.Equal(t, int64(3), count)
		return nil
	})
	require.NoError(t, err)
}

// TestBatchWithGraph verifies that WithGraph returns the batch itself (passthrough).
func TestBatchWithGraph(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		result := batch.WithGraph(graph.Graph{})
		require.NotNil(t, result)
		// WithGraph should return the same batch
		require.Equal(t, batch, result)
		return nil
	})
	require.NoError(t, err)
}

// TestBatchFlushThreshold verifies that the batch auto-flushes when the buffer exceeds flushSize.
// We create more than defaultBatchFlushSize (2000) nodes to trigger auto-flush.
func TestBatchFlushThreshold(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	const totalNodes = 2500 // > defaultBatchFlushSize (2000)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		for i := 0; i < totalNodes; i++ {
			node := graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": fmt.Sprintf("FLUSH-%d", i),
			}), batchNodeKind)
			if err := batch.CreateNode(node); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db, "MATCH (n:BatchTestNode) RETURN count(n) AS c")
	require.Equal(t, int64(totalNodes), count)
}

// TestBatchEdgeFlushThreshold verifies that edge batching auto-flushes when the edge buffer
// exceeds defaultEdgeFlushSize (5000).
func TestBatchEdgeFlushThreshold(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	// Create a pair of nodes
	ids := createNodes(ctx, t, db, batchNodeKind, 2)

	const totalEdges = 5500 // > defaultEdgeFlushSize (5000)
	edgeKinds := make([]graph.Kind, totalEdges)
	for i := 0; i < totalEdges; i++ {
		edgeKinds[i] = graph.StringKind(fmt.Sprintf("EdgeType%d", i))
	}

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		for i := 0; i < totalEdges; i++ {
			if err := batch.CreateRelationshipByIDs(ids[0], ids[1], edgeKinds[i], nil); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	// Verify at least some edges were created (exact count depends on dedup behavior)
	var edgeCount int64
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (a)-[r]->(b) RETURN count(r) AS c", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next())
		vals := result.Values()
		switch v := vals[0].(type) {
		case int64:
			edgeCount = v
		case float64:
			edgeCount = int64(v)
		}
		return result.Error()
	})
	require.NoError(t, err)
	require.Equal(t, int64(totalEdges), edgeCount, "all unique edges should be created")
}

// TestBatchCreateNodeNilProperties verifies that creating a node with nil properties works.
func TestBatchCreateNodeNilProperties(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		node := &graph.Node{
			Kinds: graph.Kinds{batchNodeKind},
		}
		return batch.CreateNode(node)
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db, "MATCH (n:BatchTestNode) RETURN count(n) AS c")
	require.Equal(t, int64(1), count)
}

// TestBatchMultipleCommits verifies that multiple explicit commits within a single batch work.
func TestBatchMultipleCommits(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		// First batch of nodes
		for i := 0; i < 3; i++ {
			node := graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": fmt.Sprintf("MC-%d", i),
			}), batchNodeKind)
			if err := batch.CreateNode(node); err != nil {
				return err
			}
		}
		if err := batch.Commit(); err != nil {
			return err
		}

		// Second batch of nodes
		for i := 3; i < 6; i++ {
			node := graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": fmt.Sprintf("MC-%d", i),
			}), batchNodeKind)
			if err := batch.CreateNode(node); err != nil {
				return err
			}
		}
		if err := batch.Commit(); err != nil {
			return err
		}

		// Third batch
		for i := 6; i < 10; i++ {
			node := graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": fmt.Sprintf("MC-%d", i),
			}), batchNodeKind)
			if err := batch.CreateNode(node); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db, "MATCH (n:BatchTestNode) RETURN count(n) AS c")
	require.Equal(t, int64(10), count)
}

// TestBatchCreateRelationshipNilProperties verifies creating edges with nil properties.
func TestBatchCreateRelationshipNilProperties(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	ids := createNodes(ctx, t, db, batchNodeKind, 2)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.CreateRelationshipByIDs(ids[0], ids[1], batchEdgeKind, nil)
	})
	require.NoError(t, err)

	assertEdgeCount(ctx, t, db, "BatchTestEdge", 1)
}

// TestBatchCreateRelationshipEmptyKind verifies error on empty kind string.
func TestBatchCreateRelationshipEmptyKind(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.CreateRelationshipByIDs(1, 2, graph.StringKind(""), nil)
	})
	require.Error(t, err)
}

// TestBatchUpdateRelationshipBy verifies upserting a relationship with start/end nodes.
func TestBatchUpdateRelationshipBy(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	var batchAltKind = graph.StringKind("BatchAltKind")

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Relationship: &graph.Relationship{
				Kind:       batchEdgeKind,
				Properties: graph.AsProperties(map[string]any{"weight": 10}),
			},
			Start: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "URB-START-1",
				"name":     "start_node",
			}), batchNodeKind, batchAltKind),
			StartIdentityKind:       batchNodeKind,
			StartIdentityProperties: []string{"objectid"},
			End: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "URB-END-1",
				"name":     "end_node",
			}), batchNodeKind, batchAltKind),
			EndIdentityKind:       batchNodeKind,
			EndIdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err)

	// Verify the relationship was created
	assertEdgeCount(ctx, t, db, "BatchTestEdge", 1)

	// Upsert again -- should not create a duplicate
	err = db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Relationship: &graph.Relationship{
				Kind:       batchEdgeKind,
				Properties: graph.AsProperties(map[string]any{"weight": 20}),
			},
			Start: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "URB-START-1",
				"name":     "start_node_v2",
			}), batchNodeKind, batchAltKind),
			StartIdentityKind:       batchNodeKind,
			StartIdentityProperties: []string{"objectid"},
			End: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "URB-END-1",
				"name":     "end_node_v2",
			}), batchNodeKind, batchAltKind),
			EndIdentityKind:       batchNodeKind,
			EndIdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err)

	assertEdgeCount(ctx, t, db, "BatchTestEdge", 1)
}

// TestBatchUpdateRelationshipByNilRelationship verifies error when relationship is nil.
func TestBatchUpdateRelationshipByNilRelationship(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{})
	})
	require.Error(t, err)
}

// TestBatchUpdateRelationshipByNilKind verifies error when relationship kind is nil.
func TestBatchUpdateRelationshipByNilKind(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Relationship: &graph.Relationship{
				Properties: graph.NewProperties(),
			},
		})
	})
	require.Error(t, err)
}

// TestBatchUpdateRelationshipByEmptyKind verifies error when relationship kind is empty string.
func TestBatchUpdateRelationshipByEmptyKind(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Relationship: &graph.Relationship{
				Kind:       graph.StringKind(""),
				Properties: graph.NewProperties(),
			},
		})
	})
	require.Error(t, err)
}

// TestBatchUpdateRelationshipByNoIdentityKinds verifies UpdateRelationshipBy works
// without start/end identity kinds (no label on MERGE).
func TestBatchUpdateRelationshipByNoIdentityKinds(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Relationship: &graph.Relationship{
				Kind:       batchEdgeKind,
				Properties: graph.NewProperties(),
			},
			Start: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "NI-START",
			}), batchNodeKind),
			StartIdentityProperties: []string{"objectid"},
			End: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "NI-END",
			}), batchNodeKind),
			EndIdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err)

	assertEdgeCount(ctx, t, db, "BatchTestEdge", 1)
}

// TestBatchUpdateNodeByNilNode verifies error when node is nil.
func TestBatchUpdateNodeByNilNode(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateNodeBy(graph.NodeUpdate{})
	})
	require.Error(t, err)
}

// TestBatchUpdateNodeByMultipleKinds verifies that UpdateNodeBy handles nodes
// with multiple kinds correctly.
func TestBatchUpdateNodeByMultipleKinds(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	var batchSecondKind = graph.StringKind("BatchSecondKind")

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "MK-1",
				"name":     "multi_kind_node",
			}), batchNodeKind, batchSecondKind),
			IdentityKind:       batchNodeKind,
			IdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db, "MATCH (n:BatchTestNode) WHERE n.objectid = 'MK-1' RETURN count(n) AS c")
	require.Equal(t, int64(1), count)
}

// TestBatchUpdateNodeByNoIdentityKind verifies that UpdateNodeBy falls back to
// the first node kind when IdentityKind is nil.
func TestBatchUpdateNodeByNoIdentityKind(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "NIK-1",
			}), batchNodeKind),
			IdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db, "MATCH (n:BatchTestNode) WHERE n.objectid = 'NIK-1' RETURN count(n) AS c")
	require.Equal(t, int64(1), count)
}

// TestBatchUpdateNodeByNilProperties verifies UpdateNodeBy with nil properties.
func TestBatchUpdateNodeByNilProperties(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateNodeBy(graph.NodeUpdate{
			Node: &graph.Node{
				Kinds: graph.Kinds{batchNodeKind},
			},
			IdentityKind: batchNodeKind,
		})
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db, "MATCH (n:BatchTestNode) RETURN count(n) AS c")
	require.Equal(t, int64(1), count)
}

// TestBatchUpdateRelationshipByWithProperties verifies that UpdateRelationshipBy
// correctly sets node properties on start and end nodes.
func TestBatchUpdateRelationshipByWithProperties(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Relationship: &graph.Relationship{
				Kind:       batchEdgeKind,
				Properties: graph.NewProperties(),
			},
			Start: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "WP-START",
				"name":     "start_with_props",
			}), batchNodeKind),
			StartIdentityKind:       batchNodeKind,
			StartIdentityProperties: []string{"objectid"},
			End: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "WP-END",
				"name":     "end_with_props",
			}), batchNodeKind),
			EndIdentityKind:       batchNodeKind,
			EndIdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err)

	// Verify start node properties
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:BatchTestNode) WHERE n.objectid = 'WP-START' RETURN n.name AS name", nil)
		defer result.Close()
		require.NoError(t, result.Error())
		require.True(t, result.Next())
		require.Equal(t, "start_with_props", result.Values()[0])
		return result.Error()
	})
	require.NoError(t, err)
}

// TestBatchUpdateRelationshipByNilRelProps verifies UpdateRelationshipBy with nil relationship properties.
func TestBatchUpdateRelationshipByNilRelProps(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		return batch.UpdateRelationshipBy(graph.RelationshipUpdate{
			Relationship: &graph.Relationship{
				Kind: batchEdgeKind,
			},
			Start: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "NRP-START",
			}), batchNodeKind),
			StartIdentityKind:       batchNodeKind,
			StartIdentityProperties: []string{"objectid"},
			End: graph.PrepareNode(graph.AsProperties(map[string]any{
				"objectid": "NRP-END",
			}), batchNodeKind),
			EndIdentityKind:       batchNodeKind,
			EndIdentityProperties: []string{"objectid"},
		})
	})
	require.NoError(t, err)

	assertEdgeCount(ctx, t, db, "BatchTestEdge", 1)
}

// TestBatchDuplicateEdgesDedup verifies that duplicate edges within a single batch are deduplicated.
func TestBatchDuplicateEdgesDedup(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	ids := createNodes(ctx, t, db, batchNodeKind, 2)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		// Create the same edge 3 times
		for i := 0; i < 3; i++ {
			if err := batch.CreateRelationshipByIDs(ids[0], ids[1], batchEdgeKind, nil); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	// Should only have 1 edge due to deduplication
	assertEdgeCount(ctx, t, db, "BatchTestEdge", 1)
}

// TestBatchDuplicateEdgesAcrossFlushes verifies that edge deduplication works across
// multiple flushes within a single batch operation.
func TestBatchDuplicateEdgesAcrossFlushes(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	ids := createNodes(ctx, t, db, batchNodeKind, 2)

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		// Create an edge
		if err := batch.CreateRelationshipByIDs(ids[0], ids[1], batchEdgeKind, nil); err != nil {
			return err
		}
		// Force flush
		if err := batch.Commit(); err != nil {
			return err
		}
		// Create the same edge again after flush
		return batch.CreateRelationshipByIDs(ids[0], ids[1], batchEdgeKind, nil)
	})
	require.NoError(t, err)

	// Should only have 1 edge due to cross-flush deduplication
	assertEdgeCount(ctx, t, db, "BatchTestEdge", 1)
}
