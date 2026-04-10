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

// ─── OPT-14: Replace reflect.ValueOf with type switch in rewriteInParam ──────
//
// Background:
//   Every Cypher query with an IN $param pattern calls rewriteInParam, which
//   previously used reflect.ValueOf(val) to determine whether the parameter
//   is a slice or array.  reflect.ValueOf allocates (it boxes the interface
//   value into a reflect.Value) and this call sits on the hot path for all
//   read queries — during analysis the same patterns execute thousands of times.
//
//   OPT-14 introduces inParamToElems, a type-switched helper that handles the
//   concrete slice types actually used in the codebase ([]string, []graph.ID,
//   []int64, []uint64, []int, []int32, []any) without reflection.  Unknown
//   slice types fall back to reflect inside the default arm, so correctness
//   is preserved for any type.
//
// Benchmarks in this file:
//   BenchmarkInParamToElems_TypeSwitch  — new implementation (OPT-14)
//   BenchmarkInParamToElems_Reflect     — original reflect-based baseline
//   BenchmarkRewriteInParam_TypeSwitch  — full rewriteInParam with new helper
//   BenchmarkRewriteInParam_Reflect     — full rewriteInParam with old helper
//
// Each benchmark measures the common case ([]string and []graph.ID) to show
// the allocation and latency reduction on the actual hot path.

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/dawgs/graph"
)

// ─── reference: original reflect-based implementation ────────────────────────

// inParamToElemsReflect is the pre-OPT-14 implementation kept here verbatim
// for benchmark comparison.  It is NOT used in production code.
func inParamToElemsReflect(val any) ([]string, bool) {
	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	elems := make([]string, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		v := rv.Index(i).Interface()
		switch v.(type) {
		case string:
			elems[i] = fmt.Sprintf("'%s'", v)
		default:
			elems[i] = fmt.Sprintf("%v", v)
		}
	}
	return elems, true
}

// rewriteInParamReflect is the pre-OPT-14 rewriteInParam kept for benchmarking.
func rewriteInParamReflect(cypher string, params map[string]any) string {
	return reInParam.ReplaceAllStringFunc(cypher, func(m string) string {
		sub := reInParam.FindStringSubmatch(m)
		paramName := sub[1]
		val, ok := params[paramName]
		if !ok {
			return m
		}
		elems, ok := inParamToElemsReflect(val)
		if !ok {
			return m
		}
		delete(params, paramName)
		return "IN [" + strings.Join(elems, ", ") + "]"
	})
}

// ─── benchmark inputs ─────────────────────────────────────────────────────────

var (
	benchStrings  = []string{"S-1-5-21-1", "S-1-5-21-2", "S-1-5-21-3", "S-1-5-21-4", "S-1-5-21-5"}
	benchGraphIDs = []graph.ID{1, 2, 3, 4, 5, 6, 7, 8}
	benchInt64s   = []int64{100, 200, 300, 400, 500}
)

// ─── inParamToElems benchmarks ────────────────────────────────────────────────

// BenchmarkInParamToElems_TypeSwitch measures inParamToElems (OPT-14) on the
// most common IN-parameter types encountered during BloodHound analysis.
func BenchmarkInParamToElems_TypeSwitch(b *testing.B) {
	b.Run("[]string", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = inParamToElems(benchStrings)
		}
	})

	b.Run("[]graph.ID", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = inParamToElems(benchGraphIDs)
		}
	})

	b.Run("[]int64", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = inParamToElems(benchInt64s)
		}
	})
}

// BenchmarkInParamToElems_Reflect measures the pre-OPT-14 reflect-based
// implementation on the same inputs as BenchmarkInParamToElems_TypeSwitch.
func BenchmarkInParamToElems_Reflect(b *testing.B) {
	b.Run("[]string", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = inParamToElemsReflect(benchStrings)
		}
	})

	b.Run("[]graph.ID", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = inParamToElemsReflect(benchGraphIDs)
		}
	})

	b.Run("[]int64", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = inParamToElemsReflect(benchInt64s)
		}
	})
}

// ─── full rewriteInParam benchmarks ──────────────────────────────────────────

// BenchmarkRewriteInParam_TypeSwitch measures the full rewriteInParam path
// (regex match + inParamToElems type switch) with the structural cache warmed.
func BenchmarkRewriteInParam_TypeSwitch(b *testing.B) {
	const cypher = `MATCH (n) WHERE n.objectid IN $ids RETURN n`

	b.Run("[]string/5_elements", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			params := map[string]any{"ids": benchStrings}
			_ = rewriteInParam(cypher, params)
		}
	})

	b.Run("[]graph.ID/8_elements", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			params := map[string]any{"ids": benchGraphIDs}
			_ = rewriteInParam(cypher, params)
		}
	})
}

// BenchmarkRewriteInParam_Reflect measures the pre-OPT-14 rewriteInParam path
// for comparison with BenchmarkRewriteInParam_TypeSwitch.
func BenchmarkRewriteInParam_Reflect(b *testing.B) {
	const cypher = `MATCH (n) WHERE n.objectid IN $ids RETURN n`

	b.Run("[]string/5_elements", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			params := map[string]any{"ids": benchStrings}
			_ = rewriteInParamReflect(cypher, params)
		}
	})

	b.Run("[]graph.ID/8_elements", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			params := map[string]any{"ids": benchGraphIDs}
			_ = rewriteInParamReflect(cypher, params)
		}
	})
}
