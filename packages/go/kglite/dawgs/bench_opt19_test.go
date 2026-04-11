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

// OPT-19: Eliminate double convertValue on query results.
//
// Before OPT-19, Raw() eagerly called convertValue on every cell in every row
// immediately after kglite returned the CypherResult.  Then, when the scanning
// infrastructure called kgliteResult.Values(), it called convertValue again on
// the already-converted values.  The cache in Values() only avoids double
// conversion within a single call to Values() — it does not know that Raw()
// had already mutated the rows in-place.
//
// After OPT-19 the eager pass in Raw() is removed.  Values() is the sole site
// of conversion and its existing convertedRow/convertedRowIdx cache prevents
// double conversion on repeated Values() calls for the same row.
//
// For large result sets this eliminates O(rows × cols) redundant convertValue
// invocations — including JSON unmarshalling for string values, map inspection
// for nodes/edges, and float-to-int conversion.
//
// Run with:
//
//	go test -tags standalone -run=^$ -bench=BenchmarkOPT19 -benchmem -benchtime=3s \
//	    ./packages/go/kglite/dawgs/

//go:build standalone

package dawgs

import (
	"context"
	"fmt"
	"testing"

	"github.com/specterops/dawgs/graph"
)

// ─── BenchmarkOPT19_QueryAndScan ─────────────────────────────────────────────
//
// Measures the full cost of executing a Cypher query that returns many rows and
// scanning each row through the graph.Result interface.  This is the primary
// hot path affected by OPT-19: every MATCH query in post-processing goes
// through Raw() → kgliteResult.Values() → Scan().
//
// With OPT-19, convertValue is called only once per cell (in Values()); before
// OPT-19 it was called twice (once eagerly in Raw(), once lazily in Values()).

func BenchmarkOPT19_QueryAndScan_100(b *testing.B) {
	benchmarkOPT19QueryAndScan(b, 100)
}

func BenchmarkOPT19_QueryAndScan_1000(b *testing.B) {
	benchmarkOPT19QueryAndScan(b, 1000)
}

func BenchmarkOPT19_QueryAndScan_5000(b *testing.B) {
	benchmarkOPT19QueryAndScan(b, 5000)
}

func benchmarkOPT19QueryAndScan(b *testing.B, n int) {
	b.Helper()
	b.ReportAllocs()

	// Set up a database with n nodes once (outside the timer).
	db := openBenchDriver(b)
	kind := graph.StringKind("OPT19Node")

	if err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
		for i := 0; i < n; i++ {
			if err := batch.UpdateNodeBy(graph.NodeUpdate{
				Node: graph.NewNode(0,
					graph.AsProperties(map[string]any{
						"objectid": fmt.Sprintf("opt19-node-%d", i),
						"name":     fmt.Sprintf("OPT19 Node %d", i),
						"enabled":  true,
					}),
					kind,
				),
				IdentityKind:       kind,
				IdentityProperties: []string{"objectid"},
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		b.Fatalf("setup BatchOperation: %v", err)
	}

	b.ResetTimer()
	for range b.N {
		// Execute a query that returns all nodes. The returned result contains
		// n rows each with one node cell — these are the values that Raw()
		// previously converted eagerly and Values() converted again.
		err := db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
			result := tx.Raw(
				fmt.Sprintf("MATCH (n:`%s`) RETURN n", kind.String()),
				nil,
			)
			defer result.Close()

			for result.Next() {
				var node graph.Node
				if err := result.Scan(&node); err != nil {
					return err
				}
				// Touch the ID so the compiler can't elide the scan.
				_ = node.ID
			}
			return result.Error()
		})
		if err != nil {
			b.Fatalf("ReadTransaction: %v", err)
		}
	}
}

// ─── BenchmarkOPT19_ConvertValueOnly ─────────────────────────────────────────
//
// Micro-benchmark of convertValue in isolation on the three most common value
// types produced by kglite: plain string, float64 (integer-valued), and a node
// map.  Shows the per-call cost that OPT-19 eliminates from the eager Raw() pass.

func BenchmarkOPT19_ConvertValueString(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = convertValue("S-1-5-21-111-222-333-500")
	}
}

func BenchmarkOPT19_ConvertValueFloat(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = convertValue(float64(12345))
	}
}

func BenchmarkOPT19_ConvertValueNodeMap(b *testing.B) {
	nodeMap := map[string]interface{}{
		"__node_idx": float64(42),
		"__labels":   []interface{}{"User", "Base"},
		"objectid":   "S-1-5-21-111-222-333-500",
		"name":       "ADMINISTRATOR@CONTOSO.LOCAL",
		"enabled":    true,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = convertValue(nodeMap)
	}
}

// ─── BenchmarkOPT19_ValuesRowScan ────────────────────────────────────────────
//
// Measures Values() + Scan() overhead on a pre-built kgliteResult with rows
// of various types, isolating the result-scanning cost from kglite CGO.
// The kgliteResult is built with raw (unconverted) values as kglite would
// return them after OPT-19.

func BenchmarkOPT19_ValuesRowScan_10cols(b *testing.B) {
	benchmarkOPT19ValuesScan(b, 1000, 10)
}

func BenchmarkOPT19_ValuesRowScan_3cols(b *testing.B) {
	benchmarkOPT19ValuesScan(b, 1000, 3)
}

func benchmarkOPT19ValuesScan(b *testing.B, nRows, nCols int) {
	b.Helper()
	b.ReportAllocs()

	// Build raw rows that mimic what kglite returns: float64 for numeric IDs.
	rows := make([][]interface{}, nRows)
	for i := range rows {
		row := make([]interface{}, nCols)
		for j := range row {
			// Alternate between float64 (common for IDs) and string values.
			if j%2 == 0 {
				row[j] = float64(i*nCols + j)
			} else {
				row[j] = fmt.Sprintf("value-%d-%d", i, j)
			}
		}
		rows[i] = row
	}

	cols := make([]string, nCols)
	for i := range cols {
		cols[i] = fmt.Sprintf("col%d", i)
	}

	b.ResetTimer()
	for range b.N {
		r := newMockResult(cols, rows)
		for r.Next() {
			_ = r.Values()
		}
	}
}
