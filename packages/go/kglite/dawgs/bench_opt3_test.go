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

	"github.com/specterops/dawgs/graph"
)

// ─── OPT-3: Flush-size amortisation benchmarks ──────────────────────────────
//
// These benchmarks measure the throughput improvement from:
//   (a) Larger edge flush size (20 000 vs 5 000) — fewer CGO round-trips per
//       batch of CreateRelationshipByIDs calls during post-processing.
//   (b) Batched IN-clause deletes vs individual per-edge Cypher queries —
//       DeleteRelationship now accumulates IDs and flushes them in a single
//       MATCH ()-[r]->() WHERE id(r) IN [...] DELETE r call.
//
// Run with:
//   go test -tags standalone -run=^$ -bench=BenchmarkOPT3 -benchtime=3s \
//       ./packages/go/kglite/dawgs/

// createBenchNode is a benchmark-scoped helper equivalent to createTestNode.
func createBenchNode(b *testing.B, db *Driver, kind graph.Kind, props map[string]any) *graph.Node {
	b.Helper()
	p := graph.AsProperties(props)
	var node *graph.Node
	err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		var err error
		node, err = tx.CreateNode(p, kind)
		return err
	})
	if err != nil {
		b.Fatalf("createBenchNode: %v", err)
	}
	return node
}

// ─── Edge creation throughput ─────────────────────────────────────────────

// BenchmarkOPT3EdgeCreation_5000 simulates the old flush threshold by creating
// exactly 5 000 edges per iteration, matching the previous defaultEdgeFlushSize.
// Use this as the "before" baseline when comparing with benchstat.
func BenchmarkOPT3EdgeCreation_5000(b *testing.B) {
	benchmarkOPT3EdgeCreation(b, 5000)
}

// BenchmarkOPT3EdgeCreation_20000 uses the new flush threshold (20 000 edges).
// Larger batches amortise CGO overhead across more edges per call.
func BenchmarkOPT3EdgeCreation_20000(b *testing.B) {
	benchmarkOPT3EdgeCreation(b, 20000)
}

func benchmarkOPT3EdgeCreation(b *testing.B, n int) {
	b.Helper()
	b.ReportAllocs()

	nodeKind := graph.StringKind("OPT3Node")
	edgeKind := graph.StringKind("OPT3Edge")

	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		db := openBenchDriver(b)

		// Pre-create n source and destination nodes outside the measured section.
		srcIDs := make([]graph.ID, n)
		dstIDs := make([]graph.ID, n)
		for i := 0; i < n; i++ {
			src := createBenchNode(b, db, nodeKind, map[string]any{"objectid": fmt.Sprintf("opt3-src-%d", i)})
			dst := createBenchNode(b, db, nodeKind, map[string]any{"objectid": fmt.Sprintf("opt3-dst-%d", i)})
			srcIDs[i] = src.ID
			dstIDs[i] = dst.ID
		}
		b.StartTimer()

		err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
			for i := 0; i < n; i++ {
				if err := batch.CreateRelationshipByIDs(srcIDs[i], dstIDs[i], edgeKind, nil); err != nil {
					return err
				}
			}
			return nil
		})
		b.StopTimer()
		if err != nil {
			b.Fatalf("BatchOperation: %v", err)
		}
		_ = db.Close(context.Background())
		b.StartTimer()
	}
}

// ─── Edge deletion throughput ─────────────────────────────────────────────

// BenchmarkOPT3DeleteRelationship_500 benchmarks deleting 500 edges per
// iteration — matching the defaultDeleteFlushSize so exactly one batched
// IN-clause query is issued.
func BenchmarkOPT3DeleteRelationship_500(b *testing.B) {
	benchmarkOPT3DeleteRelationship(b, 500)
}

// BenchmarkOPT3DeleteRelationship_5000 benchmarks deleting 5 000 edges per
// iteration — ten flush cycles of 500 IDs each — exercising the auto-flush
// path in maybeFlush.
func BenchmarkOPT3DeleteRelationship_5000(b *testing.B) {
	benchmarkOPT3DeleteRelationship(b, 5000)
}

func benchmarkOPT3DeleteRelationship(b *testing.B, n int) {
	b.Helper()
	b.ReportAllocs()

	nodeKind := graph.StringKind("OPT3DelNode")
	edgeKind := graph.StringKind("OPT3DelEdge")

	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		db := openBenchDriver(b)

		// Pre-create nodes and edges outside the measured section; collect edge IDs.
		relIDs := make([]graph.ID, n)
		for i := 0; i < n; i++ {
			src := createBenchNode(b, db, nodeKind, map[string]any{"objectid": fmt.Sprintf("del-src-%d", i)})
			dst := createBenchNode(b, db, nodeKind, map[string]any{"objectid": fmt.Sprintf("del-dst-%d", i)})
			err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
				rel, err := tx.CreateRelationshipByIDs(src.ID, dst.ID, edgeKind, nil)
				if err != nil {
					return err
				}
				relIDs[i] = rel.ID
				return nil
			})
			if err != nil {
				b.Fatalf("setup CreateRelationshipByIDs: %v", err)
			}
		}
		b.StartTimer()

		err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
			for _, id := range relIDs {
				if err := batch.DeleteRelationship(id); err != nil {
					return err
				}
			}
			return nil
		})
		b.StopTimer()
		if err != nil {
			b.Fatalf("BatchOperation DeleteRelationship: %v", err)
		}
		_ = db.Close(context.Background())
		b.StartTimer()
	}
}
