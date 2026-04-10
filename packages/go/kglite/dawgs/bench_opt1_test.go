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

//go:build standalone

package dawgs

import (
	"context"
	"fmt"
	"testing"

	"github.com/specterops/bloodhound/packages/go/kglite"
	"github.com/specterops/dawgs/graph"
)

// openBenchDriver opens an in-memory kglite driver for benchmarks.
// Unlike openTestDriver it does not register a t.Cleanup so the caller
// can control lifetime explicitly.
func openBenchDriver(b *testing.B) *Driver {
	b.Helper()
	db, err := Open("")
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { _ = db.Close(context.Background()) })
	return db
}

// ─── BenchmarkNodeIngestion ───────────────────────────────────────────────────
// Measures the cost of MERGEing N nodes with properties via BatchOperation /
// batch.UpdateNodeBy.  This exercises the full hot path:
//   - Go-side: Cypher string construction, map allocations, batch enqueue
//   - CGO boundary: goStringToC (optimized) vs C.CString (baseline)
//   - Rust side: MERGE execution inside a single Mutex lock per flush
//   - C optimisation B: CypherBatchExec skips JSON unmarshal of results

func BenchmarkNodeIngestion1000(b *testing.B) {
	benchmarkNodeIngestion(b, 1000)
}

func BenchmarkNodeIngestion10000(b *testing.B) {
	benchmarkNodeIngestion(b, 10000)
}

