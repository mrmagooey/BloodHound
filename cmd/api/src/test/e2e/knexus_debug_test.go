//go:build comparison && e2e

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

package e2e_test

// Diagnostic test to investigate node count mismatches between kglite and Neo4j
// when ingesting the k-nexus-global dataset.
//
// Known mismatches:
//   - Total nodes: kglite=11444 vs neo4j=9137 (kglite has 2307 MORE)
//   - Users: kglite=518 vs neo4j=558 (kglite has 40 FEWER)
//   - Groups: kglite=191 vs neo4j=194 (kglite has 3 FEWER)
//   - Azure tenants: kglite=1 vs neo4j=2 (kglite has 1 FEWER)

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	schema "github.com/specterops/bloodhound/packages/go/graphschema"
	"github.com/stretchr/testify/require"
)

// debugQuery holds a named diagnostic query.
type debugQuery struct {
	Name   string
	Cypher string
}

// debugQueries defines the diagnostic queries to run on both databases.
var debugQueries = []debugQuery{
	// 1. Node counts by primary label (top 30)
	{
		Name:   "Node counts by label (top 30)",
		Cypher: `MATCH (n) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 30`,
	},

	// 2. Label breakdown for User nodes
	{
		Name:   "User label breakdown",
		Cypher: `MATCH (n:User) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10`,
	},

	// 3. Label breakdown for Group nodes
	{
		Name:   "Group label breakdown",
		Cypher: `MATCH (n:Group) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10`,
	},

	// 4. Label breakdown for AZTenant nodes
	{
		Name:   "AZTenant label breakdown",
		Cypher: `MATCH (n:AZTenant) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10`,
	},

	// 5. Check for nodes without __kinds property
	{
		Name:   "Nodes without __kinds",
		Cypher: `MATCH (n) WHERE n.__kinds IS NULL RETURN count(n) AS no_kinds_count`,
	},

	// 6. Relationship types inventory (top 30)
	{
		Name:   "Relationship types (top 30)",
		Cypher: `MATCH ()-[r]->() RETURN type(r) AS relType, count(r) AS c ORDER BY c DESC LIMIT 30`,
	},

	// 7. Total nodes (sanity check)
	{
		Name:   "Total nodes",
		Cypher: `MATCH (n) RETURN count(n) AS total`,
	},

	// 8. Total relationships (sanity check)
	{
		Name:   "Total relationships",
		Cypher: `MATCH ()-[r]->() RETURN count(r) AS total`,
	},

	// 9. Nodes with Base label only (no other labels)
	{
		Name:   "Base-only nodes",
		Cypher: `MATCH (n:Base) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 20`,
	},

	// 10. Computer label breakdown
	{
		Name:   "Computer label breakdown",
		Cypher: `MATCH (n:Computer) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10`,
	},

	// 11. Domain label breakdown
	{
		Name:   "Domain label breakdown",
		Cypher: `MATCH (n:Domain) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10`,
	},

	// 12. OU label breakdown
	{
		Name:   "OU label breakdown",
		Cypher: `MATCH (n:OU) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10`,
	},

	// 13. GPO label breakdown
	{
		Name:   "GPO label breakdown",
		Cypher: `MATCH (n:GPO) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10`,
	},

	// 14. AZUser label breakdown
	{
		Name:   "AZUser label breakdown",
		Cypher: `MATCH (n:AZUser) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10`,
	},

	// 15. AZGroup label breakdown
	{
		Name:   "AZGroup label breakdown",
		Cypher: `MATCH (n:AZGroup) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10`,
	},

	// 16. AZApp label breakdown
	{
		Name:   "AZApp label breakdown",
		Cypher: `MATCH (n:AZApp) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10`,
	},

	// 17. AZServicePrincipal label breakdown
	{
		Name:   "AZServicePrincipal label breakdown",
		Cypher: `MATCH (n:AZServicePrincipal) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10`,
	},

	// 18. CertTemplate label breakdown
	{
		Name:   "CertTemplate label breakdown",
		Cypher: `MATCH (n:CertTemplate) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10`,
	},

	// 19. Nodes with __kinds property containing "Okta"
	{
		Name:   "Nodes with Okta in __kinds",
		Cypher: `MATCH (n) WHERE n.__kinds CONTAINS 'Okta' RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 20`,
	},

	// 20. Nodes with __kinds property containing "GH"
	{
		Name:   "Nodes with GH in __kinds",
		Cypher: `MATCH (n) WHERE n.__kinds CONTAINS 'GH' RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 20`,
	},

	// 21. Nodes with __kinds property containing "Jamf"
	{
		Name:   "Nodes with Jamf in __kinds",
		Cypher: `MATCH (n) WHERE n.__kinds CONTAINS 'Jamf' RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 20`,
	},

	// 22. Distinct relationship types (full list)
	{
		Name:   "All distinct relationship types",
		Cypher: `MATCH ()-[r]->() RETURN DISTINCT type(r) AS relType ORDER BY relType`,
	},

	// 23. Nodes with single label only
	{
		Name:   "Single-label nodes count",
		Cypher: `MATCH (n) WHERE size(labels(n)) = 1 RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 20`,
	},

	// 24. Nodes with exactly two labels
	{
		Name:   "Two-label nodes count",
		Cypher: `MATCH (n) WHERE size(labels(n)) = 2 RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 20`,
	},

	// 25. Nodes with 3+ labels
	{
		Name:   "Three-or-more-label nodes count",
		Cypher: `MATCH (n) WHERE size(labels(n)) >= 3 RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 20`,
	},
}

