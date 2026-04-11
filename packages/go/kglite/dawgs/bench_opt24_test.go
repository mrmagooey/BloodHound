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

// ─── OPT-24: Avoid redundant json.Unmarshal for path and array strings ────────
//
// Background:
//   convertValue handles string values from kglite query results by performing
//   speculative json.Unmarshal calls to detect JSON-encoded paths and arrays.
//
//   Before OPT-24 the path check was:
//     len(s) > 10 && s[0] == '{'  →  json.Unmarshal → check "__path" key
//
//   This caused a full json.Unmarshal for ANY JSON object string, including
//   plain property values like {"key":"value"} that are NOT paths.
//
//   After OPT-24 the path check requires the specific 9-byte prefix {"__path"
//   before attempting unmarshal.  Because kglite's path encoder always emits
//   {"__path" as the first field, this is a necessary and sufficient prefix
//   check — plain JSON objects don't start with "__path" as the first key.
//
// Results:
//   - Plain string values: unchanged (prefix checks cheap, no unmarshal)
//   - JSON array strings: no change (same logic)
//   - Non-path JSON object strings: avoid the full unmarshal entirely
//   - Path strings: same correctness, same cost (prefix matches, unmarshal proceeds)
//
// Run with:
//   go test -bench=BenchmarkConvertValue -benchmem -benchtime=3s \
//     ./packages/go/kglite/dawgs/ -run='^$'

import (
	"testing"
)

// ─── BenchmarkConvertValue ─────────────────────────────────────────────────────
// Benchmarks convertValue with different input types to show the cost breakdown
// and verify the optimization for non-path JSON object strings.

// BenchmarkConvertValuePlainString measures the baseline cost of a plain string
// (the most common case for objectid, name, distinguishedname, etc.).
// Should be nearly free: a single type switch, no allocations.
func BenchmarkConvertValuePlainString(b *testing.B) {
	b.ReportAllocs()
	v := "S-1-5-21-3623811015-3361044348-30300820-1013"
	b.ResetTimer()
	for range b.N {
		_ = convertValue(v)
	}
}

// BenchmarkConvertValueJSONArrayString measures the cost of a JSON-encoded
// string array (e.g., what kglite returns for labels() or __kinds).
// Requires one json.Unmarshal call.
func BenchmarkConvertValueJSONArrayString(b *testing.B) {
	b.ReportAllocs()
	v := `["Base","User","Entity"]`
	b.ResetTimer()
	for range b.N {
		_ = convertValue(v)
	}
}

// BenchmarkConvertValuePathString measures the cost of decoding a JSON-encoded
// path string.  After OPT-24 this path still requires json.Unmarshal (the
// prefix check passes), but non-path objects no longer pay this cost.
func BenchmarkConvertValuePathString(b *testing.B) {
	b.ReportAllocs()
	v := `{"__path":true,"nodes":[{"__node_idx":1,"__labels":["User"]},{"__node_idx":2,"__labels":["Computer"]}],"edges":[{"__edge_idx":10,"__src_idx":1,"__dst_idx":2,"__type":"HasSession"}]}`
	b.ResetTimer()
	for range b.N {
		_ = convertValue(v)
	}
}

// BenchmarkConvertValueNonPathJSONObject measures the cost of a JSON object
// string that is NOT a path.  Before OPT-24 this triggered a full
// json.Unmarshal; after OPT-24 the prefix check rejects it immediately at zero
// allocation cost.
func BenchmarkConvertValueNonPathJSONObject(b *testing.B) {
	b.ReportAllocs()
	// A JSON object string that starts with { but is not a path
	v := `{"name":"CORP.LOCAL","type":"domain","enabled":true}`
	b.ResetTimer()
	for range b.N {
		_ = convertValue(v)
	}
}

// BenchmarkConvertValueNode measures the cost of converting a node
// map[string]interface{} (already decoded by kglite's top-level JSON parse).
// This is the most common object type returned by node queries.
func BenchmarkConvertValueNode(b *testing.B) {
	b.ReportAllocs()
	v := map[string]interface{}{
		"__node_idx": float64(42),
		"__labels":   []interface{}{"User"},
		"__kinds":    `["Base","User","Entity"]`,
		"objectid":   "S-1-5-21-3623811015-3361044348-30300820-1013",
		"name":       "alice@corp.local",
		"enabled":    true,
		"lastlogon":  float64(1700000000),
	}
	b.ResetTimer()
	for range b.N {
		_ = convertValue(v)
	}
}

// BenchmarkConvertValueRelationship measures the cost of converting a
// relationship map[string]interface{}.
func BenchmarkConvertValueRelationship(b *testing.B) {
	b.ReportAllocs()
	v := map[string]interface{}{
		"__edge_idx": float64(100),
		"__src_idx":  float64(1),
		"__dst_idx":  float64(2),
		"__type":     "HasSession",
		"isacl":      false,
	}
	b.ResetTimer()
	for range b.N {
		_ = convertValue(v)
	}
}

// BenchmarkConvertValueMixed simulates a realistic result row with multiple
// mixed-type columns (common in queries that return node, relationship, and
// scalar columns together).
func BenchmarkConvertValueMixed(b *testing.B) {
	b.ReportAllocs()
	// Simulate a row with: string objectid, node map, relationship map, int count
	inputs := []interface{}{
		"S-1-5-21-3623811015-3361044348-30300820-1013",
		map[string]interface{}{
			"__node_idx": float64(42),
			"__labels":   []interface{}{"User"},
			"__kinds":    `["Base","User","Entity"]`,
			"objectid":   "S-1-5-21-3623811015-3361044348-30300820-1013",
			"enabled":    true,
		},
		map[string]interface{}{
			"__edge_idx": float64(100),
			"__src_idx":  float64(1),
			"__dst_idx":  float64(2),
			"__type":     "HasSession",
		},
		float64(7),
	}
	b.ResetTimer()
	for range b.N {
		for _, v := range inputs {
			_ = convertValue(v)
		}
	}
}

// BenchmarkConvertValueBoolAndNumeric benchmarks the fast default-case path
// for boolean and numeric values (no string checks, no allocations).
func BenchmarkConvertValueBoolAndNumeric(b *testing.B) {
	b.ReportAllocs()
	b.Run("bool", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_ = convertValue(true)
		}
	})
	b.Run("float64", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_ = convertValue(float64(3.14))
		}
	})
}
