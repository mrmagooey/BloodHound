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

package dawgs

import (
	"testing"

	"github.com/specterops/bloodhound/packages/go/kglite"
)

func newTestBatch() *Batch {
	return &Batch{}
}

func edgeSpecsEqual(a, b []kglite.EdgeSpec) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Src != b[i].Src || a[i].Dst != b[i].Dst || a[i].Type != b[i].Type {
			return false
		}
	}
	return true
}

func TestDedupNoDuplicates(t *testing.T) {
	t.Parallel()
	b := newTestBatch()
	edges := []kglite.EdgeSpec{
		{Src: 1, Dst: 2, Type: "TypeA"},
		{Src: 3, Dst: 4, Type: "TypeB"},
		{Src: 5, Dst: 6, Type: "TypeC"},
	}
	result := b.deduplicateEdges(edges)
	if len(result) != 3 {
		t.Fatalf("expected 3 edges, got %d", len(result))
	}
	if !edgeSpecsEqual(result, edges) {
		t.Fatalf("expected edges to be unchanged, got %+v", result)
	}
}

func TestDedupWithinBatchDuplicate(t *testing.T) {
	t.Parallel()
	b := newTestBatch()
	edges := []kglite.EdgeSpec{
		{Src: 1, Dst: 2, Type: "TypeA", Props: map[string]interface{}{"ver": 1}},
		{Src: 1, Dst: 2, Type: "TypeA", Props: map[string]interface{}{"ver": 2}},
	}
	result := b.deduplicateEdges(edges)
	if len(result) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(result))
	}
	if result[0].Src != 1 || result[0].Dst != 2 || result[0].Type != "TypeA" {
		t.Fatalf("unexpected edge key: %+v", result[0])
	}
	// Last occurrence should win
	if v, ok := result[0].Props["ver"]; !ok || v != 2 {
		t.Fatalf("expected last occurrence props (ver=2), got %+v", result[0].Props)
	}
}

func TestDedupWithinBatchTriple(t *testing.T) {
	t.Parallel()
	b := newTestBatch()
	edges := []kglite.EdgeSpec{
		{Src: 1, Dst: 2, Type: "TypeA", Props: map[string]interface{}{"ver": 1}},
		{Src: 1, Dst: 2, Type: "TypeA", Props: map[string]interface{}{"ver": 2}},
		{Src: 1, Dst: 2, Type: "TypeA", Props: map[string]interface{}{"ver": 3}},
	}
	result := b.deduplicateEdges(edges)
	if len(result) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(result))
	}
	if v, ok := result[0].Props["ver"]; !ok || v != 3 {
		t.Fatalf("expected last occurrence props (ver=3), got %+v", result[0].Props)
	}
}

func TestDedupCrossFlush(t *testing.T) {
	t.Parallel()
	b := newTestBatch()
	edges := []kglite.EdgeSpec{
		{Src: 1, Dst: 2, Type: "TypeA"},
		{Src: 3, Dst: 4, Type: "TypeB"},
	}
	result1 := b.deduplicateEdges(edges)
	if len(result1) != 2 {
		t.Fatalf("first call: expected 2 edges, got %d", len(result1))
	}

	// Second call with the same edges should return empty (already seen)
	result2 := b.deduplicateEdges(edges)
	if len(result2) != 0 {
		t.Fatalf("second call: expected 0 edges, got %d: %+v", len(result2), result2)
	}
}

func TestDedupCrossFlushWithNewEdges(t *testing.T) {
	t.Parallel()
	b := newTestBatch()
	edges1 := []kglite.EdgeSpec{
		{Src: 1, Dst: 2, Type: "TypeA"},
		{Src: 3, Dst: 4, Type: "TypeB"},
	}
	result1 := b.deduplicateEdges(edges1)
	if len(result1) != 2 {
		t.Fatalf("first call: expected 2 edges, got %d", len(result1))
	}

	// Second call: mix of previously seen and new
	edges2 := []kglite.EdgeSpec{
		{Src: 1, Dst: 2, Type: "TypeA"}, // seen
		{Src: 5, Dst: 6, Type: "TypeC"}, // new
		{Src: 3, Dst: 4, Type: "TypeB"}, // seen
		{Src: 7, Dst: 8, Type: "TypeD"}, // new
	}
	result2 := b.deduplicateEdges(edges2)
	if len(result2) != 2 {
		t.Fatalf("second call: expected 2 new edges, got %d: %+v", len(result2), result2)
	}
	expected := []kglite.EdgeSpec{
		{Src: 5, Dst: 6, Type: "TypeC"},
		{Src: 7, Dst: 8, Type: "TypeD"},
	}
	if !edgeSpecsEqual(result2, expected) {
		t.Fatalf("second call: expected %+v, got %+v", expected, result2)
	}
}

func TestDedupDifferentTypesSameEndpoints(t *testing.T) {
	t.Parallel()
	b := newTestBatch()
	edges := []kglite.EdgeSpec{
		{Src: 1, Dst: 2, Type: "TypeA"},
		{Src: 1, Dst: 2, Type: "TypeB"},
	}
	result := b.deduplicateEdges(edges)
	if len(result) != 2 {
		t.Fatalf("expected 2 edges (different types), got %d", len(result))
	}
}

func TestDedupDifferentDirectionSameType(t *testing.T) {
	t.Parallel()
	b := newTestBatch()
	edges := []kglite.EdgeSpec{
		{Src: 1, Dst: 2, Type: "TypeA"},
		{Src: 2, Dst: 1, Type: "TypeA"},
	}
	result := b.deduplicateEdges(edges)
	if len(result) != 2 {
		t.Fatalf("expected 2 edges (different direction), got %d", len(result))
	}
}

func TestDedupEmptyInput(t *testing.T) {
	t.Parallel()
	b := newTestBatch()
	result := b.deduplicateEdges([]kglite.EdgeSpec{})
	if len(result) != 0 {
		t.Fatalf("expected 0 edges, got %d", len(result))
	}
}

func TestDedupPropertiesDontAffectKey(t *testing.T) {
	t.Parallel()
	b := newTestBatch()
	edges := []kglite.EdgeSpec{
		{Src: 1, Dst: 2, Type: "TypeA", Props: map[string]interface{}{"foo": "bar"}},
		{Src: 1, Dst: 2, Type: "TypeA", Props: map[string]interface{}{"foo": "baz", "extra": true}},
	}
	result := b.deduplicateEdges(edges)
	if len(result) != 1 {
		t.Fatalf("expected 1 edge (props don't affect key), got %d", len(result))
	}
	// Last one should win
	if result[0].Props["foo"] != "baz" {
		t.Fatalf("expected last props to win (foo=baz), got %+v", result[0].Props)
	}
	if result[0].Props["extra"] != true {
		t.Fatalf("expected last props to win (extra=true), got %+v", result[0].Props)
	}
}
