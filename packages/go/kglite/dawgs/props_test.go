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
	"sort"
	"strings"
	"testing"
)

// ─── propsPattern ─────────────────────────────────────────────────────────────

func TestPropsPatternEmpty(t *testing.T) {
	pattern, params := propsPattern("p_", map[string]any{})
	if pattern != "" {
		t.Errorf("expected empty pattern for empty map, got %q", pattern)
	}
	if len(params) != 0 {
		t.Errorf("expected empty params for empty map, got %v", params)
	}
}

func TestPropsPatternNilMap(t *testing.T) {
	pattern, params := propsPattern("p_", nil)
	if pattern != "" {
		t.Errorf("expected empty pattern for nil map, got %q", pattern)
	}
	if len(params) != 0 {
		t.Errorf("expected empty params for nil map, got %v", params)
	}
}

func TestPropsPatternSingleProperty(t *testing.T) {
	pattern, params := propsPattern("p_", map[string]any{"name": "alice"})
	if pattern != "{name: $p_name}" {
		t.Errorf("unexpected pattern: %q", pattern)
	}
	if params["p_name"] != "alice" {
		t.Errorf("expected params[p_name]=alice, got %v", params["p_name"])
	}
}

func TestPropsPatternMultiplePropertiesSorted(t *testing.T) {
	// Keys should be sorted alphabetically
	props := map[string]any{
		"zzz":  "last",
		"aaa":  "first",
		"mmm":  42,
	}
	pattern, params := propsPattern("p_", props)
	// Should contain all three properties
	if !strings.Contains(pattern, "aaa: $p_aaa") {
		t.Errorf("pattern missing aaa: %q", pattern)
	}
	if !strings.Contains(pattern, "mmm: $p_mmm") {
		t.Errorf("pattern missing mmm: %q", pattern)
	}
	if !strings.Contains(pattern, "zzz: $p_zzz") {
		t.Errorf("pattern missing zzz: %q", pattern)
	}
	// Keys should appear in sorted order: aaa, mmm, zzz
	idxAAA := strings.Index(pattern, "aaa")
	idxMMM := strings.Index(pattern, "mmm")
	idxZZZ := strings.Index(pattern, "zzz")
	if !(idxAAA < idxMMM && idxMMM < idxZZZ) {
		t.Errorf("keys not sorted in pattern: %q", pattern)
	}
	if len(params) != 3 {
		t.Errorf("expected 3 params, got %d", len(params))
	}
}

func TestPropsPatternPrefixIsolation(t *testing.T) {
	// Two calls with different prefixes must not collide
	_, params1 := propsPattern("id_", map[string]any{"name": "alice"})
	_, params2 := propsPattern("p_", map[string]any{"name": "bob"})
	if params1["id_name"] != "alice" {
		t.Errorf("expected id_name=alice, got %v", params1)
	}
	if params2["p_name"] != "bob" {
		t.Errorf("expected p_name=bob, got %v", params2)
	}
}

func TestPropsPatternNonScalarValueIsJSON(t *testing.T) {
	// Slices should be JSON-encoded
	props := map[string]any{"kinds": []string{"A", "B"}}
	_, params := propsPattern("p_", props)
	v, ok := params["p_kinds"]
	if !ok {
		t.Fatal("expected p_kinds in params")
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected string for slice value, got %T: %v", v, v)
	}
	if s != `["A","B"]` {
		t.Errorf("unexpected JSON encoding: %q", s)
	}
}

func TestPropsPatternKeyWithDot(t *testing.T) {
	// Dots in keys should be sanitized to underscores in param names
	props := map[string]any{"some.key": "val"}
	pattern, params := propsPattern("p_", props)
	// Param name should have dot replaced with _
	if _, ok := params["p_some_key"]; !ok {
		t.Errorf("expected param p_some_key, got params: %v", params)
	}
	// The property name in the pattern should be the original key
	if !strings.Contains(pattern, "some.key: $p_some_key") {
		t.Errorf("unexpected pattern: %q", pattern)
	}
}

// ─── setClause ────────────────────────────────────────────────────────────────

