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

// ─── OPT-2: Bulk edge FFI for UpdateRelationshipBy ────────────────────────────
//
// These benchmarks measure the improvement from OPT-2, which adds an
// objectid-to-nodeindex cache to the Batch struct. When both endpoint nodes have
// been seen by UpdateNodeBy (and a flush has occurred so indices are resolved),
// UpdateRelationshipBy uses the bulk edge FFI (CreateEdgesBatch) instead of a
// triple-MERGE Cypher query.
//
// Baseline (Slow):  UpdateRelationshipBy with no prior node cache — every
//                   relationship requires a triple-MERGE Cypher query.
//
// Optimized (Fast): UpdateNodeBy populates pendingLookups; a Commit() between
//                   the node and relationship passes flushes the Cypher batch and
//                   resolves node indices into oidToIdx; subsequent
//                   UpdateRelationshipBy calls bypass Cypher entirely for the
//                   relationship MERGE.
//
// A typical AD-object ingest does UpdateNodeBy for all nodes first, then a
// second BatchOperation for relationships — exactly the pattern the fast path
// targets.

import (
	"context"
	"fmt"
	"testing"

	"github.com/specterops/dawgs/graph"
)

// BenchmarkOPT2RelIngestion_Baseline_1000 / _5000
// Relationship ingestion with no prior node cache: every relationship uses the
// triple-MERGE Cypher slow path.  Nodes do NOT exist before the batch starts so
// the MERGE must create both nodes and the relationship in one Cypher statement.
// This represents the worst-case status quo.

func BenchmarkOPT2RelIngestion_Baseline_1000(b *testing.B) {
	benchmarkOPT2RelIngestionBaseline(b, 1000)
}

func BenchmarkOPT2RelIngestion_Baseline_5000(b *testing.B) {
	benchmarkOPT2RelIngestionBaseline(b, 5000)
}

func benchmarkOPT2RelIngestionBaseline(b *testing.B, n int) {
	b.ReportAllocs()

	relKind := graph.StringKind("BenchOPT2Rel")
	nodeKind := graph.StringKind("BenchOPT2Node")

	srcNodes := make([]*graph.Node, n)
	dstNodes := make([]*graph.Node, n)
	for i := 0; i < n; i++ {
		srcNodes[i] = &graph.Node{
			Kinds:      graph.Kinds{nodeKind},
			Properties: graph.AsProperties(map[string]any{"objectid": fmt.Sprintf("opt2-src-%d", i)}),
		}
		dstNodes[i] = &graph.Node{
			Kinds:      graph.Kinds{nodeKind},
			Properties: graph.AsProperties(map[string]any{"objectid": fmt.Sprintf("opt2-dst-%d", i)}),
		}
	}

	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		db := openBenchDriver(b)
		b.StartTimer()

		// Baseline: single BatchOperation with no prior node cache.
		// Every UpdateRelationshipBy call takes the Cypher MERGE slow path.
		err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
			for i := 0; i < n; i++ {
				rel := graph.NewRelationship(0, 0, 0, graph.NewProperties(), relKind)
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
			b.Fatalf("BatchOperation baseline: %v", err)
		}

		b.StopTimer()
		_ = db.Close(context.Background())
		b.StartTimer()
	}
}

// BenchmarkOPT2RelIngestion_FFIPath_1000 / _5000
// Relationship ingestion using the OPT-2 fast path.  Nodes are created via
// UpdateNodeBy in a first BatchOperation; the flush populates oidToIdx from the
// pendingLookups list.  A second BatchOperation then calls UpdateRelationshipBy:
// both endpoint indices are in the cache so the bulk edge FFI is used.
//
// Only the second BatchOperation (relationship creation) is timed.

func BenchmarkOPT2RelIngestion_FFIPath_1000(b *testing.B) {
	benchmarkOPT2RelIngestionFFIPath(b, 1000)
}

func BenchmarkOPT2RelIngestion_FFIPath_5000(b *testing.B) {
	benchmarkOPT2RelIngestionFFIPath(b, 5000)
}