// TestKNexusDebug ingests the k-nexus-global dataset into both kglite and Neo4j,
// runs analysis on both, then executes diagnostic queries on each database and
// logs the results side-by-side for investigating node count mismatches.
func TestKNexusDebug(t *testing.T) {
	ctx := context.Background()
	knexusZip := filepath.Join(testdataDir(), "k-nexusglobal_sampledata.zip")
	skipIfMissing(t, knexusZip)

	kgliteDB := openGraph(t)
	neo4jDB := openNeo4j(t)

	// Prepare Neo4j
	clearNeo4j(ctx, t, neo4jDB)
	require.NoError(t, retryNeo4j(t, "AssertSchema", func() error {
		return neo4jDB.AssertSchema(ctx, schema.DefaultGraphSchema())
	}))

	ingestSchema := loadIngestSchema(t)

	// Ingest into both databases
	t.Log("=== Ingesting k-nexus-global data into kglite ===")
	kIngestDur := ingestZipTolerant(ctx, t, kgliteDB, knexusZip, ingestSchema)
	t.Logf("  kglite ingest: %s", kIngestDur.Round(time.Millisecond))

	t.Log("=== Ingesting k-nexus-global data into Neo4j ===")
	nIngestDur := ingestZipTolerant(ctx, t, neo4jDB, knexusZip, ingestSchema)
	t.Logf("  Neo4j ingest: %s", nIngestDur.Round(time.Millisecond))

	// Run analysis on both
	t.Log("=== Running analysis on kglite ===")
	kAnalysisDur := runAnalysis(ctx, t, kgliteDB)
	t.Logf("  kglite analysis: %s", kAnalysisDur.Round(time.Millisecond))

	t.Log("=== Running analysis on Neo4j ===")
	nAnalysisDur := runAnalysis(ctx, t, neo4jDB)
	t.Logf("  Neo4j analysis: %s", nAnalysisDur.Round(time.Millisecond))

	// Run each diagnostic query on both databases and log results
	t.Log("")
	t.Log("========================================================================")
	t.Log("  DIAGNOSTIC QUERIES: kglite vs Neo4j")
	t.Log("========================================================================")
	t.Log("")

	for i, dq := range debugQueries {
		t.Logf("--- [%d/%d] %s ---", i+1, len(debugQueries), dq.Name)
		t.Logf("    Cypher: %s", dq.Cypher)

		kResult, kDur, kErr := runQueryValues(ctx, t, kgliteDB, dq.Cypher)
		nResult, nDur, nErr := runQueryValues(ctx, t, neo4jDB, dq.Cypher)

		// Log kglite results
		t.Logf("")
		t.Logf("  [kglite] (%s):", kDur.Round(time.Microsecond))
		if kErr != nil {
			t.Logf("    ERROR: %v", kErr)
		} else if kResult == "" {
			t.Logf("    (no rows)")
		} else {
			for j, line := range strings.Split(kResult, "\n") {
				t.Logf("    %3d: %s", j+1, line)
			}
		}

		// Log Neo4j results
		t.Logf("")
		t.Logf("  [Neo4j] (%s):", nDur.Round(time.Microsecond))
		if nErr != nil {
			t.Logf("    ERROR: %v", nErr)
		} else if nResult == "" {
			t.Logf("    (no rows)")
		} else {
			for j, line := range strings.Split(nResult, "\n") {
				t.Logf("    %3d: %s", j+1, line)
			}
		}

		// Compare and flag mismatches
		kNorm := normalizeResult(kResult)
		nNorm := normalizeResult(nResult)
		if kErr == nil && nErr == nil && kNorm == nNorm {
			t.Logf("  => MATCH")
		} else if kErr != nil || nErr != nil {
			t.Logf("  => ERROR (one or both queries failed)")
		} else {
			t.Logf("  => MISMATCH")
			// Try to show diff for multi-line results
			kLines := strings.Split(kResult, "\n")
			nLines := strings.Split(nResult, "\n")
			kSet := make(map[string]bool, len(kLines))
			nSet := make(map[string]bool, len(nLines))
			for _, l := range kLines {
				kSet[normalizeResult(l)] = true
			}
			for _, l := range nLines {
				nSet[normalizeResult(l)] = true
			}
			// Lines only in kglite
			var kOnly []string
			for _, l := range kLines {
				if !nSet[normalizeResult(l)] {
					kOnly = append(kOnly, l)
				}
			}
			// Lines only in Neo4j
			var nOnly []string
			for _, l := range nLines {
				if !kSet[normalizeResult(l)] {
					nOnly = append(nOnly, l)
				}
			}
			if len(kOnly) > 0 {
				t.Logf("  Lines ONLY in kglite (%d):", len(kOnly))
				for _, l := range kOnly {
					if len(l) > 200 {
						l = l[:200] + "..."
					}
					t.Logf("    + %s", l)
				}
			}
			if len(nOnly) > 0 {
				t.Logf("  Lines ONLY in Neo4j (%d):", len(nOnly))
				for _, l := range nOnly {
					if len(l) > 200 {
						l = l[:200] + "..."
					}
					t.Logf("    - %s", l)
				}
			}
		}

		t.Logf("")
	}

	// Summary
	t.Log("========================================================================")
	t.Log("  SUMMARY")
	t.Log("========================================================================")
	t.Logf("  kglite ingest: %s, analysis: %s", kIngestDur.Round(time.Millisecond), kAnalysisDur.Round(time.Millisecond))
	t.Logf("  Neo4j  ingest: %s, analysis: %s", nIngestDur.Round(time.Millisecond), nAnalysisDur.Round(time.Millisecond))

	// Run a summary comparison of the main counts
	summaryQueries := []presetQuery{
		{Name: "Total nodes", Cypher: `MATCH (n) RETURN count(n) AS total`},
		{Name: "Total relationships", Cypher: `MATCH ()-[r]->() RETURN count(r) AS total`},
		{Name: "Users", Cypher: `MATCH (n:User) RETURN count(n) AS c`},
		{Name: "Groups", Cypher: `MATCH (n:Group) RETURN count(n) AS c`},
		{Name: "AZTenants", Cypher: `MATCH (n:AZTenant) RETURN count(n) AS c`},
		{Name: "Computers", Cypher: `MATCH (n:Computer) RETURN count(n) AS c`},
		{Name: "Domains", Cypher: `MATCH (n:Domain) RETURN count(n) AS c`},
		{Name: "AZUsers", Cypher: `MATCH (n:AZUser) RETURN count(n) AS c`},
		{Name: "AZGroups", Cypher: `MATCH (n:AZGroup) RETURN count(n) AS c`},
		{Name: "AZApps", Cypher: `MATCH (n:AZApp) RETURN count(n) AS c`},
		{Name: "AZServicePrincipal", Cypher: `MATCH (n:AZServicePrincipal) RETURN count(n) AS c`},
	}
	t.Logf("")
	t.Logf("  %-30s %12s %12s %10s", "Metric", "kglite", "Neo4j", "Diff")
	t.Logf("  %s", strings.Repeat("-", 70))
	for _, sq := range summaryQueries {
		kRes, _, kErr := runQueryValues(ctx, t, kgliteDB, sq.Cypher)
		nRes, _, nErr := runQueryValues(ctx, t, neo4jDB, sq.Cypher)
		kStr := kRes
		nStr := nRes
		diff := ""
		if kErr != nil {
			kStr = "ERR"
		}
		if nErr != nil {
			nStr = "ERR"
		}
		if kErr == nil && nErr == nil {
			kNorm := normalizeResult(kRes)
			nNorm := normalizeResult(nRes)
			if kNorm == nNorm {
				diff = "MATCH"
			} else {
				diff = fmt.Sprintf("MISMATCH")
			}
		}
		t.Logf("  %-30s %12s %12s %10s", sq.Name,
			truncateStr(kStr, 12), truncateStr(nStr, 12), diff)
	}
	t.Logf("  %s", strings.Repeat("-", 70))
}
