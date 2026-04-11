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
	"errors"
	"fmt"
	"testing"

	"github.com/specterops/bloodhound/packages/go/kglite"
	"github.com/specterops/dawgs/graph"
)

// ─── toUint64 ────────────────────────────────────────────────────────────────

func TestToUint64Float64(t *testing.T) {
	got := toUint64(float64(42))
	if got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestToUint64Int64(t *testing.T) {
	got := toUint64(int64(99))
	if got != 99 {
		t.Fatalf("expected 99, got %d", got)
	}
}

func TestToUint64Uint64(t *testing.T) {
	got := toUint64(uint64(7))
	if got != 7 {
		t.Fatalf("expected 7, got %d", got)
	}
}

func TestToUint64Int(t *testing.T) {
	got := toUint64(int(55))
	if got != 55 {
		t.Fatalf("expected 55, got %d", got)
	}
}

func TestToUint64UnknownType(t *testing.T) {
	// string is not handled, should return 0
	got := toUint64("hello")
	if got != 0 {
		t.Fatalf("expected 0 for unknown type, got %d", got)
	}
}

func TestToUint64Nil(t *testing.T) {
	got := toUint64(nil)
	if got != 0 {
		t.Fatalf("expected 0 for nil, got %d", got)
	}
}

// ─── convertJSONValue ─────────────────────────────────────────────────────────

func TestConvertJSONValueIntegralFloat(t *testing.T) {
	// A float64 that is an integer should be converted to int64
	got := convertJSONValue(float64(10))
	if v, ok := got.(int64); !ok || v != 10 {
		t.Fatalf("expected int64(10), got %T(%v)", got, got)
	}
}

func TestConvertJSONValueNonIntegralFloat(t *testing.T) {
	got := convertJSONValue(float64(3.14))
	if v, ok := got.(float64); !ok || v != 3.14 {
		t.Fatalf("expected float64(3.14), got %T(%v)", got, got)
	}
}

func TestConvertJSONValueString(t *testing.T) {
	got := convertJSONValue("hello")
	if v, ok := got.(string); !ok || v != "hello" {
		t.Fatalf("expected string(hello), got %T(%v)", got, got)
	}
}

func TestConvertJSONValueBool(t *testing.T) {
	got := convertJSONValue(true)
	if v, ok := got.(bool); !ok || !v {
		t.Fatalf("expected bool(true), got %T(%v)", got, got)
	}
}

func TestConvertJSONValueNil(t *testing.T) {
	got := convertJSONValue(nil)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestConvertJSONValueJSONEncodedArray(t *testing.T) {
	// A string that looks like a JSON array should be decoded
	got := convertJSONValue(`["a","b","c"]`)
	arr, ok := got.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", got)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(arr))
	}
	if arr[0] != "a" || arr[1] != "b" || arr[2] != "c" {
		t.Fatalf("unexpected array contents: %v", arr)
	}
}

func TestConvertJSONValueJSONEncodedArrayOfNumbers(t *testing.T) {
	// Numbers in the array should be normalized too
	got := convertJSONValue(`[1,2,3]`)
	arr, ok := got.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", got)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(arr))
	}
	// Each should be int64 after normalization
	for i, v := range arr {
		if _, ok := v.(int64); !ok {
			t.Fatalf("arr[%d]: expected int64, got %T", i, v)
		}
	}
}

func TestConvertJSONValueInvalidJSONArrayString(t *testing.T) {
	// A string starting with [ but invalid JSON — should be returned as-is
	got := convertJSONValue("[not json")
	if v, ok := got.(string); !ok || v != "[not json" {
		t.Fatalf("expected raw string, got %T(%v)", got, got)
	}
}

func TestConvertJSONValueShortString(t *testing.T) {
	// A short string starting with [ (len <= 1) should be returned as-is
	got := convertJSONValue("[")
	if v, ok := got.(string); !ok || v != "[" {
		t.Fatalf("expected string([), got %T(%v)", got, got)
	}
}

// ─── jsonToNode ───────────────────────────────────────────────────────────────

