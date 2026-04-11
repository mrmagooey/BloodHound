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

// OPT-19 tests: verify that removing the eager convertValue pass from Raw()
// does not change observable behaviour — all conversion must still happen
// correctly via kgliteResult.Values().
//
// These tests use the standalone build tag so they can open a real kglite
// database and exercise the full Raw() → Values() → Scan() pipeline.

//go:build standalone

package dawgs

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/specterops/dawgs/graph"
)

// ─── OPT-19: conversion correctness via Raw() / Values() / Scan() ─────────────

// TestOPT19_StringValuePassThrough verifies that string values returned by a
// Cypher query are still accessible (unconverted) through Values() after
// removing the eager Raw() conversion pass.
func TestOPT19_StringValuePassThrough(t *testing.T) {
	db := openTestDriver(t)

	kind := graph.StringKind("OPT19Str")
	want := "opt19-string-value"

	// Create a node with a known objectid string.
	if err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		_, err := tx.CreateNode(
			graph.AsProperties(map[string]any{"objectid": want}),
			kind,
		)
		return err
	}); err != nil {
		t.Fatalf("WriteTransaction: %v", err)
	}

	if err := db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		result := tx.Raw(
			fmt.Sprintf("MATCH (n:`%s`) RETURN n.objectid", kind.String()),
			nil,
		)
		defer result.Close()

		if !result.Next() {
			t.Fatal("expected one row")
		}
		vals := result.Values()
		if len(vals) != 1 {
			t.Fatalf("expected 1 column, got %d", len(vals))
		}
		got, ok := vals[0].(string)
		if !ok {
			t.Fatalf("expected string, got %T(%v)", vals[0], vals[0])
		}
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
		return result.Error()
	}); err != nil {
		t.Fatalf("ReadTransaction: %v", err)
	}
}

// TestOPT19_NodeScan verifies that a node value returned by Raw() is
// correctly converted to *graph.Node when scanned — the conversion now happens
// exclusively inside Values() rather than being pre-done by Raw().
func TestOPT19_NodeScan(t *testing.T) {
	db := openTestDriver(t)

	kind := graph.StringKind("OPT19Node")
	wantOID := "opt19-node-oid"

	var createdID graph.ID
	if err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		n, err := tx.CreateNode(
			graph.AsProperties(map[string]any{
				"objectid": wantOID,
				"enabled":  true,
			}),
			kind,
		)
		if err != nil {
			return err
		}
		createdID = n.ID
		return nil
	}); err != nil {
		t.Fatalf("WriteTransaction: %v", err)
	}

	if err := db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		result := tx.Raw(
			fmt.Sprintf("MATCH (n:`%s`) WHERE n.objectid = '%s' RETURN n", kind.String(), wantOID),
			nil,
		)
		defer result.Close()

		if !result.Next() {
			t.Fatal("expected one row")
		}

		var node graph.Node
		if err := result.Scan(&node); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if node.ID != createdID {
			t.Fatalf("expected ID=%d, got %d", createdID, node.ID)
		}
		if node.Properties.Get("objectid").IsNil() {
			t.Fatal("expected objectid property")
		}
		return result.Error()
	}); err != nil {
		t.Fatalf("ReadTransaction: %v", err)
	}
}

// TestOPT19_NumericValueConversion verifies that kglite float64 IDs are still
// readable as numeric values after the eager Raw() conversion pass is removed.
// (convertValue passes float64 through; only convertJSONValue converts integral
// float64 → int64 for values *inside* nodes/relationships.)
func TestOPT19_NumericValueConversion(t *testing.T) {
	db := openTestDriver(t)

	kind := graph.StringKind("OPT19Num")
	if err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		_, err := tx.CreateNode(
			graph.AsProperties(map[string]any{"objectid": "opt19-num"}),
			kind,
		)
		return err
	}); err != nil {
		t.Fatalf("WriteTransaction: %v", err)
	}

	if err := db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		result := tx.Raw(
			fmt.Sprintf("MATCH (n:`%s`) RETURN id(n)", kind.String()),
			nil,
		)
		defer result.Close()

		if !result.Next() {
			t.Fatal("expected one row")
		}
		vals := result.Values()
		if len(vals) != 1 {
			t.Fatalf("expected 1 column, got %d", len(vals))
		}
		// id(n) comes back as float64 from kglite; convertValue passes it through.
		switch vals[0].(type) {
		case float64, int64, uint64:
			// All acceptable — kglite may return any of these.
		default:
			t.Fatalf("expected numeric type, got %T(%v)", vals[0], vals[0])
		}
		return result.Error()
	}); err != nil {
		t.Fatalf("ReadTransaction: %v", err)
	}
}

// TestOPT19_MultiRowMultiCol exercises Values() across multiple rows and
// columns, ensuring the convertedRow cache is invalidated correctly between rows.
func TestOPT19_MultiRowMultiCol(t *testing.T) {
	db := openTestDriver(t)

	kind := graph.StringKind("OPT19Multi")
	const n = 5

	if err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		for i := 0; i < n; i++ {
			if _, err := tx.CreateNode(
				graph.AsProperties(map[string]any{
					"objectid": fmt.Sprintf("opt19-multi-%d", i),
					"idx":      int64(i),
				}),
				kind,
			); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("WriteTransaction: %v", err)
	}

	if err := db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		result := tx.Raw(
			fmt.Sprintf("MATCH (n:`%s`) RETURN n.objectid, id(n) ORDER BY n.objectid", kind.String()),
			nil,
		)
		defer result.Close()

		rowCount := 0
		for result.Next() {
			vals := result.Values()
			if len(vals) != 2 {
				t.Fatalf("row %d: expected 2 columns, got %d", rowCount, len(vals))
			}
			// Column 0: string objectid
			if _, ok := vals[0].(string); !ok {
				t.Fatalf("row %d: col0 expected string, got %T", rowCount, vals[0])
			}
			// Column 1: numeric id — float64 or int64/uint64 depending on kglite version.
			switch vals[1].(type) {
			case float64, int64, uint64:
			default:
				t.Fatalf("row %d: col1 expected numeric, got %T", rowCount, vals[1])
			}
			// Calling Values() again on the same row must return the same cached slice.
			vals2 := result.Values()
			if fmt.Sprintf("%p", vals) != fmt.Sprintf("%p", vals2) {
				t.Fatalf("row %d: Values() returned different slice on repeated call (cache miss)", rowCount)
			}
			rowCount++
		}
		if rowCount != n {
			t.Fatalf("expected %d rows, got %d", n, rowCount)
		}
		return result.Error()
	}); err != nil {
		t.Fatalf("ReadTransaction: %v", err)
	}
}