func TestSetClauseEmpty(t *testing.T) {
	frag, params := setClause("n", "p_", map[string]any{})
	if frag != "" {
		t.Errorf("expected empty fragment for empty map, got %q", frag)
	}
	if len(params) != 0 {
		t.Errorf("expected empty params, got %v", params)
	}
}

func TestSetClauseSingleProperty(t *testing.T) {
	frag, params := setClause("n", "p_", map[string]any{"name": "alice"})
	if frag != "n.name = $p_name" {
		t.Errorf("unexpected setClause fragment: %q", frag)
	}
	if params["p_name"] != "alice" {
		t.Errorf("expected p_name=alice, got %v", params["p_name"])
	}
}

func TestSetClauseMultiplePropertiesSorted(t *testing.T) {
	props := map[string]any{
		"zz": 99,
		"aa": "first",
	}
	frag, params := setClause("n", "p_", props)
	if !strings.Contains(frag, "n.aa = $p_aa") {
		t.Errorf("missing n.aa in fragment: %q", frag)
	}
	if !strings.Contains(frag, "n.zz = $p_zz") {
		t.Errorf("missing n.zz in fragment: %q", frag)
	}
	// aa should appear before zz
	if strings.Index(frag, "aa") > strings.Index(frag, "zz") {
		t.Errorf("expected sorted order (aa before zz): %q", frag)
	}
	if len(params) != 2 {
		t.Errorf("expected 2 params, got %d", len(params))
	}
}

func TestSetClauseVariableName(t *testing.T) {
	frag, _ := setClause("myNode", "x_", map[string]any{"age": 30})
	if frag != "myNode.age = $x_age" {
		t.Errorf("unexpected setClause fragment: %q", frag)
	}
}

func TestSetClauseNilMap(t *testing.T) {
	frag, params := setClause("n", "p_", nil)
	if frag != "" {
		t.Errorf("expected empty fragment for nil map, got %q", frag)
	}
	if len(params) != 0 {
		t.Errorf("expected empty params for nil map, got %v", params)
	}
}

// ─── mergeParams ──────────────────────────────────────────────────────────────

func TestMergeParamsNoOverlap(t *testing.T) {
	a := map[string]any{"x": 1}
	b := map[string]any{"y": 2}
	merged := mergeParams(a, b)
	if merged["x"] != 1 {
		t.Errorf("expected x=1, got %v", merged["x"])
	}
	if merged["y"] != 2 {
		t.Errorf("expected y=2, got %v", merged["y"])
	}
	if len(merged) != 2 {
		t.Errorf("expected 2 keys, got %d", len(merged))
	}
}

func TestMergeParamsOverlapLastWins(t *testing.T) {
	a := map[string]any{"key": "first"}
	b := map[string]any{"key": "second"}
	merged := mergeParams(a, b)
	if merged["key"] != "second" {
		t.Errorf("expected key=second (last wins), got %v", merged["key"])
	}
}

func TestMergeParamsEmpty(t *testing.T) {
	merged := mergeParams()
	if len(merged) != 0 {
		t.Errorf("expected empty merged map, got %v", merged)
	}
}

func TestMergeParamsSingleMap(t *testing.T) {
	a := map[string]any{"a": 1, "b": 2}
	merged := mergeParams(a)
	if len(merged) != 2 {
		t.Errorf("expected 2 keys, got %d", len(merged))
	}
}

func TestMergeParamsThreeMaps(t *testing.T) {
	a := map[string]any{"a": 1}
	b := map[string]any{"b": 2}
	c := map[string]any{"c": 3, "a": 99} // overwrites a
	merged := mergeParams(a, b, c)
	if merged["a"] != 99 {
		t.Errorf("expected a=99 (last wins), got %v", merged["a"])
	}
	if merged["b"] != 2 {
		t.Errorf("expected b=2, got %v", merged["b"])
	}
	if merged["c"] != 3 {
		t.Errorf("expected c=3, got %v", merged["c"])
	}
}

// ─── quoteIdent ───────────────────────────────────────────────────────────────

func TestQuoteIdentNormal(t *testing.T) {
	result := quoteIdent("MyLabel")
	if result != "`MyLabel`" {
		t.Errorf("expected `MyLabel`, got %q", result)
	}
}