func TestJsonToNodeBasic(t *testing.T) {
	m := map[string]interface{}{
		"__node_idx": float64(5),
		"__labels":   []interface{}{"Computer"},
		"name":       "test-host",
	}
	node := jsonToNode(m, m["__node_idx"])
	if node == nil {
		t.Fatal("expected non-nil node")
	}
	if node.ID != graph.ID(5) {
		t.Fatalf("expected ID=5, got %d", node.ID)
	}
	if len(node.Kinds) != 1 || node.Kinds[0].String() != "Computer" {
		t.Fatalf("expected kind=Computer, got %v", node.Kinds)
	}
	namePV := node.Properties.Get("name")
	if namePV.IsNil() {
		t.Fatal("expected 'name' property to exist")
	}
	if namePV.Any() != "test-host" {
		t.Fatalf("expected name=test-host, got %v", namePV.Any())
	}
}

func TestJsonToNodeNoLabels(t *testing.T) {
	m := map[string]interface{}{
		"__node_idx": float64(1),
	}
	node := jsonToNode(m, m["__node_idx"])
	if node == nil {
		t.Fatal("expected non-nil node")
	}
	if len(node.Kinds) != 0 {
		t.Fatalf("expected no kinds, got %v", node.Kinds)
	}
}

func TestJsonToNodeMultipleLabels(t *testing.T) {
	m := map[string]interface{}{
		"__node_idx": float64(10),
		"__labels":   []interface{}{"Base", "User"},
	}
	node := jsonToNode(m, m["__node_idx"])
	if len(node.Kinds) != 2 {
		t.Fatalf("expected 2 kinds, got %v", node.Kinds)
	}
}

func TestJsonToNodeSecondaryKindsString(t *testing.T) {
	// __kinds stored as a JSON-encoded string (as done by scalarize)
	m := map[string]interface{}{
		"__node_idx": float64(3),
		"__labels":   []interface{}{"Base"},
		"__kinds":    `["Base","Domain","Entity"]`,
	}
	node := jsonToNode(m, m["__node_idx"])
	// Base is in both __labels and __kinds — no duplicates
	kindNames := make(map[string]bool)
	for _, k := range node.Kinds {
		kindNames[k.String()] = true
	}
	if !kindNames["Base"] || !kindNames["Domain"] || !kindNames["Entity"] {
		t.Fatalf("expected Base, Domain, Entity kinds, got %v", node.Kinds)
	}
	if len(node.Kinds) != 3 {
		t.Fatalf("expected exactly 3 kinds (no duplicates), got %d: %v", len(node.Kinds), node.Kinds)
	}
}

func TestJsonToNodeSecondaryKindsSlice(t *testing.T) {
	// __kinds as a native []interface{}
	m := map[string]interface{}{
		"__node_idx": float64(4),
		"__labels":   []interface{}{"Base"},
		"__kinds":    []interface{}{"Base", "User", "Entity"},
	}
	node := jsonToNode(m, m["__node_idx"])
	kindNames := make(map[string]bool)
	for _, k := range node.Kinds {
		kindNames[k.String()] = true
	}
	if len(node.Kinds) != 3 {
		t.Fatalf("expected 3 kinds (no duplicates), got %d: %v", len(node.Kinds), node.Kinds)
	}
	if !kindNames["Base"] || !kindNames["User"] || !kindNames["Entity"] {
		t.Fatalf("missing expected kind in %v", node.Kinds)
	}
}

func TestJsonToNodePrivateFieldsExcluded(t *testing.T) {
	m := map[string]interface{}{
		"__node_idx": float64(1),
		"__labels":   []interface{}{"Node"},
		"__kinds":    `["Node"]`,
		"name":       "visible",
	}
	node := jsonToNode(m, m["__node_idx"])
	// __node_idx, __labels, __kinds must NOT appear as properties
	for k := range node.Properties.Map {
		if len(k) >= 2 && k[0] == '_' && k[1] == '_' {
			t.Fatalf("found internal field %q in node properties", k)
		}
	}
	if node.Properties.Get("name").IsNil() {
		t.Fatal("expected 'name' property")
	}
}

