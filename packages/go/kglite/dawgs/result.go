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
	"encoding/json"
	"fmt"
	"math"

	"github.com/specterops/bloodhound/packages/go/kglite"
	"github.com/specterops/dawgs/graph"
)

var jsonUnmarshal = json.Unmarshal

// kgliteResult implements graph.Result over a kglite.CypherResult.
type kgliteResult struct {
	result          *kglite.CypherResult
	rowIdx          int
	err             error
	mapper          graph.ValueMapper
	convertedRow    []any // cached converted values for current row
	convertedRowIdx int   // which rowIdx the cache is for
}

func newResult(r *kglite.CypherResult) *kgliteResult {
	return &kgliteResult{
		result:          r,
		rowIdx:          -1,
		convertedRowIdx: -1,
		mapper:          graph.NewValueMapper(mapValue),
	}
}

func newErrorResult(err error) *kgliteResult {
	return &kgliteResult{err: err, rowIdx: -1, convertedRowIdx: -1, mapper: graph.NewValueMapper(mapValue)}
}

func (r *kgliteResult) Next() bool {
	if r.err != nil || r.result == nil {
		return false
	}
	r.rowIdx++
	return r.rowIdx < len(r.result.Rows)
}

func (r *kgliteResult) Keys() []string {
	if r.result == nil {
		return nil
	}
	return r.result.Columns
}

func (r *kgliteResult) Values() []any {
	if r.result == nil || r.rowIdx < 0 || r.rowIdx >= len(r.result.Rows) {
		return nil
	}
	row := r.result.Rows[r.rowIdx]

	// Check if we already converted this row (cache converted rows)
	if r.convertedRow != nil && r.convertedRowIdx == r.rowIdx {
		return r.convertedRow
	}

	vals := make([]any, len(row))
	for i, v := range row {
		vals[i] = convertValue(v)
	}
	r.convertedRow = vals
	r.convertedRowIdx = r.rowIdx
	return vals
}

func (r *kgliteResult) Mapper() graph.ValueMapper {
	return r.mapper
}

func (r *kgliteResult) Scan(targets ...any) error {
	return graph.ScanNextResult(r, targets...)
}

func (r *kgliteResult) Error() error {
	return r.err
}

func (r *kgliteResult) Close() {}

// convertValue converts a JSON-decoded value (interface{}) to a DAWGS-compatible Go value.
// kglite returns JSON objects for nodes and edges with special __ prefix fields.
func convertValue(v interface{}) interface{} {
	switch typed := v.(type) {
	case map[string]interface{}:
		// Check if it's a node (has __node_idx) or edge (has __edge_idx)
		if nodeIdx, ok := typed["__node_idx"]; ok {
			return jsonToNode(typed, nodeIdx)
		}
		if edgeIdx, ok := typed["__edge_idx"]; ok {
			return jsonToRelationship(typed, edgeIdx)
		}
		// Unknown object — return as-is
		return v
	case []interface{}:
		return v
	case string:
		// Check for path JSON encoded as string: {"__path": true, ...}
		if len(typed) > 10 && typed[0] == '{' {
			var m map[string]interface{}
			if err := jsonUnmarshal([]byte(typed), &m); err == nil {
				if _, isPath := m["__path"]; isPath {
					return jsonToPath(m)
				}
			}
		}
		// Check for JSON-encoded arrays (e.g. labels() returns '["Base", "User"]')
		if len(typed) > 1 && typed[0] == '[' {
			var arr []interface{}
			if err := jsonUnmarshal([]byte(typed), &arr); err == nil {
				for i, elem := range arr {
					arr[i] = convertJSONValue(elem)
				}
				return arr
			}
		}
		return v
	default:
		return v
	}
}

func jsonToNode(m map[string]interface{}, nodeIdxRaw interface{}) *graph.Node {
	nodeIdx := toUint64(nodeIdxRaw)
	id := graph.ID(nodeIdx)

	// Extract labels
	var kinds graph.Kinds
	if labelsRaw, ok := m["__labels"]; ok {
		if labelsSlice, ok := labelsRaw.([]interface{}); ok {
			for _, l := range labelsSlice {
				if s, ok := l.(string); ok {
					kinds = append(kinds, graph.StringKind(s))
				}
			}
		}
	}
	// Also read __kinds (extra kinds stored by UpdateNodeBy for multi-label nodes)
	if kindsRaw, ok := m["__kinds"]; ok {
		var kindsSlice []interface{}
		switch kv := kindsRaw.(type) {
		case string:
			// JSON-encoded string array
			_ = jsonUnmarshal([]byte(kv), &kindsSlice)
		case []interface{}:
			kindsSlice = kv
		}
		for _, k := range kindsSlice {
			if s, ok := k.(string); ok {
				// Avoid duplicates
				found := false
				for _, existing := range kinds {
					if existing.String() == s {
						found = true
						break
					}
				}
				if !found {
					kinds = append(kinds, graph.StringKind(s))
				}
			}
		}
	}

	// Extract properties (everything not starting with __)
	props := make(map[string]any)
	for k, val := range m {
		if len(k) > 0 && k[0] == '_' && len(k) > 1 && k[1] == '_' {
			continue
		}
		props[k] = convertJSONValue(val)
	}

	return graph.NewNode(id, graph.AsProperties(props), kinds...)
}

