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

// OPT-18: Reduce allocations in propsPattern and setClause.
//
// Before OPT-18 each call to propsPattern or setClause:
//   - Allocated a fresh []string for sorted keys
//   - Allocated a fresh []string for Cypher fragment parts
//   - Allocated a strings.Builder in sanitizeKey (one per key per call)
//   - Allocated a map[string]any for params
//   - mergeParams() allocated a new map regardless of hint size
//
// After OPT-18:
//   - sanitizeKey results are cached in a package-level sync.Map — repeated
//     calls for "objectid", "name", "enabled", etc. are free after the first.
//   - []string slices for keys and parts are recycled via sync.Pool.
//   - strings.Builder instances are recycled via a separate sync.Pool.
//   - mergeParams pre-sizes the output map to sum of input sizes.
//
// Run with:
//
//	go test -tags standalone -run=^$ -bench=BenchmarkOPT18 -benchmem -benchtime=3s \
//	    ./packages/go/kglite/dawgs/

//go:build standalone

package dawgs

import (
	"context"
	"fmt"
	"testing"

	"github.com/specterops/dawgs/graph"
)

// typicalNodeProps represents the property set used for most BloodHound nodes.
var typicalNodeProps = map[string]any{
	"objectid":  "S-1-5-21-111-222-333-500",
	"name":      "ADMINISTRATOR@CONTOSO.LOCAL",
	"enabled":   true,
	"lastseen":  int64(1_700_000_000),
	"domain":    "CONTOSO.LOCAL",
	"sensitive": false,
}

// typicalIdentityProps is the single-key identity map used in every MERGE.
var typicalIdentityProps = map[string]any{
	"objectid": "S-1-5-21-111-222-333-500",
}

// ─── micro-benchmarks: individual functions ───────────────────────────────────

// BenchmarkOPT18_PropsPattern_Typical measures propsPattern on the 6-property
// node map representative of normal AD object ingest.
func BenchmarkOPT18_PropsPattern_Typical(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		pat, params := propsPattern("p_", typicalNodeProps)
		_ = pat
		_ = params
	}
}

// BenchmarkOPT18_PropsPattern_Identity measures propsPattern on the 1-property
// identity map (the most frequent MERGE match pattern).
func BenchmarkOPT18_PropsPattern_Identity(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		pat, params := propsPattern("id_", typicalIdentityProps)
		_ = pat
		_ = params
	}
}

// BenchmarkOPT18_SetClause_Typical measures setClause on the 6-property node
// map, reflecting the SET fragment built for every UpdateNodeBy call.
func BenchmarkOPT18_SetClause_Typical(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		frag, params := setClause("n", "p_", typicalNodeProps)
		_ = frag
		_ = params
	}
}

// BenchmarkOPT18_SanitizeKey_Cached measures sanitizeKey after the cache has
// been warmed for common property names. The vast majority of calls during a
// real ingest hit this fast path.
func BenchmarkOPT18_SanitizeKey_Cached(b *testing.B) {
	// Warm the cache for typical keys.
	for _, k := range []string{"objectid", "name", "enabled", "lastseen", "domain", "sensitive"} {
		_ = sanitizeKey(k)
	}
	b.ReportAllocs()
	b.ResetTimer()
	keys := []string{"objectid", "name", "enabled", "lastseen", "domain", "sensitive"}
	for i := range b.N {
		_ = sanitizeKey(keys[i%len(keys)])
	}
}

// BenchmarkOPT18_SanitizeKey_Miss measures sanitizeKey on a cache-miss (unique
// keys not previously seen), exercising the strings.Builder slow path.
func BenchmarkOPT18_SanitizeKey_Miss(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		// Unique key to guarantee a cache miss.
		key := fmt.Sprintf("unique_opt18_key_%d", i)
		_ = sanitizeKey(key)
	}
}

// BenchmarkOPT18_MergeParams_TwoMaps measures mergeParams with two maps —
// the pattern used by UpdateNodeBy (identity params + prop params).
func BenchmarkOPT18_MergeParams_TwoMaps(b *testing.B) {
	idParams := map[string]any{
		"id_objectid": "S-1-5-21-111-222-333-500",
	}
	propParams := map[string]any{
		"p_objectid":  "S-1-5-21-111-222-333-500",
		"p_name":      "ADMINISTRATOR@CONTOSO.LOCAL",
		"p_enabled":   true,
		"p_lastseen":  int64(1_700_000_000),
		"p_domain":    "CONTOSO.LOCAL",
		"p_sensitive": false,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		merged := mergeParams(idParams, propParams)
		_ = merged
	}
}

// BenchmarkOPT18_MergeParams_FiveMaps measures mergeParams with five maps —
// the worst case from the triple-MERGE slow path in UpdateRelationshipBy.
func BenchmarkOPT18_MergeParams_FiveMaps(b *testing.B) {
	a := map[string]any{"si_objectid": "src-oid"}
	c := map[string]any{"ei_objectid": "dst-oid"}
	e := map[string]any{"rp_isacl": true}
	f := map[string]any{"sp_objectid": "src-oid", "sp_name": "SRC", "sp_enabled": true}
	g := map[string]any{"ep_objectid": "dst-oid", "ep_name": "DST", "ep_enabled": false}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		merged := mergeParams(a, c, e, f, g)
		_ = merged
	}
}

// ─── full BatchOperation benchmarks ──────────────────────────────────────────

// BenchmarkOPT18_NodeIngestion_1000 benchmarks a full BatchOperation of 1000
// UpdateNodeBy calls — the primary hot path that OPT-18 targets.
func BenchmarkOPT18_NodeIngestion_1000(b *testing.B) {
	benchmarkOPT18NodeIngestion(b, 1000)
}

// BenchmarkOPT18_NodeIngestion_5000 uses the default flush size to show the
// full improvement at one complete flush boundary.
func BenchmarkOPT18_NodeIngestion_5000(b *testing.B) {
	benchmarkOPT18NodeIngestion(b, 5000)
}

func benchmarkOPT18NodeIngestion(b *testing.B, n int) {
	b.Helper()
	b.ReportAllocs()

	kind := graph.StringKind("OPT18Node")

	// Pre-build node list outside the timer.
	nodes := make([]*graph.Node, n)
	for i := 0; i < n; i++ {
		nodes[i] = graph.NewNode(0,
			graph.AsProperties(map[string]any{
				"objectid":  fmt.Sprintf("opt18-node-%d", i),
				"name":      fmt.Sprintf("OPT18 Node %d", i),
				"enabled":   true,
				"lastseen":  int64(1_700_000_000 + i),
				"domain":    "CONTOSO.LOCAL",
				"sensitive": false,
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

// BenchmarkOPT18_PropsPattern_Parallel measures propsPattern under concurrent
// access to verify that sync.Pool and sync.Map caching are contention-free.
func BenchmarkOPT18_PropsPattern_Parallel(b *testing.B) {
	// Warm the sanitizeKey cache before the parallel run.
	for _, k := range []string{"objectid", "name", "enabled", "lastseen", "domain", "sensitive"} {
		_ = sanitizeKey(k)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pat, params := propsPattern("p_", typicalNodeProps)
			_ = pat
			_ = params
		}
	})
}