func TestJsonToNodeNumericProperty(t *testing.T) {
	m := map[string]interface{}{
		"__node_idx": float64(1),
		"count":      float64(42),
	}
	node := jsonToNode(m, m["__node_idx"])
	countPV := node.Properties.Get("count")
	if countPV.IsNil() {
		t.Fatal("expected 'count' property")
	}
	if countPV.Any() != int64(42) {
		t.Fatalf("expected int64(42), got %T(%v)", countPV.Any(), countPV.Any())
	}
}

func TestJsonToNodeBoolProperty(t *testing.T) {
	m := map[string]interface{}{
		"__node_idx": float64(2),
		"enabled":    true,
	}
	node := jsonToNode(m, m["__node_idx"])
	enabledPV := node.Properties.Get("enabled")
	if enabledPV.IsNil() {
		t.Fatal("expected 'enabled' property")
	}
	if enabledPV.Any() != true {
		t.Fatalf("expected true, got %v", enabledPV.Any())
	}
}

// ─── jsonToRelationship ────────────────────────────────────────────────────────

func TestJsonToRelationshipBasic(t *testing.T) {
	m := map[string]interface{}{
		"__edge_idx": float64(20),
		"__src_idx":  float64(1),
		"__dst_idx":  float64(2),
		"__type":     "HasSession",
		"weight":     float64(5),
	}
	rel := jsonToRelationship(m, m["__edge_idx"])
	if rel == nil {
		t.Fatal("expected non-nil relationship")
	}
	if rel.ID != graph.ID(20) {
		t.Fatalf("expected ID=20, got %d", rel.ID)
	}
	if rel.StartID != graph.ID(1) {
		t.Fatalf("expected StartID=1, got %d", rel.StartID)
	}
	if rel.EndID != graph.ID(2) {
		t.Fatalf("expected EndID=2, got %d", rel.EndID)
	}
	if rel.Kind.String() != "HasSession" {
		t.Fatalf("expected kind=HasSession, got %s", rel.Kind.String())
	}
	wPV := rel.Properties.Get("weight")
	if wPV.IsNil() {
		t.Fatal("expected 'weight' property")
	}
	if wPV.Any() != int64(5) {
		t.Fatalf("expected int64(5), got %T(%v)", wPV.Any(), wPV.Any())
	}
}

func TestJsonToRelationshipNoType(t *testing.T) {
	m := map[string]interface{}{
		"__edge_idx": float64(1),
		"__src_idx":  float64(0),
		"__dst_idx":  float64(0),
	}
	rel := jsonToRelationship(m, m["__edge_idx"])
	if rel == nil {
		t.Fatal("expected non-nil relationship")
	}
	// Kind should be nil or empty string kind
	if rel.Kind != nil && rel.Kind.String() != "" {
		t.Fatalf("expected empty kind, got %v", rel.Kind)
	}
}

func TestJsonToRelationshipPrivateFieldsExcluded(t *testing.T) {
	m := map[string]interface{}{
		"__edge_idx": float64(5),
		"__src_idx":  float64(1),
		"__dst_idx":  float64(2),
		"__type":     "Contains",
		"visible":    "yes",
	}
	rel := jsonToRelationship(m, m["__edge_idx"])
	for k := range rel.Properties.Map {
		if len(k) >= 2 && k[0] == '_' && k[1] == '_' {
			t.Fatalf("found internal field %q in relationship properties", k)
		}
	}
	if rel.Properties.Get("visible").IsNil() {
		t.Fatal("expected 'visible' property")
	}
}

// ─── jsonToPath ────────────────────────────────────────────────────────────────

func TestJsonToPathEmpty(t *testing.T) {
	m := map[string]interface{}{
		"__path": true,
		"nodes":  []interface{}{},
		"edges":  []interface{}{},
	}
	path := jsonToPath(m)
	if path == nil {
		t.Fatal("expected non-nil path")
	}
	if len(path.Nodes) != 0 {
		t.Fatalf("expected 0 nodes, got %d", len(path.Nodes))
	}
	if len(path.Edges) != 0 {
		t.Fatalf("expected 0 edges, got %d", len(path.Edges))
	}
}

