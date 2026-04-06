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
	"path/filepath"
	"testing"

	kglitedawgs "github.com/specterops/bloodhound/packages/go/kglite/dawgs"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var saveLoadNodeKind = graph.StringKind("SaveLoadNode")
var saveLoadAltKind = graph.StringKind("SaveLoadAlt")
var saveLoadEdgeKind = graph.StringKind("SaveLoadEdge")

// TestDriverSaveLoadRoundTrip verifies that data written to a kglite graph
// survives a Close (save) followed by a re-Open (load) cycle.
//
// All node and edge properties must survive the cycle, including properties
// on nodes created via raw Cypher MERGE that have no registered column schema.
func TestDriverSaveLoadRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "roundtrip.kgl")

	// Phase 1: Open, write data, close (triggers save)
	func() {
		db, err := kglitedawgs.Open(graphPath)
		require.NoError(t, err)

		// Create nodes via Cypher (using inline properties)
		err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw(
				`CREATE (n:SaveLoadNode {name: 'alice', objectid: 'SL-1', age: 30, active: true}) RETURN n.name`, nil)
			defer result.Close()
			for result.Next() {
			}
			return result.Error()
		})
		require.NoError(t, err)

		err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw(
				`CREATE (n:SaveLoadNode {name: 'bob', objectid: 'SL-2', age: 25, active: false}) RETURN n.name`, nil)
			defer result.Close()
			for result.Next() {
			}
			return result.Error()
		})
		require.NoError(t, err)

		// Node with multiple labels
		err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw(
				`CREATE (n:SaveLoadNode:SaveLoadAlt {name: 'multi', objectid: 'SL-3'}) RETURN n.name`, nil)
			defer result.Close()
			for result.Next() {
			}
			return result.Error()
		})
		require.NoError(t, err)

		// Create relationships with properties
		err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw(
				`MATCH (a:SaveLoadNode {name: 'alice'}), (b:SaveLoadNode {name: 'bob'})
				 CREATE (a)-[:SaveLoadEdge {weight: 42}]->(b)
				 RETURN count(*) AS c`, nil)
			defer result.Close()
			for result.Next() {
			}
			return result.Error()
		})
		require.NoError(t, err)

		err = db.WriteTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw(
				`MATCH (b:SaveLoadNode {name: 'bob'}), (c:SaveLoadNode {name: 'multi'})
				 CREATE (b)-[:SaveLoadEdge {weight: 99}]->(c)
				 RETURN count(*) AS c`, nil)
			defer result.Close()
			for result.Next() {
			}
			return result.Error()
		})
		require.NoError(t, err)

		// Verify data before save
		nodeCount := runQueryInt64(ctx, t, db, "MATCH (n:SaveLoadNode) RETURN count(n) AS c")
		require.Equal(t, int64(3), nodeCount, "node count before save")

		edgeCount := runQueryInt64(ctx, t, db, "MATCH ()-[r:SaveLoadEdge]->() RETURN count(r) AS c")
		require.Equal(t, int64(2), edgeCount, "edge count before save")

		// Close triggers save to disk
		err = db.Close(ctx)
		require.NoError(t, err)
	}()

	// Phase 2: Reopen from same path and verify everything survived
	db2, err := kglitedawgs.Open(graphPath)
	require.NoError(t, err)
	defer db2.Close(ctx)

	// -- Node count --
	nodeCount := runQueryInt64(ctx, t, db2, "MATCH (n:SaveLoadNode) RETURN count(n) AS c")
	require.Equal(t, int64(3), nodeCount, "node count after reload")

	// -- Edge count --
	edgeCount := runQueryInt64(ctx, t, db2, "MATCH ()-[r:SaveLoadEdge]->() RETURN count(r) AS c")
	require.Equal(t, int64(2), edgeCount, "edge count after reload")

	// -- Node name property survives --
	err = db2.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:SaveLoadNode) WHERE n.name = 'alice' RETURN n.name AS name", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next(), "expected node with name=alice after reload")
		require.Equal(t, "alice", result.Values()[0])
		return result.Error()
	})
	require.NoError(t, err)

	// -- Relationship type is preserved --
	err = db2.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw(
			`MATCH (a:SaveLoadNode {name: 'alice'})-[r:SaveLoadEdge]->(b:SaveLoadNode {name: 'bob'})
			 RETURN r.weight AS w`, nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next(), "expected edge alice->bob after reload")
		w := result.Values()[0]
		switch v := w.(type) {
		case int64:
			require.Equal(t, int64(42), v, "edge weight")
		case float64:
			require.Equal(t, float64(42), v, "edge weight")
		default:
			t.Fatalf("unexpected type for weight: %T", w)
		}
		return result.Error()
	})
	require.NoError(t, err)

	// -- Edge properties survive (second edge) --
	err = db2.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw(
			`MATCH (b:SaveLoadNode {name: 'bob'})-[r:SaveLoadEdge]->(c:SaveLoadNode {name: 'multi'})
			 RETURN r.weight AS w`, nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next(), "expected edge bob->multi after reload")
		w := result.Values()[0]
		switch v := w.(type) {
		case int64:
			require.Equal(t, int64(99), v, "edge weight")
		case float64:
			require.Equal(t, float64(99), v, "edge weight")
		default:
			t.Fatalf("unexpected type for weight: %T", w)
		}
		return result.Error()
	})
	require.NoError(t, err)

	// -- Multiple labels survive --
	altCount := runQueryInt64(ctx, t, db2, "MATCH (n:SaveLoadAlt) RETURN count(n) AS c")
	require.Equal(t, int64(1), altCount, "SaveLoadAlt label count after reload")

	// -- Multi-label node queryable by secondary label --
	err = db2.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw(
			"MATCH (n:SaveLoadAlt) WHERE n.name = 'multi' RETURN n.name AS name", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		require.True(t, result.Next(), "expected multi-label node queryable as SaveLoadAlt")
		require.Equal(t, "multi", result.Values()[0])
		return result.Error()
	})
	require.NoError(t, err)

	// -- All node names survived --
	err = db2.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:SaveLoadNode) RETURN n.name AS name ORDER BY n.name", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		names := []string{}
		for result.Next() {
			names = append(names, result.Values()[0].(string))
		}
		require.Equal(t, []string{"alice", "bob", "multi"}, names, "all node names after reload")
		return result.Error()
	})
	require.NoError(t, err)

	// -- Non-name node properties must survive save/load --
	t.Run("node_property_persistence", func(t *testing.T) {
		err := db2.ReadTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw("MATCH (n:SaveLoadNode) WHERE n.name = 'alice' RETURN n.objectid, n.age, n.active", nil)
			defer result.Close()
			if result.Error() != nil {
				return result.Error()
			}
			require.True(t, result.Next(), "node should exist")
			vals := result.Values()

			assert.Equal(t, "SL-1", vals[0], "objectid should be preserved")
			assert.Equal(t, float64(30), vals[1], "age should be preserved")
			assert.Equal(t, true, vals[2], "active should be preserved")
			return result.Error()
		})
		require.NoError(t, err)
	})
}
