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

// ─── OPT-4: IngestCountThreshold batch-flush overhead ────────────────────────
//
// Background:
//   The graphify decoder accumulates converted JSON objects into a ConvertedData
//   buffer and calls IngestBasicData (which ultimately calls BatchOperation /
//   Batch.flush) once every IngestCountThreshold items.  Neo4j's threshold was
//   500 to limit per-transaction memory pressure.  kglite's in-memory model has
//   no such constraint, so larger batches reduce the number of CGO calls per
//   ingest run.
//
// This benchmark measures flush overhead directly at the kglite layer by
// driving N total node upserts through a single BatchOperation with different
// intermediate flush sizes.  The flush size is controlled by temporarily
// replacing Batch.flushSize; the total work (N upserts) is constant across all
// sub-benchmarks so that throughput (nodes/s) is directly comparable.
//
// Sub-benchmarks:
//   BenchmarkFlushOverhead/flushEvery500   — legacy Neo4j threshold
//   BenchmarkFlushOverhead/flushEvery5000  — new kglite threshold (OPT-4)
//   BenchmarkFlushOverhead/flushEvery10000 — headroom check

import (
	"context"
	"fmt"
	"testing"

	"github.com/specterops/dawgs/graph"
)

// totalNodes is the total number of node upserts per benchmark iteration.
// Chosen to be large enough to trigger multiple intermediate flushes even at
// the largest flush size, and small enough to complete quickly.
const totalNodes = 10_000

// benchmarkFlushOverhead runs a single BatchOperation that upserts totalNodes
// nodes, but instructs the Batch to flush every flushEvery items internally.
// The flushSize field is set directly since it is package-private and this test
// lives in the same package.
func benchmarkFlushOverhead(b *testing.B, flushEvery int) {
	b.Helper()
	b.ReportAllocs()

	kind := graph.StringKind("OPT4Node")

	// Pre-build all NodeUpdate structs outside the timer.
	updates := make([]graph.NodeUpdate, totalNodes)
	for i := range updates {
		node := graph.NewNode(0,
			graph.AsProperties(map[string]any{
				"objectid": fmt.Sprintf("opt4-node-%d", i),
				"name":     fmt.Sprintf("OPT4 Node %d", i),
				"idx":      int64(i),
			}),
			kind,
		)
		updates[i] = graph.NodeUpdate{
			Node:               node,
			IdentityKind:       kind,
			IdentityProperties: []string{"objectid"},
		}
	}

	b.ResetTimer()
	for range b.N {
		db := openBenchDriver(b)

		err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
			// Cast to *Batch to set the internal flushSize.  This is only possible
			// because the benchmark lives in the same package as the implementation.
			kbatch := batch.(*Batch)
			kbatch.flushSize = flushEvery

			for i := range updates {
				if err := batch.UpdateNodeBy(updates[i]); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.Fatalf("BatchOperation (flushEvery=%d): %v", flushEvery, err)
		}

		_ = db.Close(context.Background())
	}

	b.SetBytes(int64(totalNodes)) // report bytes = nodes so ns/node is visible
}

func BenchmarkFlushOverhead(b *testing.B) {
	b.Run("flushEvery500", func(b *testing.B) {
		benchmarkFlushOverhead(b, 500)
	})
	b.Run("flushEvery5000", func(b *testing.B) {
		benchmarkFlushOverhead(b, 5000)
	})
	b.Run("flushEvery10000", func(b *testing.B) {
		benchmarkFlushOverhead(b, 10000)
	})
}
