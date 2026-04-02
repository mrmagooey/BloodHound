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
	"github.com/stretchr/testify/require"
)

var edgeCaseKind = graph.StringKind("EdgeCaseNode")

// --- Error Path Tests ---

// TestErrorCypherSyntaxError verifies that invalid Cypher returns a non-nil error
// with a meaningful message.
func TestErrorCypherSyntaxError(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("INVALID CYPHER!!!", nil)
		defer result.Close()
		return result.Error()
	})
	require.Error(t, err, "invalid Cypher should produce an error")
	require.NotEmpty(t, err.Error(), "error message should be non-empty")
}

// TestErrorEmptyKinds verifies that creating a node with no kinds returns an error.
func TestErrorEmptyKinds(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateNode(
			graph.AsProperties(map[string]any{"name": "no_kinds"}),
			// No kinds provided
		)
		return err
	})
	require.Error(t, err, "CreateNode with no kinds should error")
}

// TestErrorWriteInReadTransaction verifies behavior when attempting a write
// operation inside a read transaction. The driver may or may not enforce this;
// this test documents the current behavior.
func TestErrorWriteInReadTransaction(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("CREATE (n:EdgeCaseNode {name: 'should_fail'})", nil)
		defer result.Close()
		return result.Error()
	})

	// kglite does not currently enforce read-only transactions at the driver
	// level, so this may succeed. Document whichever behavior occurs.
	if err != nil {
		t.Logf("write in read transaction correctly rejected: %v", err)
	} else {
		t.Logf("write in read transaction was not rejected (kglite does not enforce read-only at driver level)")
	}
}

// --- Edge Case Tests ---

// TestEdgeCaseQueryOnEmptyGraph verifies that querying an empty graph returns
// zero results rather than an error.
func TestEdgeCaseQueryOnEmptyGraph(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	count := runQueryInt64(ctx, t, db, "MATCH (n) RETURN count(n) AS c")
	require.Equal(t, int64(0), count, "empty graph should return count 0")
}

// TestEdgeCaseNilProperties verifies behavior when creating a node with nil properties.
func TestEdgeCaseNilProperties(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateNode(nil, edgeCaseKind)
		return err
	})

	// Document whether nil properties works or returns an error.
	if err != nil {
		t.Logf("CreateNode with nil properties returned error (acceptable): %v", err)
	} else {
		t.Logf("CreateNode with nil properties succeeded")
		// Verify the node actually exists
		count := runQueryInt64(ctx, t, db, "MATCH (n:EdgeCaseNode) RETURN count(n) AS c")
		require.Equal(t, int64(1), count, "node with nil properties should exist")
	}
}

// TestEdgeCaseDeleteNonExistentNode verifies that deleting a node that does not
// exist is a no-op and does not produce an error.
func TestEdgeCaseDeleteNonExistentNode(t *testing.T) {
	ctx := context.Background()
	db := openGraph(t)

	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n) WHERE id(n) = 999999 DELETE n", nil)
		defer result.Close()
		return result.Error()
	})
	require.NoError(t, err, "deleting a non-existent node should be a no-op, not an error")
}

// TestEdgeCaseDoubleClose verifies that closing the driver twice does not panic.
func TestEdgeCaseDoubleClose(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "double_close.kgl")
	db, err := kglitedawgs.Open(graphPath)
	require.NoError(t, err)

	ctx := context.Background()

	// First close should succeed
	err = db.Close(ctx)
	require.NoError(t, err, "first close should succeed")

	// Second close should not panic (nil-safe)
	require.NotPanics(t, func() {
		err = db.Close(ctx)
	}, "second close should not panic")
	// The second close may return nil since kg is already nil
	t.Logf("second close returned: %v", err)
}

// TestEdgeCaseOpenNonExistentDirectory verifies behavior when opening a graph
// at a path where the parent directory does not exist.
func TestEdgeCaseOpenNonExistentDirectory(t *testing.T) {
	db, err := kglitedawgs.Open("/nonexistent/path/graph.db")

	// kglite.Open falls back to kglite.New() when Load fails, so this should
	// succeed (creating an in-memory graph). Document actual behavior.
	if err != nil {
		t.Logf("Open with non-existent path returned error (acceptable): %v", err)
	} else {
		require.NotNil(t, db, "driver should be non-nil when Open succeeds")
		t.Logf("Open with non-existent path succeeded (fallback to new graph)")
		// Clean up
		db.Close(context.Background())
	}
}
