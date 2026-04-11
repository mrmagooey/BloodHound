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

package ad_test

import (
	"testing"

	adAnalysis "github.com/specterops/bloodhound/packages/go/analysis/ad"
	"github.com/specterops/bloodhound/packages/go/graphschema/ad"
	"github.com/specterops/bloodhound/packages/go/graphschema/common"
	"github.com/specterops/dawgs/cypher/models/cypher"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildSIDSuffixOrCriteria_Empty verifies that an empty suffix list produces a
// valid (if vacuously-false) OR with no arms — the cypher layer handles empty
// disjunctions gracefully rather than panicking.
func TestBuildSIDSuffixOrCriteria_Empty(t *testing.T) {
	criteria := adAnalysis.BuildSIDSuffixOrCriteria([]string{})
	require.NotNil(t, criteria)

	paren, ok := criteria.(*cypher.Parenthetical)
	require.True(t, ok, "expected *cypher.Parenthetical, got %T", criteria)
	require.NotNil(t, paren.Expression)
}

// TestBuildSIDSuffixOrCriteria_Single verifies that a single suffix produces a
// Parenthetical wrapping a Disjunction with exactly one Comparison child.
func TestBuildSIDSuffixOrCriteria_Single(t *testing.T) {
	criteria := adAnalysis.BuildSIDSuffixOrCriteria([]string{"-512"})
	require.NotNil(t, criteria)

	paren, ok := criteria.(*cypher.Parenthetical)
	require.True(t, ok, "expected *cypher.Parenthetical, got %T", criteria)

	disj, ok := paren.Expression.(*cypher.Disjunction)
	require.True(t, ok, "expected Disjunction inside Parenthetical, got %T", paren.Expression)
	assert.Len(t, disj.Expressions, 1, "expected exactly 1 expression for 1 suffix")

	_, isComparison := disj.Expressions[0].(*cypher.Comparison)
	assert.True(t, isComparison, "expected Comparison inside Disjunction, got %T", disj.Expressions[0])
}

// TestBuildSIDSuffixOrCriteria_Multiple verifies that multiple suffixes each produce
// their own Comparison arm inside the Disjunction, and that the count matches.
func TestBuildSIDSuffixOrCriteria_Multiple(t *testing.T) {
	suffixes := []string{"-512", "-519", "-500", "-516", "-518", "-520", "-526", "-527", "-551", "-544"}
	criteria := adAnalysis.BuildSIDSuffixOrCriteria(suffixes)
	require.NotNil(t, criteria)

	paren, ok := criteria.(*cypher.Parenthetical)
	require.True(t, ok, "expected *cypher.Parenthetical, got %T", criteria)

	disj, ok := paren.Expression.(*cypher.Disjunction)
	require.True(t, ok, "expected Disjunction inside Parenthetical, got %T", paren.Expression)
	assert.Len(t, disj.Expressions, len(suffixes),
		"OR should have exactly one arm per SID suffix")

	// Every arm must be a Comparison (StringEndsWith produces a Comparison)
	for i, expr := range disj.Expressions {
		_, ok := expr.(*cypher.Comparison)
		assert.True(t, ok, "arm %d should be a *cypher.Comparison, got %T", i, expr)
	}
}

// TestBuildSIDSuffixOrCriteria_TierZeroSuffixes checks the real production suffix list
// (10 entries) produces the correct query structure with all 10 OR arms.
func TestBuildSIDSuffixOrCriteria_TierZeroSuffixes(t *testing.T) {
	suffixes := adAnalysis.TierZeroWellKnownSIDSuffixes()
	require.Len(t, suffixes, 10, "TierZeroWellKnownSIDSuffixes should return exactly 10 entries")

	criteria := adAnalysis.BuildSIDSuffixOrCriteria(suffixes)
	require.NotNil(t, criteria)

	paren, ok := criteria.(*cypher.Parenthetical)
	require.True(t, ok)

	disj, ok := paren.Expression.(*cypher.Disjunction)
	require.True(t, ok)
	assert.Len(t, disj.Expressions, 10,
		"combined OR must contain one arm per tier-zero SID suffix")
}

// TestBuildSIDSuffixOrCriteria_ReturnsGraphCriteria checks that the return type
// satisfies graph.Criteria so it can be passed directly to query.And().
func TestBuildSIDSuffixOrCriteria_ReturnsGraphCriteria(t *testing.T) {
	criteria := adAnalysis.BuildSIDSuffixOrCriteria([]string{"-512"})
	var _ graph.Criteria = criteria // compile-time assertion
	require.NotNil(t, criteria)
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

// BenchmarkBuildSIDSuffixOrCriteria benchmarks the helper that builds the combined
// OR predicate for all tier-zero SID suffixes.  The cost is dominated by
// building slice of Comparison expressions — this is called once per
// FetchWellKnownTierZeroEntities invocation.
func BenchmarkBuildSIDSuffixOrCriteria(b *testing.B) {
	suffixes := adAnalysis.TierZeroWellKnownSIDSuffixes()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = adAnalysis.BuildSIDSuffixOrCriteria(suffixes)
	}
}

// BenchmarkBuildSIDSuffixCriteriaOld simulates the old approach: building N
// separate AND criteria (one per suffix), which is what the original
// loop would have produced for each separate query.  Comparing ns/op with
// BenchmarkBuildSIDSuffixOrCriteria illustrates the query-construction overhead
// of the old approach while the real gain is reduced DB round-trips (10→1).
func BenchmarkBuildSIDSuffixCriteriaOld(b *testing.B) {
	const domainSID = "S-1-5-21-1234567890-1234567890-1234567890"
	suffixes := adAnalysis.TierZeroWellKnownSIDSuffixes()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		// Simulate what the old loop built for each per-suffix query.
		for _, suffix := range suffixes {
			_ = query.And(
				query.KindIn(query.Node(), ad.Group, ad.User),
				query.KindIn(query.Node(), ad.Entity),
				query.StringEndsWith(query.NodeProperty(common.ObjectID.String()), suffix),
				query.Equals(query.NodeProperty(ad.DomainSID.String()), domainSID),
			)
		}
	}
}

// BenchmarkBuildSIDSuffixCriteriaNew benchmarks building the single combined
// AND+OR query used by OPT-23, allowing direct comparison with the old approach.
func BenchmarkBuildSIDSuffixCriteriaNew(b *testing.B) {
	const domainSID = "S-1-5-21-1234567890-1234567890-1234567890"
	suffixes := adAnalysis.TierZeroWellKnownSIDSuffixes()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = query.And(
			query.KindIn(query.Node(), ad.Group, ad.User),
			query.KindIn(query.Node(), ad.Entity),
			adAnalysis.BuildSIDSuffixOrCriteria(suffixes),
			query.Equals(query.NodeProperty(ad.DomainSID.String()), domainSID),
		)
	}
}