func TestJsonToPathWithNodesAndEdges(t *testing.T) {
	m := map[string]interface{}{
		"__path": true,
		"nodes":  []interface{}{float64(10), float64(20)},
		"edges": []interface{}{
			map[string]interface{}{
				"__edge_idx": float64(100),
				"__src_idx":  float64(10),
				"__dst_idx":  float64(20),
				"__type":     "MemberOf",
			},
		},
	}
	path := jsonToPath(m)
	if len(path.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(path.Nodes))
	}
	if path.Nodes[0].ID != graph.ID(10) {
		t.Fatalf("expected node[0].ID=10, got %d", path.Nodes[0].ID)
	}
	if path.Nodes[1].ID != graph.ID(20) {
		t.Fatalf("expected node[1].ID=20, got %d", path.Nodes[1].ID)
	}
	if len(path.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(path.Edges))
	}
	if path.Edges[0].Kind.String() != "MemberOf" {
		t.Fatalf("expected edge kind=MemberOf, got %v", path.Edges[0].Kind)
	}
}

func TestJsonToPathMissingFields(t *testing.T) {
	// No nodes or edges keys — should return empty path without panic
	m := map[string]interface{}{
		"__path": true,
	}
	path := jsonToPath(m)
	if path == nil {
		t.Fatal("expected non-nil path")
	}
	if len(path.Nodes) != 0 || len(path.Edges) != 0 {
		t.Fatalf("expected empty path, got nodes=%d edges=%d", len(path.Nodes), len(path.Edges))
	}
}

// ─── convertValue ─────────────────────────────────────────────────────────────

func TestConvertValueNode(t *testing.T) {
	v := map[string]interface{}{
		"__node_idx": float64(7),
		"__labels":   []interface{}{"User"},
		"name":       "alice",
	}
	got := convertValue(v)
	node, ok := got.(*graph.Node)
	if !ok {
		t.Fatalf("expected *graph.Node, got %T", got)
	}
	if node.ID != graph.ID(7) {
		t.Fatalf("expected ID=7, got %d", node.ID)
	}
}

func TestConvertValueRelationship(t *testing.T) {
	v := map[string]interface{}{
		"__edge_idx": float64(3),
		"__src_idx":  float64(1),
		"__dst_idx":  float64(2),
		"__type":     "HasSession",
	}
	got := convertValue(v)
	rel, ok := got.(*graph.Relationship)
	if !ok {
		t.Fatalf("expected *graph.Relationship, got %T", got)
	}
	if rel.ID != graph.ID(3) {
		t.Fatalf("expected ID=3, got %d", rel.ID)
	}
}

func TestConvertValueUnknownMap(t *testing.T) {
	// A map with no __node_idx or __edge_idx should be returned as-is
	v := map[string]interface{}{"foo": "bar"}
	got := convertValue(v)
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", got)
	}
	if m["foo"] != "bar" {
		t.Fatalf("expected foo=bar, got %v", m)
	}
}

func TestConvertValueString(t *testing.T) {
	got := convertValue("hello")
	if got != "hello" {
		t.Fatalf("expected 'hello', got %v", got)
	}
}

func TestConvertValuePathString(t *testing.T) {
	// A JSON-encoded path stored as a string
	pathJSON := `{"__path":true,"nodes":[1,2],"edges":[]}`
	got := convertValue(pathJSON)
	path, ok := got.(*graph.Path)
	if !ok {
		t.Fatalf("expected *graph.Path, got %T", got)
	}
	if len(path.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(path.Nodes))
	}
}

func TestConvertValuePathStringShort(t *testing.T) {
	// String that's too short to be a path — returned as-is
	got := convertValue("{short}")
	if got != "{short}" {
		t.Fatalf("expected raw string, got %v", got)
	}
}

// ─── OPT-24 prefix-check correctness ─────────────────────────────────────────

