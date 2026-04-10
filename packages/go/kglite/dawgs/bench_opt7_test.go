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

// OPT-7 benchmark: batched relationship deletion via IN-clause vs per-ID Cypher queries.
//
// Before OPT-7, DeleteRelationship issued one CypherBatchExec call per ID:
//
//	MATCH ()-[r]->() WHERE id(r) = $id DELETE r
//
// After OPT-7, IDs are accumulated in pendingDeletes and flushed as a single
// IN-clause query once the buffer reaches defaultDeleteFlushSize (500):
//
//	MATCH ()-[r]->() WHERE id(r) IN [id1, id2, ...] DELETE r
//
// This reduces CGO round-trips from N to ceil(N/500) for large bulk deletions
// such as DeleteTransitEdges during post-processing.
//
// Run with:
//
//	go test -tags standalone -bench=BenchmarkDeleteRelationship -benchmem -benchtime=3s \
//	  ./packages/go/kglite/dawgs/ -run='^$'

package dawgs

import (
	"context"
	"fmt"
	"testing"

	"github.com/specterops/bloodhound/packages/go/kglite"
	"github.com/specterops/dawgs/graph"
)

// benchFetchRelIDs fetches n relationship IDs from the graph via raw Cypher.
// This is required because transaction.CreateRelationshipByIDs always returns
// id=0; the actual IDs must be queried back from the kglite engine.
func benchFetchRelIDs(b *testing.B, db *Driver, n int) []graph.ID {
	b.Helper()
	result, err := db.kg.Cypher("MATCH ()-[r]->() RETURN id(r)", nil)
	if err != nil {
		b.Fatalf("benchFetchRelIDs: %v", err)
	}
	ids := make([]graph.ID, 0, len(result.Rows))
	for _, row := range result.Rows {
		if len(row) == 0 {
			continue
		}
		switch v := row[0].(type) {
		case float64:
			ids = append(ids, graph.ID(uint64(v)))
		case int64:
			ids = append(ids, graph.ID(v))
		case uint64:
			ids = append(ids, graph.ID(v))
		}
	}
	if len(ids) != n {
		b.Fatalf("benchFetchRelIDs: expected %d IDs, got %d", n, len(ids))
	}
	return ids
}

// BenchmarkDeleteRelationshipBatched measures the OPT-7 batched path:
// DeleteRelationship accumulates IDs and issues a single IN-clause DELETE
// per defaultDeleteFlushSize IDs.
func BenchmarkDeleteRelationshipBatched1000(b *testing.B) {
	benchmarkDeleteRelationshipBatched(b, 1000)
}

func BenchmarkDeleteRelationshipBatched5000(b *testing.B) {
	benchmarkDeleteRelationshipBatched(b, 5000)
}

func benchmarkDeleteRelationshipBatched(b *testing.B, n int) {
	b.Helper()
	b.ReportAllocs()

	nodeKind := graph.StringKind("BenchDelOpt7Node")

	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		db := openBenchDriver(b)

		// Pre-create src and dst nodes outside the measured window.
		var srcID, dstID graph.ID
		if err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
			src, err := tx.CreateNode(graph.NewProperties(), nodeKind)
			if err != nil {
				return err
			}
			srcID = src.ID
			dst, err := tx.CreateNode(graph.NewProperties(), nodeKind)
			if err != nil {
				return err
			}
			dstID = dst.ID
			return nil
		}); err != nil {
			b.Fatalf("setup nodes: %v", err)
		}

		// Create n relationships with distinct edge types (avoids dedup).
		for i := 0; i < n; i++ {
			kind := graph.StringKind(fmt.Sprintf("BenchDelOpt7Rel%d", i))
			if err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
				_, err := tx.CreateRelationshipByIDs(srcID, dstID, kind, nil)
				return err
			}); err != nil {
				b.Fatalf("setup rel[%d]: %v", i, err)
			}
		}

		// Fetch the real IDs from the engine (tx.CreateRelationshipByIDs returns id=0).
		relIDs := benchFetchRelIDs(b, db, n)

		b.StartTimer()

		// Measured: delete n relationships via the batched IN-clause path.
		err := db.BatchOperation(context.Background(), func(batch graph.Batch) error {
			for _, id := range relIDs {
				if err := batch.DeleteRelationship(id); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.Fatalf("BatchOperation (batched delete): %v", err)
		}

		b.StopTimer()
		_ = db.Close(context.Background())
		b.StartTimer()
	}
}

// BenchmarkDeleteRelationshipPerID measures the legacy per-ID path for
// comparison: each ID is submitted as its own single-query CypherBatchExec call.
// This simulates the OPT-7 baseline (pre-patch behaviour).
func BenchmarkDeleteRelationshipPerID1000(b *testing.B) {
	benchmarkDeleteRelationshipPerID(b, 1000)
}

func BenchmarkDeleteRelationshipPerID5000(b *testing.B) {
	benchmarkDeleteRelationshipPerID(b, 5000)
}

func benchmarkDeleteRelationshipPerID(b *testing.B, n int) {
	b.Helper()
	b.ReportAllocs()

	nodeKind := graph.StringKind("BenchDelBaselineNode")

	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		db := openBenchDriver(b)

		var srcID, dstID graph.ID
		if err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
			src, err := tx.CreateNode(graph.NewProperties(), nodeKind)
			if err != nil {
				return err
			}
			srcID = src.ID
			dst, err := tx.CreateNode(graph.NewProperties(), nodeKind)
			if err != nil {
				return err
			}
			dstID = dst.ID
			return nil
		}); err != nil {
			b.Fatalf("setup nodes: %v", err)
		}

		for i := 0; i < n; i++ {
			kind := graph.StringKind(fmt.Sprintf("BenchDelBaselineRel%d", i))
			if err := db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
				_, err := tx.CreateRelationshipByIDs(srcID, dstID, kind, nil)
				return err
			}); err != nil {
				b.Fatalf("setup rel[%d]: %v", i, err)
			}
		}

		// Fetch the real IDs from the engine.
		relIDs := benchFetchRelIDs(b, db, n)

		b.StartTimer()

		// Baseline: one CypherBatchExec call per relationship ID.
		for _, id := range relIDs {
			if err := db.kg.CypherBatchExec([]kglite.BatchQuery{{
				Query:  fmt.Sprintf("MATCH ()-[r]->() WHERE id(r) = %d DELETE r", uint64(id)),
			}}); err != nil {
				b.Fatalf("per-ID delete: %v", err)
			}
		}

		b.StopTimer()
		_ = db.Close(context.Background())
		b.StartTimer()
	}
}
