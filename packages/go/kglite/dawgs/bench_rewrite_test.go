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

import (
	"testing"
)

// analysisQueries contains representative Cypher queries emitted by the neo4j
// query builder during BloodHound analysis.  These are the patterns that run
// thousands of times per analysis job and benefit most from caching.
var analysisQueries = []struct {
	name   string
	cypher string
	params map[string]any
}{
	{
		name:   "find_computers_by_objectid",
		cypher: `match (n) where n.objectid ends with $p0 and not ((n:Group or n:ADLocalGroup)) return n`,
		params: map[string]any{"p0": "-S-1-5-32-544"},
	},
	{
		name:   "multi_type_rel_no_where",
		cypher: `MATCH (n)-[:MemberOf|GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner]->(g) RETURN g`,
		params: map[string]any{},
	},
	{
		name:   "multi_type_rel_with_where",
		cypher: `MATCH (n)-[:MemberOf|GenericAll]->(g) WHERE g.domain = $domain RETURN g`,
		params: map[string]any{"domain": "CONTOSO.LOCAL"},
	},
	{
		name:   "label_check_in_where",
		cypher: `MATCH (n) WHERE n:Computer AND n.enabled = true RETURN n`,
		params: map[string]any{},
	},
	{
		name:   "label_check_with_not",
		cypher: `MATCH (n) WHERE NOT (n:Group OR n:ADLocalGroup) AND n.objectid = $id RETURN n`,
		params: map[string]any{"id": "S-1-5-21-1234"},
	},
	{
		name:   "in_param_expansion",
		cypher: `MATCH (n) WHERE n.objectid IN $ids RETURN n`,
		params: map[string]any{"ids": []string{"S-1-1", "S-1-2", "S-1-3", "S-1-4", "S-1-5"}},
	},
	{
		name:   "complex_analysis_query",
		cypher: `MATCH (s)-[:MemberOf|AdminTo|HasSession]->(t) WHERE s:User AND t:Computer AND t.enabled = true RETURN s, t`,
		params: map[string]any{},
	},
	{
		name:   "empty_where",
		cypher: `match (n) where  return n`,
		params: map[string]any{},
	},
	{
		name:   "acl_path_query",
		cypher: `MATCH (n)-[r:GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner|AllExtendedRights]->(m) WHERE n.objectid = $src RETURN m`,
		params: map[string]any{"src": "S-1-5-21-9999-500"},
	},
	{
		name:   "node_fetch_by_id",
		cypher: `MATCH (n) WHERE id(n) = $id RETURN n`,
		params: map[string]any{"id": int64(42)},
	},
}

// rewriteForKgliteUncached is the original (no-cache) implementation used as
// the baseline in benchmarks.  It applies the same four passes but skips the
// structural cache so every call pays the full regex cost.
func rewriteForKgliteUncached(cypher string, params map[string]any) string {
	cypher = rewriteMultiTypeRel(cypher)
	cypher = rewriteLabelWhere(cypher)
	cypher = rewriteInParam(cypher, params)
	cypher = rewriteEmptyWhere(cypher)
	return cypher
}

// BenchmarkRewriteUncached measures the cost of applying all four rewrite
// passes on every call with no caching — the original baseline.
func BenchmarkRewriteUncached(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		q := analysisQueries[i%len(analysisQueries)]
		// Copy params so rewriteInParam can delete consumed keys without
		// corrupting the shared slice entry.
		params := make(map[string]any, len(q.params))
		for k, v := range q.params {
			params[k] = v
		}
		_ = rewriteForKgliteUncached(q.cypher, params)
	}
}

// BenchmarkRewriteCached measures the cost of rewriteForKglite with the
// structural cache warmed up.  After the first pass through all queries the
// cache is fully populated and subsequent iterations pay only a sync.Map
// lookup + rewriteInParam (for queries with IN $param) or a lookup-only cost.
func BenchmarkRewriteCached(b *testing.B) {
	b.ReportAllocs()

	// Warm the structural cache before starting the timer.
	for _, q := range analysisQueries {
		params := make(map[string]any, len(q.params))
		for k, v := range q.params {
			params[k] = v
		}
		_ = rewriteForKglite(q.cypher, params)
	}

	b.ResetTimer()
	for i := range b.N {
		q := analysisQueries[i%len(analysisQueries)]
		params := make(map[string]any, len(q.params))
		for k, v := range q.params {
			params[k] = v
		}
		_ = rewriteForKglite(q.cypher, params)
	}
}

// BenchmarkRewriteCachedColdStart measures rewriteForKglite including the
// initial cache-miss cost (no pre-warming).  The cache state carries over
// across iterations within a single b.N run so this approaches the cached
// steady-state after the first pass.
func BenchmarkRewriteCachedColdStart(b *testing.B) {
	b.ReportAllocs()

	// Clear cache so every b.N run starts cold.
	rewriteStructuralCache.Range(func(k, _ any) bool {
		rewriteStructuralCache.Delete(k)
		return true
	})

	b.ResetTimer()
	for i := range b.N {
		q := analysisQueries[i%len(analysisQueries)]
		params := make(map[string]any, len(q.params))
		for k, v := range q.params {
			params[k] = v
		}
		_ = rewriteForKglite(q.cypher, params)
	}
}

// BenchmarkStructuralRewriteOnly isolates the three parameter-independent
// passes to show the per-call cost that the cache eliminates.
func BenchmarkStructuralRewriteOnly(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		q := analysisQueries[i%len(analysisQueries)]
		_ = rewriteStructural(q.cypher)
	}
}

// BenchmarkStructuralRewriteOnlyCold is like BenchmarkStructuralRewriteOnly
// but clears the cache before each sub-benchmark run so the cache miss cost
// is included.
func BenchmarkStructuralRewriteOnlyCold(b *testing.B) {
	b.ReportAllocs()

	rewriteStructuralCache.Range(func(k, _ any) bool {
		rewriteStructuralCache.Delete(k)
		return true
	})

	b.ResetTimer()
	for i := range b.N {
		q := analysisQueries[i%len(analysisQueries)]
		_ = rewriteStructural(q.cypher)
	}
}
