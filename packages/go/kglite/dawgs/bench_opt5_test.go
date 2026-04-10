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

// OPT-5 benchmark: demonstrate that removing the Go-side cross-flush
// edgesSeen map eliminates its allocation overhead.
//
// Before OPT-5 each flush that called deduplicateEdges would:
//   - allocate a localSeen map (O(flush-size) entries)
//   - append to the growing edgesSeen map (O(total-edges-ever-flushed))
//
// After OPT-5 the Go side allocates nothing extra per flush — duplicate
// suppression is delegated to the Rust CreateEdgesBatch (skipExisting=false).
//
// Run with:
//   go test -tags standalone -bench=BenchmarkEdgeBatch -benchmem -benchtime=3s \
//     ./packages/go/kglite/dawgs/ -run='^$'

package dawgs

import (
	"context"
	"fmt"
	"testing"

	"github.com/specterops/dawgs/graph"
)

// BenchmarkEdgeBatch50000 measures the cost of adding 50 000 unique edges
// via batch.CreateRelationshipByIDs.  Nodes are pre-created outside the
// timer so only the relationship-ingest path is measured.
// b.ReportAllocs() captures bytes/op and allocs/op for comparison.
func BenchmarkEdgeBatch50000(b *testing.B) {
	benchmarkEdgeBatch(b, 50000)
}

// BenchmarkEdgeBatch10000 is a smaller variant for faster CI runs.
func BenchmarkEdgeBatch10000(b *testing.B) {
	benchmarkEdgeBatch(b, 10000)
}

func benchmarkEdgeBatch(b *testing.B, n int) {
	b.Helper()
	b.ReportAllocs()

	nodeKind := graph.StringKind("BenchEdgeOpt5Node")
	edgeKind := graph.StringKind("BenchEdgeOpt5Rel")

	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		db := openBenchDriver(b)

		// Pre-create n src nodes and n dst nodes outside the measured window.
		srcIDs := make([]graph.ID, n)
		dstIDs := make([]graph.ID, n)
		if err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
			for i := 0; i < n; i++ {
				src, err := tx.CreateNode(
					graph.AsProperties(map[string]any{"objectid": fmt.Sprintf("src-opt5-%d", i)}),
					nodeKind,
				)
				if err != nil {
					return err
				}
				srcIDs[i] = src.ID

				dst, err := tx.CreateNode(
					graph.AsProperties(map[string]any{"objectid": fmt.Sprintf("dst-opt5-%d", i)}),
					nodeKind,
				)
				if err != nil {
					return err
				}
				dstIDs[i] = dst.ID
			}
			return nil
		}); err != nil {
			b.Fatalf("setup WriteTransaction: %v", err)
		}

		b.StartTimer()

		// Measured: ingest n edges via the batch edge FFI path.
		err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
			for i := 0; i < n; i++ {
				if err := batch.CreateRelationshipByIDs(srcIDs[i], dstIDs[i], edgeKind, nil); err != nil {
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
