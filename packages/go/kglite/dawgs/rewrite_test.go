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
	"fmt"
	"reflect"
	"testing"

	"github.com/specterops/dawgs/graph"
)

func TestRewriteMultiTypeRel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "basic two types without variable",
			input:  "MATCH (a)-[:TypeA|TypeB]->(b) RETURN b",
			expect: "MATCH (a)-[_kgrt0]->(b) WHERE type(_kgrt0) IN ['TypeA', 'TypeB'] RETURN b",
		},
		{
			name:   "with named variable",
			input:  "MATCH (a)-[r:TypeA|TypeB]->(b) RETURN r",
			expect: "MATCH (a)-[r]->(b) WHERE type(r) IN ['TypeA', 'TypeB'] RETURN r",
		},
		{
			name:   "three types",
			input:  "MATCH (a)-[:A|B|C]->(b) RETURN b",
			expect: "MATCH (a)-[_kgrt0]->(b) WHERE type(_kgrt0) IN ['A', 'B', 'C'] RETURN b",
		},
		{
			name:   "single type passthrough",
			input:  "MATCH (a)-[r:SingleType]->(b) RETURN r",
			expect: "MATCH (a)-[r:SingleType]->(b) RETURN r",
		},
		{
			name:   "no relationship pattern passthrough",
			input:  "MATCH (n) RETURN n",
			expect: "MATCH (n) RETURN n",
		},
		{
			name:   "existing WHERE clause",
			input:  "MATCH (a)-[:TypeA|TypeB]->(b) WHERE b.name = 'x' RETURN b",
			expect: "MATCH (a)-[_kgrt0]->(b) WHERE type(_kgrt0) IN ['TypeA', 'TypeB'] AND b.name = 'x' RETURN b",
		},
		{
			name:   "multiple relationships in one query",
			input:  "MATCH (a)-[:X|Y]->(b)-[:M|N]->(c) RETURN c",
			expect: "MATCH (a)-[_kgrt0]->(b)-[_kgrt1]->(c) WHERE type(_kgrt0) IN ['X', 'Y'] AND type(_kgrt1) IN ['M', 'N'] RETURN c",
		},
		{
			name:   "backtick-quoted types",
			input:  "MATCH (a)-[:`Contains`|MemberOf]->(b) RETURN b",
			expect: "MATCH (a)-[_kgrt0]->(b) WHERE type(_kgrt0) IN ['Contains', 'MemberOf'] RETURN b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := rewriteMultiTypeRel(tt.input)
			if got != tt.expect {
				t.Errorf("rewriteMultiTypeRel(%q)\n  got:    %q\n  expect: %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestRewriteLabelWhere(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "basic label check",
			input:  "MATCH (n) WHERE n:Computer RETURN n",
			expect: `MATCH (n) WHERE labels(n) CONTAINS '"Computer"' RETURN n`,
		},
		{
			name:   "combined with other condition",
			input:  "MATCH (n) WHERE n:Computer AND n.enabled RETURN n",
			expect: `MATCH (n) WHERE labels(n) CONTAINS '"Computer"' AND n.enabled RETURN n`,
		},
		{
			name:   "multiple label checks with OR",
			input:  "MATCH (n) WHERE n:User OR n:Computer RETURN n",
			expect: `MATCH (n) WHERE labels(n) CONTAINS '"User"' OR labels(n) CONTAINS '"Computer"' RETURN n`,
		},
		{
			name:   "no WHERE clause passthrough",
			input:  "MATCH (n) RETURN n",
			expect: "MATCH (n) RETURN n",
		},
		{
			name:   "WHERE with no label check passthrough",
			input:  "MATCH (n) WHERE n.name = 'test' RETURN n",
			expect: "MATCH (n) WHERE n.name = 'test' RETURN n",
		},
		{
			name:   "label in MATCH not rewritten",
			input:  "MATCH (n:Computer) RETURN n",
			expect: "MATCH (n:Computer) RETURN n",
		},
		{
			name:   "colon in string literal in WHERE NOT rewritten",
			input:  "MATCH (n) WHERE n.name = 'foo:bar' RETURN n",
			expect: `MATCH (n) WHERE n.name = 'foo:bar' RETURN n`,
		},
		{
			name:   "multi-MATCH with property block not rewritten",
			input:  "MATCH (gg:Group) WHERE gg.members_count IS NOT NULL MATCH (g)-[r2{isacl:true}]->(n) RETURN g.name",
			expect: "MATCH (gg:Group) WHERE gg.members_count IS NOT NULL MATCH (g)-[r2{isacl:true}]->(n) RETURN g.name",
		},
		{
			name:   "multi-MATCH with relationship type not rewritten",
			input:  "MATCH (n:Group) WHERE n.name STARTS WITH 'PRE' MATCH (m)-[r:MemberOf]->(n) RETURN m.name",
			expect: "MATCH (n:Group) WHERE n.name STARTS WITH 'PRE' MATCH (m)-[r:MemberOf]->(n) RETURN m.name",
		},
		{
			name:   "label check in second WHERE still rewritten",
			input:  "MATCH (a) WHERE a.x = 1 MATCH (b) WHERE b:Computer RETURN b",
			expect: `MATCH (a) WHERE a.x = 1 MATCH (b) WHERE labels(b) CONTAINS '"Computer"' RETURN b`,
		},
		{
			name:   "label check in nested parens",
			input:  "match (n) where n.objectid ends with $p0 and not ((n:Group or n:ADLocalGroup)) return n",
			expect: `match (n) where n.objectid ends with $p0 and not ((labels(n) CONTAINS '"Group"' or labels(n) CONTAINS '"ADLocalGroup"')) return n`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := rewriteLabelWhere(tt.input)
			if got != tt.expect {
				t.Errorf("rewriteLabelWhere(%q)\n  got:    %q\n  expect: %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestRewriteInParam(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		params       map[string]any
		expect       string
		expectParams map[string]any // params after call
	}{
		{
			name:         "string slice",
			input:        "WHERE n.id IN $ids RETURN n",
			params:       map[string]any{"ids": []string{"a", "b"}},
			expect:       "WHERE n.id IN ['a', 'b'] RETURN n",
			expectParams: map[string]any{},
		},
		{
			name:         "integer slice",
			input:        "WHERE n.id IN $ids RETURN n",
			params:       map[string]any{"ids": []int{1, 2, 3}},
			expect:       "WHERE n.id IN [1, 2, 3] RETURN n",
			expectParams: map[string]any{},
		},
		{
			name:         "empty slice",
			input:        "WHERE n.id IN $ids RETURN n",
			params:       map[string]any{"ids": []string{}},
			expect:       "WHERE n.id IN [] RETURN n",
			expectParams: map[string]any{},
		},
		{
			name:         "non-slice param unchanged",
			input:        "WHERE n.id IN $id RETURN n",
			params:       map[string]any{"id": "single"},
			expect:       "WHERE n.id IN $id RETURN n",
			expectParams: map[string]any{"id": "single"},
		},
		{
			name:         "missing param unchanged",
			input:        "WHERE n.id IN $missing RETURN n",
			params:       map[string]any{"other": "val"},
			expect:       "WHERE n.id IN $missing RETURN n",
			expectParams: map[string]any{"other": "val"},
		},
		{
			name:         "param consumed after expansion",
			input:        "WHERE n.id IN $ids RETURN n",
			params:       map[string]any{"ids": []string{"x"}, "keep": "yes"},
			expect:       "WHERE n.id IN ['x'] RETURN n",
			expectParams: map[string]any{"keep": "yes"},
		},
		{
			name:         "multiple IN params",
			input:        "WHERE n.id IN $ids AND n.name IN $names RETURN n",
			params:       map[string]any{"ids": []int{1, 2}, "names": []string{"a", "b"}},
			expect:       "WHERE n.id IN [1, 2] AND n.name IN ['a', 'b'] RETURN n",
			expectParams: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteInParam(tt.input, tt.params)
			if got != tt.expect {
				t.Errorf("rewriteInParam(%q)\n  got:    %q\n  expect: %q", tt.input, got, tt.expect)
			}
			// Check that params map was correctly modified
			if len(tt.params) != len(tt.expectParams) {
				t.Errorf("params after call: got %v, expect %v", tt.params, tt.expectParams)
			} else {
				for k, v := range tt.expectParams {
					if tt.params[k] != v {
						t.Errorf("params[%q] = %v, expect %v", k, tt.params[k], v)
					}
				}
			}
		})
	}
}

func TestRewriteEmptyWhere(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "empty WHERE before RETURN",
			input:  "match (n) where  return n",
			expect: "match (n) return n",
		},
		{
			name:   "empty WHERE before RETURN uppercase",
			input:  "MATCH (n) WHERE  RETURN n",
			expect: "MATCH (n) RETURN n",
		},
		{
			name:   "empty WHERE before ORDER",
			input:  "MATCH (n) WHERE ORDER BY n.name RETURN n",
			expect: "MATCH (n) ORDER BY n.name RETURN n",
		},
		{
			name:   "empty WHERE before LIMIT",
			input:  "MATCH (n) WHERE LIMIT 10",
			expect: "MATCH (n) LIMIT 10",
		},
		{
			name:   "non-empty WHERE unchanged",
			input:  "MATCH (n) WHERE n.name = 'test' RETURN n",
			expect: "MATCH (n) WHERE n.name = 'test' RETURN n",
		},
		{
			name:   "no WHERE unchanged",
			input:  "MATCH (n) RETURN n",
			expect: "MATCH (n) RETURN n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := rewriteEmptyWhere(tt.input)
			if got != tt.expect {
				t.Errorf("rewriteEmptyWhere(%q)\n  got:    %q\n  expect: %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestRewriteForKglite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		params map[string]any
		expect string
	}{
		{
			name:   "triggers all three rewrites",
			input:  "MATCH (a)-[:TypeA|TypeB]->(b) WHERE b:Computer AND b.id IN $ids RETURN b",
			params: map[string]any{"ids": []string{"x", "y"}},
			expect: `MATCH (a)-[_kgrt0]->(b) WHERE type(_kgrt0) IN ['TypeA', 'TypeB'] AND labels(b) CONTAINS '"Computer"' AND b.id IN ['x', 'y'] RETURN b`,
		},
		{
			name:   "triggers none",
			input:  "MATCH (n) WHERE n.name = 'test' RETURN n",
			params: map[string]any{},
			expect: "MATCH (n) WHERE n.name = 'test' RETURN n",
		},
		{
			name:   "empty WHERE removed",
			input:  "match (n) where  return n",
			params: map[string]any{},
			expect: "match (n) return n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteForKglite(tt.input, tt.params)
			if got != tt.expect {
				t.Errorf("rewriteForKglite(%q)\n  got:    %q\n  expect: %q", tt.input, got, tt.expect)
			}
		})
	}
}

// TestInParamToElems verifies that every concrete type handled by the type
// switch in inParamToElems produces output identical to the reflect-based
// fallback. This guards against regressions when adding or modifying arms.
func TestInParamToElems(t *testing.T) {
	t.Parallel()

	// reflectElems is the original reflect-based implementation used as the
	// reference for comparison in each test case.
	reflectElems := func(val any) ([]string, bool) {
		rv := reflect.ValueOf(val)
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return nil, false
		}
		elems := make([]string, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			v := rv.Index(i).Interface()
			switch typed := v.(type) {
			case string:
				elems[i] = "'" + typed + "'"
			default:
				elems[i] = fmt.Sprintf("%v", v)
			}
		}
		return elems, true
	}

	tests := []struct {
		name        string
		val         any
		wantOk      bool
		wantElems   []string // if nil, compare against reflectElems output
	}{
		// ── concrete slice types ──────────────────────────────────────────
		{
			name:      "[]string basic",
			val:       []string{"a", "b", "c"},
			wantOk:    true,
			wantElems: []string{"'a'", "'b'", "'c'"},
		},
		{
			name:      "[]string empty",
			val:       []string{},
			wantOk:    true,
			wantElems: []string{},
		},
		{
			name:      "[]string single",
			val:       []string{"only"},
			wantOk:    true,
			wantElems: []string{"'only'"},
		},
		{
			name:      "[]graph.ID",
			val:       []graph.ID{graph.ID(1), graph.ID(42), graph.ID(999)},
			wantOk:    true,
			wantElems: []string{"1", "42", "999"},
		},
		{
			name:      "[]graph.ID empty",
			val:       []graph.ID{},
			wantOk:    true,
			wantElems: []string{},
		},
		{
			name:      "[]int64",
			val:       []int64{100, 200, 300},
			wantOk:    true,
			wantElems: []string{"100", "200", "300"},
		},
		{
			name:      "[]uint64",
			val:       []uint64{10, 20},
			wantOk:    true,
			wantElems: []string{"10", "20"},
		},
		{
			name:      "[]int",
			val:       []int{1, 2, 3},
			wantOk:    true,
			wantElems: []string{"1", "2", "3"},
		},
		{
			name:      "[]int32",
			val:       []int32{7, 8},
			wantOk:    true,
			wantElems: []string{"7", "8"},
		},
		{
			name:      "[]any with strings",
			val:       []any{"x", "y"},
			wantOk:    true,
			wantElems: []string{"'x'", "'y'"},
		},
		{
			name:      "[]any with numbers",
			val:       []any{int64(1), int64(2)},
			wantOk:    true,
			wantElems: []string{"1", "2"},
		},
		{
			name:   "[]any empty",
			val:    []any{},
			wantOk: true,
			wantElems: []string{},
		},
		// ── reflect fallback ──────────────────────────────────────────────
		{
			name:   "[]float64 via reflect fallback",
			val:    []float64{1.5, 2.5},
			wantOk: true,
			// wantElems nil → compare against reflectElems output
		},
		{
			name:   "[]bool via reflect fallback",
			val:    []bool{true, false},
			wantOk: true,
		},
		// ── non-slice types ───────────────────────────────────────────────
		{
			name:   "string scalar — not a slice",
			val:    "hello",
			wantOk: false,
		},
		{
			name:   "int64 scalar — not a slice",
			val:    int64(42),
			wantOk: false,
		},
		{
			name:   "nil — not a slice",
			val:    nil,
			wantOk: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := inParamToElems(tt.val)
			if ok != tt.wantOk {
				t.Fatalf("inParamToElems(%T) ok=%v, want %v", tt.val, ok, tt.wantOk)
			}
			if !ok {
				return
			}
			// If wantElems is provided, compare directly.
			if tt.wantElems != nil {
				if len(got) != len(tt.wantElems) {
					t.Fatalf("inParamToElems(%T) len=%d, want %d; got %v, want %v",
						tt.val, len(got), len(tt.wantElems), got, tt.wantElems)
				}
				for i := range got {
					if got[i] != tt.wantElems[i] {
						t.Errorf("inParamToElems(%T)[%d] = %q, want %q", tt.val, i, got[i], tt.wantElems[i])
					}
				}
				return
			}
			// Otherwise, compare against the reflect-based reference.
			want, wantOk := reflectElems(tt.val)
			if !wantOk {
				t.Fatalf("reflectElems(%T) returned ok=false unexpectedly", tt.val)
			}
			if len(got) != len(want) {
				t.Fatalf("inParamToElems(%T) len=%d, reflectElems len=%d", tt.val, len(got), len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					t.Errorf("inParamToElems(%T)[%d] = %q, reflectElems = %q", tt.val, i, got[i], want[i])
				}
			}
		})
	}
}