// TestConvertValuePathPrefixExact verifies that the 9-byte {"__path" prefix
// check correctly identifies a path string.
func TestConvertValuePathPrefixExact(t *testing.T) {
	pathJSON := `{"__path":true,"nodes":[],"edges":[]}`
	got := convertValue(pathJSON)
	if _, ok := got.(*graph.Path); !ok {
		t.Fatalf("expected *graph.Path, got %T", got)
	}
}

// TestConvertValuePathPrefixWithSpaces verifies that a path JSON string with a
// space after the opening brace does NOT match the prefix check (the path
// encoder never emits such strings, but correctness requires no false positives).
func TestConvertValuePathPrefixWithSpaces(t *testing.T) {
	// { "__path": ... — note the space: this should NOT be decoded as a path
	// by the prefix check (it doesn't match the 9-byte prefix {"__path").
	// It falls through to return v as-is (string).
	notPath := `{ "__path":true}`
	got := convertValue(notPath)
	if _, ok := got.(*graph.Path); ok {
		t.Fatalf("expected string pass-through, got *graph.Path (false positive)")
	}
	if got != notPath {
		t.Fatalf("expected original string, got %v", got)
	}
}

// TestConvertValueJSONObjectNotPath verifies that a plain JSON object string
// that starts with { but is NOT a path is returned as a string (no unmarshal).
func TestConvertValueJSONObjectNotPath(t *testing.T) {
	// A JSON object that starts with {"foo": ...} — no __path key
	notPath := `{"foo":"bar","baz":42}`
	got := convertValue(notPath)
	if s, ok := got.(string); !ok || s != notPath {
		t.Fatalf("expected string pass-through for non-path JSON object, got %T(%v)", got, got)
	}
}

// TestConvertValueJSONObjectStartingWithDoubleUnderscore verifies that a
// JSON object whose first key starts with __ but is NOT __path is not
// treated as a path.
func TestConvertValueJSONObjectNotPathDoubleUnderscore(t *testing.T) {
	notPath := `{"__other":true,"nodes":[]}`
	got := convertValue(notPath)
	if _, ok := got.(*graph.Path); ok {
		t.Fatalf("expected string pass-through, got *graph.Path (false positive for __other key)")
	}
	if got != notPath {
		t.Fatalf("expected original string, got %v", got)
	}
}

// TestConvertValuePathPrefixTooShort verifies that strings shorter than the
// 9-byte path prefix are returned as-is without attempting unmarshal.
func TestConvertValuePathPrefixTooShort(t *testing.T) {
	// Only 8 chars starting with {; shorter than the 9-byte prefix
	tooShort := `{"__pat`
	got := convertValue(tooShort)
	if got != tooShort {
		t.Fatalf("expected raw string for too-short prefix, got %v", got)
	}
}

// TestConvertValueArrayEmpty verifies that an empty JSON array string is
// correctly decoded.
func TestConvertValueArrayEmpty(t *testing.T) {
	got := convertValue(`[]`)
	arr, ok := got.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{} for empty array, got %T", got)
	}
	if len(arr) != 0 {
		t.Fatalf("expected empty array, got %v", arr)
	}
}

// TestConvertValueArrayStrings verifies that a JSON-encoded array of strings
// is correctly decoded by convertValue (top-level array result case).
func TestConvertValueArrayStrings(t *testing.T) {
	got := convertValue(`["User","Base","Entity"]`)
	arr, ok := got.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", got)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(arr))
	}
	if arr[0] != "User" || arr[1] != "Base" || arr[2] != "Entity" {
		t.Fatalf("unexpected array contents: %v", arr)
	}
}

// TestConvertValueArrayInvalidJSON verifies that a string starting with [
// that is not valid JSON is returned as-is.
func TestConvertValueArrayInvalidJSON(t *testing.T) {
	notArray := "[not valid json"
	got := convertValue(notArray)
	if got != notArray {
		t.Fatalf("expected raw string for invalid JSON array, got %v", got)
	}
}