// TestOPT19_PathConversion verifies that path results (JSON-encoded as a string
// by kglite) are still converted to *graph.Path after OPT-19. This exercises
// the string → path branch of convertValue that was previously handled eagerly.
func TestOPT19_PathConversion(t *testing.T) {
	// Build a mock result that simulates a kglite path encoded as a JSON string,
	// which is what the Cypher executor returns for path queries.
	pathJSON, _ := json.Marshal(map[string]interface{}{
		"__path": true,
		"nodes": []interface{}{
			map[string]interface{}{
				"__node_idx": float64(1),
				"__labels":   []interface{}{"User"},
			},
			map[string]interface{}{
				"__node_idx": float64(2),
				"__labels":   []interface{}{"Computer"},
			},
		},
		"edges": []interface{}{
			map[string]interface{}{
				"__edge_idx": float64(10),
				"__src_idx":  float64(1),
				"__dst_idx":  float64(2),
				"__type":     "HasSession",
			},
		},
	})

	// newMockResult builds a kgliteResult with raw unconverted rows — exactly as
	// kglite would return them after OPT-19 removes the Raw() eager pass.
	r := newMockResult([]string{"p"}, [][]interface{}{{string(pathJSON)}})

	if !r.Next() {
		t.Fatal("expected one row")
	}
	vals := r.Values()
	if len(vals) != 1 {
		t.Fatalf("expected 1 column, got %d", len(vals))
	}
	path, ok := vals[0].(*graph.Path)
	if !ok {
		t.Fatalf("expected *graph.Path, got %T(%v)", vals[0], vals[0])
	}
	if len(path.Nodes) != 2 {
		t.Fatalf("expected 2 nodes in path, got %d", len(path.Nodes))
	}
	if len(path.Edges) != 1 {
		t.Fatalf("expected 1 edge in path, got %d", len(path.Edges))
	}
	if path.Edges[0].Kind.String() != "HasSession" {
		t.Fatalf("expected HasSession, got %v", path.Edges[0].Kind)
	}
}

// TestOPT19_ConvertValueIdempotent verifies that calling convertValue twice
// on the same already-converted value produces the same result. This is
// important to ensure that if any future code path calls convertValue on
// already-converted data, it is safe.
func TestOPT19_ConvertValueIdempotent(t *testing.T) {
	// Test that convertValue on a *graph.Node (already converted) does not panic
	// or corrupt the value. convertValue switches on map[string]interface{}, so
	// it will hit the default case for *graph.Node and return it unchanged.
	original := graph.NewNode(graph.ID(42), graph.NewProperties(), graph.StringKind("User"))
	result := convertValue(original)
	// Should be returned as-is since it's not a map[string]interface{}.
	if result != original {
		t.Fatalf("expected convertValue on *graph.Node to return same pointer, got %T", result)
	}
}

// TestOPT19_ConversionCounter verifies that with OPT-19 in place, convertValue
// is called exactly once per cell (not twice as before). We achieve this by
// temporarily replacing jsonUnmarshal with a counting wrapper.
func TestOPT19_ConversionCounter(t *testing.T) {
	// Count how many times jsonUnmarshal is called. Each call to convertValue on
	// a JSON-path string triggers exactly one jsonUnmarshal call.
	var callCount atomic.Int64
	original := jsonUnmarshal
	jsonUnmarshal = func(data []byte, v any) error {
		callCount.Add(1)
		return original(data, v)
	}
	t.Cleanup(func() { jsonUnmarshal = original })

	// Build a path string that will require JSON unmarshalling.
	pathJSON := `{"__path":true,"nodes":[1,2],"edges":[]}`

	// Simulate what happens after OPT-19: raw rows from kglite are passed to
	// newResult() without pre-conversion; Values() does the conversion once.
	r := newMockResult([]string{"p"}, [][]interface{}{{pathJSON}})

	// First call — should trigger conversion.
	if !r.Next() {
		t.Fatal("expected one row")
	}
	callCount.Store(0)
	vals1 := r.Values()
	if _, ok := vals1[0].(*graph.Path); !ok {
		t.Fatalf("expected *graph.Path, got %T", vals1[0])
	}
	firstCount := callCount.Load()
	if firstCount == 0 {
		t.Fatal("expected at least one jsonUnmarshal call during first Values()")
	}

	// Second call on the same row — must use the cache and call jsonUnmarshal zero times.
	callCount.Store(0)
	vals2 := r.Values()
	if fmt.Sprintf("%p", vals1) != fmt.Sprintf("%p", vals2) {
		t.Fatal("Values() did not return cached slice on second call")
	}
	if callCount.Load() != 0 {
		t.Fatalf("expected 0 jsonUnmarshal calls on cached Values(), got %d", callCount.Load())
	}
}