func jsonToRelationship(m map[string]interface{}, edgeIdxRaw interface{}) *graph.Relationship {
	edgeIdx := toUint64(edgeIdxRaw)
	id := graph.ID(edgeIdx)

	srcIdx := graph.ID(toUint64(m["__src_idx"]))
	dstIdx := graph.ID(toUint64(m["__dst_idx"]))

	var kind graph.Kind
	if typeRaw, ok := m["__type"]; ok {
		if s, ok := typeRaw.(string); ok {
			kind = graph.StringKind(s)
		}
	}

	props := make(map[string]any)
	for k, val := range m {
		if len(k) > 0 && k[0] == '_' && len(k) > 1 && k[1] == '_' {
			continue
		}
		props[k] = convertJSONValue(val)
	}

	return graph.NewRelationship(id, srcIdx, dstIdx, graph.AsProperties(props), kind)
}

func jsonToPath(m map[string]interface{}) *graph.Path {
	path := &graph.Path{}

	if nodesRaw, ok := m["nodes"].([]interface{}); ok {
		for _, nRaw := range nodesRaw {
			// Nodes may be either a plain integer index (legacy) or a full property
			// object emitted by the fixed executor (has "__node_idx" key).
			if nm, ok := nRaw.(map[string]interface{}); ok {
				if nodeIdxRaw, hasIdx := nm["__node_idx"]; hasIdx {
					path.Nodes = append(path.Nodes, jsonToNode(nm, nodeIdxRaw))
					continue
				}
			}
			// Fallback: bare integer index — create a node with no properties.
			nodeIdx := graph.ID(toUint64(nRaw))
			path.Nodes = append(path.Nodes, graph.NewNode(nodeIdx, graph.NewProperties()))
		}
	}

	if edgesRaw, ok := m["edges"].([]interface{}); ok {
		for _, eRaw := range edgesRaw {
			if em, ok := eRaw.(map[string]interface{}); ok {
				rel := jsonToRelationship(em, em["__edge_idx"])
				path.Edges = append(path.Edges, rel)
			}
		}
	}

	return path
}

// convertJSONValue normalizes JSON-decoded values to expected Go types.
func convertJSONValue(v interface{}) interface{} {
	switch typed := v.(type) {
	case float64:
		// JSON numbers come as float64; convert to int64 if integral
		if typed == math.Trunc(typed) && typed >= math.MinInt64 && typed <= math.MaxInt64 {
			return int64(typed)
		}
		return typed
	case string:
		// Detect JSON-encoded arrays stored as strings by scalarize().
		// These need to be unmarshalled back to []interface{} for DAWGS compatibility.
		if len(typed) > 1 && typed[0] == '[' {
			var arr []interface{}
			if err := jsonUnmarshal([]byte(typed), &arr); err == nil {
				// Normalize elements within the array too
				for i, elem := range arr {
					arr[i] = convertJSONValue(elem)
				}
				return arr
			}
		}
		return typed
	default:
		return v
	}
}

func toUint64(v interface{}) uint64 {
	switch typed := v.(type) {
	case float64:
		return uint64(typed)
	case int64:
		return uint64(typed)
	case uint64:
		return typed
	case int:
		return uint64(typed)
	}
	return 0
}

// mapValue is the custom mapper function for kglite result values.
func mapValue(rawValue, target any) bool {
	switch typedTarget := target.(type) {
	case *graph.Node:
		if node, ok := rawValue.(*graph.Node); ok {
			*typedTarget = *node
			return true
		}

	case *graph.Relationship:
		if rel, ok := rawValue.(*graph.Relationship); ok {
			*typedTarget = *rel
			return true
		}

	case *graph.Path:
		if p, ok := rawValue.(*graph.Path); ok {
			*typedTarget = *p
			return true
		}

	case *graph.ID:
		switch typed := rawValue.(type) {
		case float64:
			*typedTarget = graph.ID(typed)
			return true
		case int64:
			*typedTarget = graph.ID(typed)
			return true
		case uint64:
			*typedTarget = graph.ID(typed)
			return true
		case *graph.Node:
			*typedTarget = typed.ID
			return true
		case *graph.Relationship:
			*typedTarget = typed.ID
			return true
		}

	case *graph.Kinds:
		if node, ok := rawValue.(*graph.Node); ok {
			*typedTarget = node.Kinds
			return true
		}
		if slice, ok := rawValue.([]interface{}); ok {
			var kinds graph.Kinds
			for _, item := range slice {
				if s, ok := item.(string); ok {
					kinds = append(kinds, graph.StringKind(s))
				}
			}
			*typedTarget = kinds
			return true
		}

	case *graph.KindsResult:
		if node, ok := rawValue.(*graph.Node); ok {
			typedTarget.ID = node.ID
			typedTarget.Kinds = node.Kinds
			return true
		}

	case *int64:
		switch typed := rawValue.(type) {
		case float64:
			*typedTarget = int64(typed)
			return true
		case int64:
			*typedTarget = typed
			return true
		case *graph.Node:
			*typedTarget = int64(typed.ID)
			return true
		}

	case *bool:
		if b, ok := rawValue.(bool); ok {
			*typedTarget = b
			return true
		}

	case *string:
		if s, ok := rawValue.(string); ok {
			*typedTarget = s
			return true
		}
		if rawValue == nil {
			*typedTarget = ""
			return true
		}
	}

	return false
}

// emptyResult is a result with no rows.
type emptyResult struct{}

func (emptyResult) Next() bool              { return false }
func (emptyResult) Keys() []string          { return nil }
func (emptyResult) Values() []any           { return nil }
func (emptyResult) Mapper() graph.ValueMapper { return graph.NewValueMapper() }
func (emptyResult) Scan(_ ...any) error     { return fmt.Errorf("no results") }
func (emptyResult) Error() error            { return nil }
func (emptyResult) Close()                  {}