func TestConvertValueSlice(t *testing.T) {
	v := []interface{}{"a", "b"}
	got := convertValue(v)
	if _, ok := got.([]interface{}); !ok {
		t.Fatalf("expected []interface{}, got %T", got)
	}
}

func TestConvertValueNumeric(t *testing.T) {
	got := convertValue(float64(3.14))
	if got != float64(3.14) {
		t.Fatalf("expected 3.14, got %v", got)
	}
}

func TestConvertValueBool(t *testing.T) {
	got := convertValue(true)
	if got != true {
		t.Fatalf("expected true, got %v", got)
	}
}

// ─── mapValue ─────────────────────────────────────────────────────────────────

func TestMapValueNode(t *testing.T) {
	node := graph.NewNode(graph.ID(1), graph.NewProperties(), graph.StringKind("User"))
	var target graph.Node
	if !mapValue(node, &target) {
		t.Fatal("mapValue should return true for *graph.Node target")
	}
	if target.ID != graph.ID(1) {
		t.Fatalf("expected ID=1, got %d", target.ID)
	}
}

func TestMapValueRelationship(t *testing.T) {
	rel := graph.NewRelationship(graph.ID(5), graph.ID(1), graph.ID(2), nil, graph.StringKind("MemberOf"))
	var target graph.Relationship
	if !mapValue(rel, &target) {
		t.Fatal("mapValue should return true for *graph.Relationship target")
	}
	if target.ID != graph.ID(5) {
		t.Fatalf("expected ID=5, got %d", target.ID)
	}
}

func TestMapValuePath(t *testing.T) {
	p := &graph.Path{
		Nodes: []*graph.Node{graph.NewNode(graph.ID(1), graph.NewProperties())},
	}
	var target graph.Path
	if !mapValue(p, &target) {
		t.Fatal("mapValue should return true for *graph.Path target")
	}
	if len(target.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(target.Nodes))
	}
}

func TestMapValueIDFromFloat64(t *testing.T) {
	var id graph.ID
	if !mapValue(float64(42), &id) {
		t.Fatal("expected mapValue to handle float64 -> *graph.ID")
	}
	if id != graph.ID(42) {
		t.Fatalf("expected ID=42, got %d", id)
	}
}

func TestMapValueIDFromInt64(t *testing.T) {
	var id graph.ID
	if !mapValue(int64(99), &id) {
		t.Fatal("expected mapValue to handle int64 -> *graph.ID")
	}
	if id != graph.ID(99) {
		t.Fatalf("expected ID=99, got %d", id)
	}
}

func TestMapValueIDFromUint64(t *testing.T) {
	var id graph.ID
	if !mapValue(uint64(77), &id) {
		t.Fatal("expected mapValue to handle uint64 -> *graph.ID")
	}
	if id != graph.ID(77) {
		t.Fatalf("expected ID=77, got %d", id)
	}
}

func TestMapValueIDFromNode(t *testing.T) {
	node := graph.NewNode(graph.ID(11), graph.NewProperties())
	var id graph.ID
	if !mapValue(node, &id) {
		t.Fatal("expected mapValue to extract ID from *graph.Node")
	}
	if id != graph.ID(11) {
		t.Fatalf("expected ID=11, got %d", id)
	}
}

func TestMapValueIDFromRelationship(t *testing.T) {
	rel := graph.NewRelationship(graph.ID(33), graph.ID(1), graph.ID(2), nil, nil)
	var id graph.ID
	if !mapValue(rel, &id) {
		t.Fatal("expected mapValue to extract ID from *graph.Relationship")
	}
	if id != graph.ID(33) {
		t.Fatalf("expected ID=33, got %d", id)
	}
}

func TestMapValueKindsFromNode(t *testing.T) {
	node := graph.NewNode(graph.ID(1), graph.NewProperties(), graph.StringKind("User"), graph.StringKind("Base"))
	var kinds graph.Kinds
	if !mapValue(node, &kinds) {
		t.Fatal("expected mapValue to extract Kinds from *graph.Node")
	}
	if len(kinds) != 2 {
		t.Fatalf("expected 2 kinds, got %d", len(kinds))
	}
}