func TestQuoteIdentWithSpaces(t *testing.T) {
	result := quoteIdent("My Label")
	if result != "`My Label`" {
		t.Errorf("expected `My Label`, got %q", result)
	}
}

func TestQuoteIdentReservedWord(t *testing.T) {
	// Reserved Cypher words like MATCH, RETURN should be backtick-quoted
	reserved := []string{"Match", "Return", "Where", "Contains", "With"}
	for _, word := range reserved {
		result := quoteIdent(word)
		expected := "`" + word + "`"
		if result != expected {
			t.Errorf("quoteIdent(%q) = %q, expected %q", word, result, expected)
		}
	}
}

func TestQuoteIdentEmpty(t *testing.T) {
	result := quoteIdent("")
	if result != "``" {
		t.Errorf("expected ``, got %q", result)
	}
}

// ─── sanitizeKey ──────────────────────────────────────────────────────────────

func TestSanitizeKeyAlphanumeric(t *testing.T) {
	result := sanitizeKey("abc123")
	if result != "abc123" {
		t.Errorf("expected abc123, got %q", result)
	}
}

func TestSanitizeKeyWithDot(t *testing.T) {
	result := sanitizeKey("some.key")
	if result != "some_key" {
		t.Errorf("expected some_key, got %q", result)
	}
}

func TestSanitizeKeyWithHyphen(t *testing.T) {
	result := sanitizeKey("some-key")
	if result != "some_key" {
		t.Errorf("expected some_key, got %q", result)
	}
}

func TestSanitizeKeyWithSpaces(t *testing.T) {
	result := sanitizeKey("some key")
	if result != "some_key" {
		t.Errorf("expected some_key, got %q", result)
	}
}

func TestSanitizeKeyWithUnderscore(t *testing.T) {
	// Underscores should be preserved
	result := sanitizeKey("some_key")
	if result != "some_key" {
		t.Errorf("expected some_key, got %q", result)
	}
}

func TestSanitizeKeyEmpty(t *testing.T) {
	result := sanitizeKey("")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestSanitizeKeyAllSpecialChars(t *testing.T) {
	result := sanitizeKey("!@#$%^&*()")
	// All chars should be replaced with underscores
	for _, r := range result {
		if r != '_' {
			t.Errorf("expected all underscores, got %q", result)
			break
		}
	}
}

func TestSanitizeKeyMixedCase(t *testing.T) {
	result := sanitizeKey("MyKey_123")
	if result != "MyKey_123" {
		t.Errorf("expected MyKey_123, got %q", result)
	}
}

// ─── scalarize ────────────────────────────────────────────────────────────────

func TestScalarizeNil(t *testing.T) {
	result := scalarize(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestScalarizeString(t *testing.T) {
	result := scalarize("hello")
	if result != "hello" {
		t.Errorf("expected hello, got %v", result)
	}
}

func TestScalarizeInt(t *testing.T) {
	result := scalarize(42)
	if result != 42 {
		t.Errorf("expected 42, got %v", result)
	}
}

func TestScalarizeInt64(t *testing.T) {
	result := scalarize(int64(1234567890))
	if result != int64(1234567890) {
		t.Errorf("expected 1234567890, got %v", result)
	}
}

func TestScalarizeFloat64(t *testing.T) {
	result := scalarize(3.14)
	if result != 3.14 {
		t.Errorf("expected 3.14, got %v", result)
	}
}

func TestScalarizeBoolTrue(t *testing.T) {
	result := scalarize(true)
	if result != true {
		t.Errorf("expected true, got %v", result)
	}
}

func TestScalarizeBoolFalse(t *testing.T) {
	result := scalarize(false)
	if result != false {
		t.Errorf("expected false, got %v", result)
	}
}

func TestScalarizeStringSlice(t *testing.T) {
	result := scalarize([]string{"a", "b", "c"})
	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string (JSON), got %T: %v", result, result)
	}
	if s != `["a","b","c"]` {
		t.Errorf("unexpected JSON: %q", s)
	}
}

func TestScalarizeIntSlice(t *testing.T) {
	result := scalarize([]int{1, 2, 3})
	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string (JSON), got %T: %v", result, result)
	}
	if s != `[1,2,3]` {
		t.Errorf("unexpected JSON: %q", s)
	}
}

func TestScalarizeMap(t *testing.T) {
	// Maps should be JSON-encoded
	m := map[string]any{"key": "val"}
	result := scalarize(m)
	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string (JSON), got %T: %v", result, result)
	}
	if !strings.Contains(s, `"key"`) || !strings.Contains(s, `"val"`) {
		t.Errorf("unexpected JSON for map: %q", s)
	}
}