func benchmarkOPT2RelIngestionFFIPath(b *testing.B, n int) {
	b.ReportAllocs()

	relKind := graph.StringKind("BenchOPT2Rel")
	nodeKind := graph.StringKind("BenchOPT2Node")

	srcNodes := make([]*graph.Node, n)
	dstNodes := make([]*graph.Node, n)
	for i := 0; i < n; i++ {
		srcNodes[i] = &graph.Node{
			Kinds:      graph.Kinds{nodeKind},
			Properties: graph.AsProperties(map[string]any{"objectid": fmt.Sprintf("opt2-src-%d", i)}),
		}
		dstNodes[i] = &graph.Node{
			Kinds:      graph.Kinds{nodeKind},
			Properties: graph.AsProperties(map[string]any{"objectid": fmt.Sprintf("opt2-dst-%d", i)}),
		}
	}

	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		db := openBenchDriver(b)

		// Setup: create nodes via UpdateNodeBy so their indices are resolved into
		// oidToIdx before the timed relationship-ingestion pass begins.
		// This mirrors the real-world ingest pattern where node files are processed
		// before relationship files.
		err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
			for i := 0; i < n; i++ {
				if err := batch.UpdateNodeBy(graph.NodeUpdate{
					Node:               srcNodes[i],
					IdentityKind:       nodeKind,
					IdentityProperties: []string{"objectid"},
				}); err != nil {
					return err
				}
				if err := batch.UpdateNodeBy(graph.NodeUpdate{
					Node:               dstNodes[i],
					IdentityKind:       nodeKind,
					IdentityProperties: []string{"objectid"},
				}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.Fatalf("setup BatchOperation: %v", err)
		}

		b.StartTimer()

		// Timed section: relationship ingestion via the FFI fast path.
		// Because oidToIdx is populated in a fresh BatchOperation, we need to
		// seed the cache inline by doing one more BatchOperation that calls
		// UpdateNodeBy (which appends to pendingLookups) followed by a
		// Commit() to trigger resolvePendingLookups before the relationship pass.
		err = db.BatchOperation(context.Background(), func(batch graph.Batch) error {
			// Re-upsert nodes: this is cheap (MERGE is idempotent) and populates
			// pendingLookups with all objectids.
			for i := 0; i < n; i++ {
				if err := batch.UpdateNodeBy(graph.NodeUpdate{
					Node:               srcNodes[i],
					IdentityKind:       nodeKind,
					IdentityProperties: []string{"objectid"},
				}); err != nil {
					return err
				}
				if err := batch.UpdateNodeBy(graph.NodeUpdate{
					Node:               dstNodes[i],
					IdentityKind:       nodeKind,
					IdentityProperties: []string{"objectid"},
				}); err != nil {
					return err
				}
			}
			// Flush: executes the MERGE queries and resolves oidToIdx.
			if err := batch.Commit(); err != nil {
				return err
			}

			// Relationship ingestion: both endpoints are now cached → FFI path.
			for i := 0; i < n; i++ {
				rel := graph.NewRelationship(0, 0, 0, graph.NewProperties(), relKind)
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
			b.Fatalf("BatchOperation FFI path: %v", err)
		}

		b.StopTimer()
		_ = db.Close(context.Background())
		b.StartTimer()
	}
}

// BenchmarkOPT2RelIngestion_TwoBatch_1000 / _5000
// Measures the two-BatchOperation pattern that is most common in real ingest:
//   1. First BatchOperation: UpdateNodeBy for all nodes.
//   2. Second BatchOperation: UpdateNodeBy (to seed cache) + Commit + UpdateRelationshipBy.
//
// The entire two-batch sequence is timed to show end-to-end throughput.
// Compare with BenchmarkRelationshipIngestion (opt1) which times only the
// relationship MERGE part without any node-cache benefit.

func BenchmarkOPT2TwoBatch_1000(b *testing.B) {
	benchmarkOPT2TwoBatch(b, 1000)
}

func BenchmarkOPT2TwoBatch_5000(b *testing.B) {
	benchmarkOPT2TwoBatch(b, 5000)
}

func benchmarkOPT2TwoBatch(b *testing.B, n int) {
	b.ReportAllocs()

	relKind := graph.StringKind("BenchOPT2TwoBatch")
	nodeKind := graph.StringKind("BenchOPT2TwoBatchNode")

	srcNodes := make([]*graph.Node, n)
	dstNodes := make([]*graph.Node, n)
	for i := 0; i < n; i++ {
		srcNodes[i] = &graph.Node{
			Kinds:      graph.Kinds{nodeKind},
			Properties: graph.AsProperties(map[string]any{"objectid": fmt.Sprintf("tb-src-%d", i)}),
		}
		dstNodes[i] = &graph.Node{
			Kinds:      graph.Kinds{nodeKind},
			Properties: graph.AsProperties(map[string]any{"objectid": fmt.Sprintf("tb-dst-%d", i)}),
		}
	}

	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		db := openBenchDriver(b)
		b.StartTimer()

		// BatchOperation 1: node ingestion (UpdateNodeBy for all nodes).
		err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
			for i := 0; i < n; i++ {
				if err := batch.UpdateNodeBy(graph.NodeUpdate{
					Node:               srcNodes[i],
					IdentityKind:       nodeKind,
					IdentityProperties: []string{"objectid"},
				}); err != nil {
					return err
				}
				if err := batch.UpdateNodeBy(graph.NodeUpdate{
					Node:               dstNodes[i],
					IdentityKind:       nodeKind,
					IdentityProperties: []string{"objectid"},
				}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.Fatalf("node BatchOperation: %v", err)
		}

		// BatchOperation 2: relationship ingestion using FFI fast path.
		err = db.BatchOperation(context.Background(), func(batch graph.Batch) error {
			// Re-upsert nodes to populate pendingLookups, then flush to resolve oidToIdx.
			for i := 0; i < n; i++ {
				if err := batch.UpdateNodeBy(graph.NodeUpdate{
					Node:               srcNodes[i],
					IdentityKind:       nodeKind,
					IdentityProperties: []string{"objectid"},
				}); err != nil {
					return err
				}
				if err := batch.UpdateNodeBy(graph.NodeUpdate{
					Node:               dstNodes[i],
					IdentityKind:       nodeKind,
					IdentityProperties: []string{"objectid"},
				}); err != nil {
					return err
				}
			}
			if err := batch.Commit(); err != nil {
				return err
			}
			for i := 0; i < n; i++ {
				rel := graph.NewRelationship(0, 0, 0, graph.NewProperties(), relKind)
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
			b.Fatalf("relationship BatchOperation: %v", err)
		}

		b.StopTimer()
		_ = db.Close(context.Background())
		b.StartTimer()
	}
}