func TestMapValueKindsFromStringSlice(t *testing.T) {
	raw := []interface{}{"Computer", "Base"}
	var kinds graph.Kinds
	if !mapValue(raw, &kinds) {
		t.Fatal("expected mapValue to handle []interface{} -> *graph.Kinds")
	}
	if len(kinds) != 2 {
		t.Fatalf("expected 2 kinds, got %d", len(kinds))
	}
}

func TestMapValueKindsResult(t *testing.T) {
	node := graph.NewNode(graph.ID(55), graph.NewProperties(), graph.StringKind("Domain"))
	var kr graph.KindsResult
	if !mapValue(node, &kr) {
		t.Fatal("expected mapValue to fill *graph.KindsResult from *graph.Node")
	}
	if kr.ID != graph.ID(55) {
		t.Fatalf("expected ID=55, got %d", kr.ID)
	}
	if len(kr.Kinds) != 1 || kr.Kinds[0].String() != "Domain" {
		t.Fatalf("expected kind=Domain, got %v", kr.Kinds)
	}
}

func TestMapValueInt64FromFloat64(t *testing.T) {
	var v int64
	if !mapValue(float64(7), &v) {
		t.Fatal("expected mapValue to handle float64 -> *int64")
	}
	if v != 7 {
		t.Fatalf("expected 7, got %d", v)
	}
}

func TestMapValueInt64FromInt64(t *testing.T) {
	var v int64
	if !mapValue(int64(13), &v) {
		t.Fatal("expected mapValue to handle int64 -> *int64")
	}
	if v != 13 {
		t.Fatalf("expected 13, got %d", v)
	}
}

func TestMapValueInt64FromNode(t *testing.T) {
	node := graph.NewNode(graph.ID(22), graph.NewProperties())
	var v int64
	if !mapValue(node, &v) {
		t.Fatal("expected mapValue to extract int64 ID from *graph.Node")
	}
	if v != 22 {
		t.Fatalf("expected 22, got %d", v)
	}
}

func TestMapValueBool(t *testing.T) {
	var b bool
	if !mapValue(true, &b) {
		t.Fatal("expected mapValue to handle bool")
	}
	if !b {
		t.Fatal("expected true")
	}
}

func TestMapValueString(t *testing.T) {
	var s string
	if !mapValue("hello", &s) {
		t.Fatal("expected mapValue to handle string")
	}
	if s != "hello" {
		t.Fatalf("expected 'hello', got %q", s)
	}
}

func TestMapValueStringNil(t *testing.T) {
	var s string
	if !mapValue(nil, &s) {
		t.Fatal("expected mapValue to handle nil -> *string as empty string")
	}
	if s != "" {
		t.Fatalf("expected empty string, got %q", s)
	}
}

func TestMapValueReturnsFalseForMismatch(t *testing.T) {
	// Wrong target type — should return false
	var s string
	if mapValue(42, &s) {
		t.Fatal("expected mapValue to return false for int -> *string")
	}
}

// ─── kgliteResult ─────────────────────────────────────────────────────────────

func newMockResult(cols []string, rows [][]interface{}) *kgliteResult {
	return newResult(&kglite.CypherResult{
		Columns: cols,
		Rows:    rows,
	})
}

func TestResultNextEmpty(t *testing.T) {
	r := newMockResult([]string{"n"}, nil)
	if r.Next() {
		t.Fatal("Next() should return false for empty result")
	}
}

func TestResultNextWithRows(t *testing.T) {
	r := newMockResult([]string{"n"}, [][]interface{}{{"a"}, {"b"}})
	if !r.Next() {
		t.Fatal("Next() should return true for first row")
	}
	if !r.Next() {
		t.Fatal("Next() should return true for second row")
	}
	if r.Next() {
		t.Fatal("Next() should return false after last row")
	}
}

func TestResultKeys(t *testing.T) {
	r := newMockResult([]string{"a", "b", "c"}, nil)
	keys := r.Keys()
	if len(keys) != 3 || keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Fatalf("unexpected keys: %v", keys)
	}
}