func benchmarkNodeIngestion(b *testing.B, n int) {
	b.ReportAllocs()

	// Pre-build the node list so setup work is not measured.
	kind := graph.StringKind("BenchNode")
	nodes := make([]*graph.Node, n)
	for i := 0; i < n; i++ {
		nodes[i] = graph.NewNode(0,
			graph.AsProperties(map[string]any{
				"objectid": fmt.Sprintf("node-%d", i),
				"name":     fmt.Sprintf("Node %d", i),
				"enabled":  true,
				"idx":      int64(i),
			}),
			kind,
		)
	}

	b.ResetTimer()
	for range b.N {
		db := openBenchDriver(b)
		err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
			for _, node := range nodes {
				if err := batch.UpdateNodeBy(graph.NodeUpdate{
					Node:               node,
					IdentityKind:       kind,
					IdentityProperties: []string{"objectid"},
				}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.Fatalf("BatchOperation: %v", err)
		}
		_ = db.Close(context.Background())
	}
}

// ─── BenchmarkRelationshipIngestion ──────────────────────────────────────────
// Measures the cost of MERGEing N relationships via batch.UpdateRelationshipBy.
// Nodes are created first (outside the timer) so only relationship MERGE is
// measured.

func BenchmarkRelationshipIngestion1000(b *testing.B) {
	benchmarkRelationshipIngestion(b, 1000)
}

func BenchmarkRelationshipIngestion5000(b *testing.B) {
	benchmarkRelationshipIngestion(b, 5000)
}

func benchmarkRelationshipIngestion(b *testing.B, n int) {
	b.ReportAllocs()

	kind := graph.StringKind("BenchRel")
	nodeKind := graph.StringKind("BenchRelNode")

	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		db := openBenchDriver(b)

		// Create src and dst nodes — not part of measured work.
		srcNodes := make([]*graph.Node, n)
		dstNodes := make([]*graph.Node, n)
		for i := 0; i < n; i++ {
			srcNodes[i] = &graph.Node{
				Kinds:      graph.Kinds{nodeKind},
				Properties: graph.AsProperties(map[string]any{"objectid": fmt.Sprintf("src-%d", i)}),
			}
			dstNodes[i] = &graph.Node{
				Kinds:      graph.Kinds{nodeKind},
				Properties: graph.AsProperties(map[string]any{"objectid": fmt.Sprintf("dst-%d", i)}),
			}
		}
		b.StartTimer()

		err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
			for i := 0; i < n; i++ {
				rel := graph.NewRelationship(0, 0, 0, graph.NewProperties(), kind)
				if err := batch.UpdateRelationshipBy(graph.RelationshipUpdate{
					Relationship:            rel,
					Start:                   srcNodes[i],
					StartIdentityKind:       nodeKind,
					StartIdentityProperties: []string{"objectid"},
					End:                     dstNodes[i],
					EndIdentityKind:         nodeKind,
					EndIdentityProperties:   []string{"objectid"},
				}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.Fatalf("BatchOperation: %v", err)
		}
		b.StopTimer()
		_ = db.Close(context.Background())
		b.StartTimer()
	}
}

// ─── BenchmarkCypherBatchExec ─────────────────────────────────────────────────
// Directly benchmarks kg.CypherBatchExec vs kg.CypherBatch with a batch of 100
// simple MERGE queries, isolating the unmarshal savings from optimisation B.
//
// CypherBatchExec (added in the optimised build) discards result rows without
// full JSON unmarshal; CypherBatch parses every result into []*CypherResult.
// BenchmarkCypherBatchExec uses CypherBatchExec when it exists (optimised),
// and falls back to CypherBatch (baseline) so the same benchmark name appears
// in both result files and benchstat can compare them.
//
// BenchmarkCypherBatchFull always calls CypherBatch so the full unmarshal cost
// can be compared in isolation.

func BenchmarkCypherBatchExec(b *testing.B) {
	b.ReportAllocs()

	kg, err := kglite.New()
	if err != nil {
		b.Fatalf("kglite.New: %v", err)
	}
	b.Cleanup(func() { kg.Free() })

	const batchSize = 100
	queries := make([]kglite.BatchQuery, batchSize)
	for i := range queries {
		queries[i] = kglite.BatchQuery{
			Query:  fmt.Sprintf("MERGE (n:`BenchCypher` {objectid: 'obj-%d'})", i),
			Params: nil,
		}
	}

	b.ResetTimer()
	for range b.N {
		if err := kg.CypherBatchExec(queries); err != nil {
			b.Fatalf("CypherBatchExec: %v", err)
		}
	}
}

func BenchmarkCypherBatchFull(b *testing.B) {
	b.ReportAllocs()

	kg, err := kglite.New()
	if err != nil {
		b.Fatalf("kglite.New: %v", err)
	}
	b.Cleanup(func() { kg.Free() })

	const batchSize = 100
	queries := make([]kglite.BatchQuery, batchSize)
	for i := range queries {
		queries[i] = kglite.BatchQuery{
			Query:  fmt.Sprintf("MERGE (n:`BenchCypher` {objectid: 'obj-%d'})", i),
			Params: nil,
		}
	}

	b.ResetTimer()
	for range b.N {
		if _, err := kg.CypherBatch(queries); err != nil {
			b.Fatalf("CypherBatch: %v", err)
		}
	}
}

// ─── BenchmarkCypherReadQuery ─────────────────────────────────────────────────
// Benchmarks a simple read query (MATCH (n) RETURN count(n)) to measure the
// improvement from optimisation A (goStringToC vs C.CString) on read paths.
// The query string is allocated once per iteration through the CGO boundary.

func BenchmarkCypherReadQuery(b *testing.B) {
	b.ReportAllocs()

	kg, err := kglite.New()
	if err != nil {
		b.Fatalf("kglite.New: %v", err)
	}
	b.Cleanup(func() { kg.Free() })

	// Pre-populate a handful of nodes so the query is non-trivial.
	for i := 0; i < 100; i++ {
		if _, err := kg.Cypher(
			fmt.Sprintf("MERGE (n:`BenchRead` {objectid: 'r-%d'})", i),
			nil,
		); err != nil {
			b.Fatalf("setup Cypher: %v", err)
		}
	}

	const query = "MATCH (n) RETURN count(n)"

	b.ResetTimer()
	for range b.N {
		result, err := kg.Cypher(query, nil)
		if err != nil {
			b.Fatalf("Cypher: %v", err)
		}
		_ = result
	}
}