func TestScalarizeEmptySlice(t *testing.T) {
	result := scalarize([]string{})
	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string (JSON), got %T: %v", result, result)
	}
	if s != `[]` {
		t.Errorf("unexpected JSON for empty slice: %q", s)
	}
}

func TestScalarizeUint8(t *testing.T) {
	result := scalarize(uint8(255))
	if result != uint8(255) {
		t.Errorf("expected 255, got %v", result)
	}
}

func TestScalarizeUint32(t *testing.T) {
	result := scalarize(uint32(42))
	if result != uint32(42) {
		t.Errorf("expected 42, got %v", result)
	}
}

func TestScalarizeFloat32(t *testing.T) {
	result := scalarize(float32(1.5))
	if result != float32(1.5) {
		t.Errorf("expected 1.5, got %v", result)
	}
}

// ─── integration: propsPattern + setClause + mergeParams ─────────────────────

func TestPropsAndSetClauseIntegration(t *testing.T) {
	// Simulate UpdateNodeBy: identity pattern + set clause combined
	identityMap := map[string]any{"objectid": "abc-123"}
	propsMap := map[string]any{"name": "Alice", "enabled": true}

	identityPattern, identityParams := propsPattern("id_", identityMap)
	setFrag, propParams := setClause("n", "p_", propsMap)
	allParams := mergeParams(identityParams, propParams)

	if !strings.Contains(identityPattern, "objectid: $id_objectid") {
		t.Errorf("unexpected identity pattern: %q", identityPattern)
	}
	if !strings.Contains(setFrag, "n.enabled = $p_enabled") {
		t.Errorf("unexpected set frag: %q", setFrag)
	}
	if !strings.Contains(setFrag, "n.name = $p_name") {
		t.Errorf("unexpected set frag: %q", setFrag)
	}
	if allParams["id_objectid"] != "abc-123" {
		t.Errorf("expected id_objectid=abc-123, got %v", allParams["id_objectid"])
	}
	if allParams["p_name"] != "Alice" {
		t.Errorf("expected p_name=Alice, got %v", allParams["p_name"])
	}
	if allParams["p_enabled"] != true {
		t.Errorf("expected p_enabled=true, got %v", allParams["p_enabled"])
	}
}

func TestPropsPatternResultIsValidPattern(t *testing.T) {
	// The result must start with { and end with }
	props := map[string]any{"x": 1, "y": 2}
	pattern, _ := propsPattern("p_", props)
	if !strings.HasPrefix(pattern, "{") {
		t.Errorf("pattern should start with {: %q", pattern)
	}
	if !strings.HasSuffix(pattern, "}") {
		t.Errorf("pattern should end with }: %q", pattern)
	}
}

func TestSetClauseMultiplePartsCommaSeparated(t *testing.T) {
	props := map[string]any{"a": 1, "b": 2, "c": 3}
	frag, _ := setClause("n", "p_", props)
	// Parts should be separated by ", "
	parts := strings.Split(frag, ", ")
	if len(parts) != 3 {
		t.Errorf("expected 3 comma-separated parts, got %d: %q", len(parts), frag)
	}
	// All parts should be sorted
	keys := make([]string, len(parts))
	for i, p := range parts {
		keys[i] = strings.Split(p, ".")[1]
		keys[i] = strings.Split(keys[i], " ")[0]
	}
	sorted := make([]string, len(keys))
	copy(sorted, keys)
	sort.Strings(sorted)
	for i := range keys {
		if keys[i] != sorted[i] {
			t.Errorf("expected sorted keys, got %v", keys)
			break
		}
	}
}