func TestResultKeysNilResult(t *testing.T) {
	r := &kgliteResult{rowIdx: -1, convertedRowIdx: -1}
	keys := r.Keys()
	if keys != nil {
		t.Fatalf("expected nil keys for nil result, got %v", keys)
	}
}

func TestResultValues(t *testing.T) {
	r := newMockResult([]string{"name", "age"}, [][]interface{}{{"alice", float64(30)}})
	if !r.Next() {
		t.Fatal("expected a row")
	}
	vals := r.Values()
	if len(vals) != 2 {
		t.Fatalf("expected 2 values, got %d", len(vals))
	}
	if vals[0] != "alice" {
		t.Fatalf("expected alice, got %v", vals[0])
	}
	// convertValue passes through float64 as-is (no integer conversion for raw row values);
	// only properties inside nodes/relationships get normalized via convertJSONValue.
	if vals[1] != float64(30) {
		t.Fatalf("expected float64(30), got %T(%v)", vals[1], vals[1])
	}
}

func TestResultValuesBeforeNext(t *testing.T) {
	r := newMockResult([]string{"n"}, [][]interface{}{{"row"}})
	// Not called Next() yet
	if r.Values() != nil {
		t.Fatal("expected nil Values() before Next()")
	}
}

func TestResultValuesCached(t *testing.T) {
	r := newMockResult([]string{"n"}, [][]interface{}{{"value"}})
	if !r.Next() {
		t.Fatal("expected a row")
	}
	vals1 := r.Values()
	vals2 := r.Values()
	// Pointer equality confirms caching
	if fmt.Sprintf("%p", vals1) != fmt.Sprintf("%p", vals2) {
		t.Fatal("expected Values() to return cached slice on repeated call")
	}
}

func TestResultValuesNilResult(t *testing.T) {
	r := &kgliteResult{rowIdx: 0, convertedRowIdx: -1}
	if r.Values() != nil {
		t.Fatal("expected nil Values() when result is nil")
	}
}

func TestResultError(t *testing.T) {
	r := newMockResult(nil, nil)
	if r.Error() != nil {
		t.Fatal("expected nil error on normal result")
	}
}

func TestResultErrorResult(t *testing.T) {
	err := errors.New("test error")
	r := newErrorResult(err)
	if r.Error() != err {
		t.Fatalf("expected error to be passed through, got %v", r.Error())
	}
}

func TestErrorResultNextReturnsFalse(t *testing.T) {
	r := newErrorResult(errors.New("boom"))
	if r.Next() {
		t.Fatal("Next() should return false for error result")
	}
}

func TestResultClose(t *testing.T) {
	r := newMockResult([]string{"n"}, nil)
	r.Close() // should not panic
}

func TestResultMapper(t *testing.T) {
	r := newMockResult([]string{"n"}, nil)
	// Just verify Mapper() returns without panicking; ValueMapper is a struct (not a pointer)
	_ = r.Mapper()
}

// ─── emptyResult ──────────────────────────────────────────────────────────────

func TestEmptyResultBehavior(t *testing.T) {
	r := emptyResult{}
	if r.Next() {
		t.Fatal("emptyResult.Next() should return false")
	}
	if r.Keys() != nil {
		t.Fatalf("emptyResult.Keys() should return nil, got %v", r.Keys())
	}
	if r.Values() != nil {
		t.Fatalf("emptyResult.Values() should return nil, got %v", r.Values())
	}
	if r.Error() != nil {
		t.Fatalf("emptyResult.Error() should return nil, got %v", r.Error())
	}
	r.Close() // must not panic
	if err := r.Scan(); err == nil {
		t.Fatal("emptyResult.Scan() should return an error")
	}
}

func TestEmptyResultClose(t *testing.T) {
	r := emptyResult{}
	r.Close() // must not panic
}

func TestEmptyResultMapper(t *testing.T) {
	r := emptyResult{}
	// Just verify Mapper() returns without panicking; ValueMapper is a struct (not a pointer)
	_ = r.Mapper()
}