// TestRewriteInParamAllConcreteTypes ensures rewriteInParam produces correct
// Cypher for each concrete slice type that inParamToElems handles directly.
func TestRewriteInParamAllConcreteTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		params map[string]any
		expect string
	}{
		{
			name:   "[]string",
			params: map[string]any{"ids": []string{"S-1-1", "S-1-2"}},
			expect: "MATCH (n) WHERE n.objectid IN ['S-1-1', 'S-1-2'] RETURN n",
		},
		{
			name:   "[]graph.ID",
			params: map[string]any{"ids": []graph.ID{graph.ID(1), graph.ID(2)}},
			expect: "MATCH (n) WHERE n.objectid IN [1, 2] RETURN n",
		},
		{
			name:   "[]int64",
			params: map[string]any{"ids": []int64{10, 20}},
			expect: "MATCH (n) WHERE n.objectid IN [10, 20] RETURN n",
		},
		{
			name:   "[]uint64",
			params: map[string]any{"ids": []uint64{30, 40}},
			expect: "MATCH (n) WHERE n.objectid IN [30, 40] RETURN n",
		},
		{
			name:   "[]int",
			params: map[string]any{"ids": []int{1, 2, 3}},
			expect: "MATCH (n) WHERE n.objectid IN [1, 2, 3] RETURN n",
		},
		{
			name:   "[]int32",
			params: map[string]any{"ids": []int32{5, 6}},
			expect: "MATCH (n) WHERE n.objectid IN [5, 6] RETURN n",
		},
		{
			name:   "[]any with strings",
			params: map[string]any{"ids": []any{"a", "b"}},
			expect: "MATCH (n) WHERE n.objectid IN ['a', 'b'] RETURN n",
		},
		{
			name:   "empty slice",
			params: map[string]any{"ids": []string{}},
			expect: "MATCH (n) WHERE n.objectid IN [] RETURN n",
		},
		{
			name:   "single element",
			params: map[string]any{"ids": []graph.ID{graph.ID(99)}},
			expect: "MATCH (n) WHERE n.objectid IN [99] RETURN n",
		},
	}

	cypher := "MATCH (n) WHERE n.objectid IN $ids RETURN n"
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := rewriteInParam(cypher, tt.params)
			if got != tt.expect {
				t.Errorf("rewriteInParam with %T\n  got:    %q\n  expect: %q", tt.params["ids"], got, tt.expect)
			}
		})
	}
}
