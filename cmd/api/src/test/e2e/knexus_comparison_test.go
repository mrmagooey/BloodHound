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

// K-Nexus Global comparison tests.
//
// The k-nexusglobal dataset is a comprehensive multi-platform dataset containing:
//   - 2 AD domains with trust (SharpHound CE)
//   - 1 Entra/Azure tenant (AzureHound CE)
//   - 1 Entra SSO integration
//   - 2 Okta tenants (OktaHound)
//   - 1 GitHub Enterprise account (GitHound)
//   - 1 Jamf tenant (JamfHound)
//
// These tests ingest the dataset into both kglite and Neo4j, run analysis,
// and compare query results to verify correctness.

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	schema "github.com/specterops/bloodhound/packages/go/graphschema"
	"github.com/stretchr/testify/require"
)

// knexusPresetQueries are preset queries run against the k-nexus-global dataset.
// They cover the AD, Azure, and cross-platform node types present in the data.
var knexusPresetQueries = []presetQuery{
	// Overall counts
	{
		Name:   "Total nodes",
		Cypher: `MATCH (n) RETURN count(n) AS nodes`,
	},
	{
		Name:   "Total relationships",
		Cypher: `MATCH ()-[r]->() RETURN count(r) AS relationships`,
	},

	// AD counts
	{
		Name:   "Domains",
		Cypher: `MATCH (n:Domain) RETURN n.name AS domain, n.objectid AS sid ORDER BY domain`,
	},
	{
		Name:   "Computers",
		Cypher: `MATCH (n:Computer) RETURN count(n) AS computers`,
	},
	{
		Name:   "Users",
		Cypher: `MATCH (n:User) RETURN count(n) AS users`,
	},
	{
		Name:   "Groups",
		Cypher: `MATCH (n:Group) RETURN count(n) AS groups`,
	},
	{
		Name:   "OUs",
		Cypher: `MATCH (n:OU) RETURN count(n) AS ous`,
	},
	{
		Name:   "GPOs",
		Cypher: `MATCH (n:GPO) RETURN count(n) AS gpos`,
	},
	{
		Name:   "ADCS cert templates",
		Cypher: `MATCH (n:CertTemplate) RETURN count(n) AS cert_templates`,
	},
	{
		Name:   "Enterprise CAs",
		Cypher: `MATCH (n:EnterpriseCA) RETURN count(n) AS enterprise_cas`,
	},

	// AD security queries
	{
		Name:   "Kerberoastable users",
		Cypher: `MATCH (u:User) WHERE u.hasspn = true AND u.enabled = true RETURN count(u) AS kerberoastable`,
	},
	{
		Name:   "AS-REP roastable users",
		Cypher: `MATCH (u:User) WHERE u.dontreqpreauth = true AND u.enabled = true RETURN count(u) AS asrep_roastable`,
	},
	{
		Name:   "AdminCount users",
		Cypher: `MATCH (u:User) WHERE u.admincount = true RETURN count(u) AS admin_count_users`,
	},
	{
		Name:   "Unconstrained delegation computers",
		Cypher: `MATCH (c:Computer) WHERE c.unconstraineddelegation = true RETURN count(c) AS unconstrained`,
	},
	{
		Name:   "Enabled domain admin users",
		Cypher: `MATCH (u:User)-[:MemberOf*1..]->(g:Group) WHERE g.objectid ENDS WITH '-512' AND u.enabled = true RETURN count(DISTINCT u) AS domain_admins`,
	},

	// AD relationship counts
	{
		Name:   "DCSync relationships",
		Cypher: `MATCH ()-[r:DCSync]->() RETURN count(r) AS dcsync`,
	},
	{
		Name:   "HasSession relationships",
		Cypher: `MATCH ()-[r:HasSession]->() RETURN count(r) AS sessions`,
	},
	{
		Name:   "AdminTo relationships",
		Cypher: `MATCH ()-[r:AdminTo]->() RETURN count(r) AS admin_tos`,
	},
	{
		Name:   "MemberOf relationships",
		Cypher: `MATCH ()-[r:MemberOf]->() RETURN count(r) AS member_ofs`,
	},

	// Azure counts
	{
		Name:   "Azure tenants",
		Cypher: `MATCH (n:AZTenant) RETURN count(n) AS tenants`,
	},
	{
		Name:   "Azure users",
		Cypher: `MATCH (n:AZUser) RETURN count(n) AS az_users`,
	},
	{
		Name:   "Azure groups",
		Cypher: `MATCH (n:AZGroup) RETURN count(n) AS az_groups`,
	},
	{
		Name:   "Azure apps",
		Cypher: `MATCH (n:AZApp) RETURN count(n) AS apps`,
	},
	{
		Name:   "Azure service principals",
		Cypher: `MATCH (n:AZServicePrincipal) RETURN count(n) AS service_principals`,
	},
	{
		Name:   "Azure roles",
		Cypher: `MATCH (n:AZRole) RETURN count(n) AS az_roles`,
	},
	{
		Name:   "Azure VMs",
		Cypher: `MATCH (n:AZVM) RETURN count(n) AS vms`,
	},
	{
		Name:   "Azure devices",
		Cypher: `MATCH (n:AZDevice) RETURN count(n) AS az_devices`,
	},

	// Azure relationships
	{
		Name:   "AZGlobalAdmin relationships",
		Cypher: `MATCH ()-[r:AZGlobalAdmin]->() RETURN count(r) AS global_admins`,
	},
	{
		Name:   "AZOwns relationships",
		Cypher: `MATCH ()-[r:AZOwns]->() RETURN count(r) AS az_owns`,
	},
	{
		Name:   "AZMemberOf relationships",
		Cypher: `MATCH ()-[r:AZMemberOf]->() RETURN count(r) AS az_member_ofs`,
	},
	{
		Name:   "AZHasRole relationships",
		Cypher: `MATCH ()-[r:AZHasRole]->() RETURN count(r) AS az_has_role`,
	},

	// Cross-domain trust
	{
		Name:   "Domain trusts",
		Cypher: `MATCH ()-[r:TrustedBy]->() RETURN count(r) AS trusts`,
	},

	// Relationship type inventory
	{
		Name:   "Distinct relationship types",
		Cypher: `MATCH ()-[r]->() RETURN DISTINCT type(r) AS relationType ORDER BY relationType`,
	},
}

// knexusAttackPathEdges are attack path edge types to compare after analysis.
var knexusAttackPathEdges = []struct {
	Name     string
	EdgeType string
}{
	// AD attack paths
	{"DCSync", "DCSync"},
	{"HasSession", "HasSession"},
	{"MemberOf", "MemberOf"},
	{"AdminTo", "AdminTo"},
	{"GenericAll", "GenericAll"},
	{"GenericWrite", "GenericWrite"},
	{"Owns", "Owns"},
	{"WriteOwner", "WriteOwner"},
	{"ForceChangePassword", "ForceChangePassword"},
	{"AddMember", "AddMember"},
	{"ADCSESC1", "ADCSESC1"},
	{"ADCSESC3", "ADCSESC3"},
	{"SyncLAPSPassword", "SyncLAPSPassword"},
	{"GoldenCert", "GoldenCert"},

	// Azure attack paths
	{"AZGlobalAdmin", "AZGlobalAdmin"},
	{"AZOwns", "AZOwns"},
	{"AZPrivilegedRoleAdmin", "AZPrivilegedRoleAdmin"},
	{"AZMemberOf", "AZMemberOf"},
	{"AZHasRole", "AZHasRole"},
	{"AZContributor", "AZContributor"},
}

// TestCompareKNexus loads the k-nexus-global multi-platform dataset into both
// kglite and Neo4j, runs analysis, and compares query results side-by-side.
func TestCompareKNexus(t *testing.T) {
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

	// Ingest into both
	t.Log("=== Ingesting k-nexus-global data into kglite ===")
	kIngestDur := ingestZipTolerant(ctx, t, kgliteDB, knexusZip, ingestSchema)
	t.Logf("  kglite ingest: %s", kIngestDur.Round(time.Millisecond))

	t.Log("=== Ingesting k-nexus-global data into Neo4j ===")
	nIngestDur := ingestZipTolerant(ctx, t, neo4jDB, knexusZip, ingestSchema)
	t.Logf("  Neo4j ingest: %s", nIngestDur.Round(time.Millisecond))

	// Analysis on both
	t.Log("=== Running analysis on kglite ===")
	kAnalysisDur := runAnalysis(ctx, t, kgliteDB)
	t.Log("=== Running analysis on Neo4j ===")
	nAnalysisDur := runAnalysis(ctx, t, neo4jDB)

	// Performance summary
	t.Log("")
	t.Log("=== Performance Summary: k-nexus-global ===")
	t.Logf("%-25s %12s %12s %10s", "Phase", "kglite", "Neo4j", "Speedup")
	t.Logf("%s", strings.Repeat("-", 65))
	t.Logf("%-25s %12s %12s %10.1fx", "Ingest",
		kIngestDur.Round(time.Millisecond), nIngestDur.Round(time.Millisecond),
		float64(nIngestDur)/float64(kIngestDur))
	t.Logf("%-25s %12s %12s %10.1fx", "Analysis",
		kAnalysisDur.Round(time.Millisecond), nAnalysisDur.Round(time.Millisecond),
		float64(nAnalysisDur)/float64(kAnalysisDur))
	totalK := kIngestDur + kAnalysisDur
	totalN := nIngestDur + nAnalysisDur
	t.Logf("%-25s %12s %12s %10.1fx", "Total (ingest+analysis)",
		totalK.Round(time.Millisecond), totalN.Round(time.Millisecond),
		float64(totalN)/float64(totalK))
	t.Logf("%s", strings.Repeat("-", 65))
	t.Log("")

	// Compare preset queries
	t.Log("=== Comparing k-nexus-global preset queries ===")
	results := compareQueries(ctx, t, kgliteDB, neo4jDB, knexusPresetQueries)
	reportComparison(t, results)

	// Compare attack path edges
	attackQueries := make([]presetQuery, 0, len(knexusAttackPathEdges))
	for _, e := range knexusAttackPathEdges {
		attackQueries = append(attackQueries, presetQuery{
			Name:   e.Name,
			Cypher: fmt.Sprintf("MATCH ()-[r:%s]->() RETURN count(r) AS c", e.EdgeType),
		})
	}
	t.Log("=== Comparing k-nexus-global attack path edges ===")
	attackResults := compareQueries(ctx, t, kgliteDB, neo4jDB, attackQueries)
	reportComparison(t, attackResults)
}

// loadQueriesFromZip extracts presetQuery entries from the 08-queries/ directory
// inside a k-nexus-global zip. Each JSON file has {"name", "query"/"cypher"} fields.
// Queries are grouped by subdirectory (githound, oktahound, jamfhound, hybrid, etc.).
func loadQueriesFromZip(t *testing.T, zipPath string) map[string][]presetQuery {
	t.Helper()
	archive, err := zip.OpenReader(zipPath)
	require.NoError(t, err)
	defer archive.Close()

	groups := make(map[string][]presetQuery)
	for _, f := range archive.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !strings.Contains(f.Name, "/08-queries/") {
			continue
		}
		if !strings.HasSuffix(f.Name, ".json") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			continue
		}
		var data json.RawMessage
		if err := json.NewDecoder(rc).Decode(&data); err != nil {
			rc.Close()
			continue
		}
		rc.Close()

		// Handle both single objects and arrays
		var items []map[string]any
		if data[0] == '[' {
			json.Unmarshal(data, &items)
		} else {
			var single map[string]any
			if err := json.Unmarshal(data, &single); err == nil {
				items = []map[string]any{single}
			}
		}

		// Determine category from path
		parts := strings.Split(f.Name, "/")
		category := "unknown"
		for i, p := range parts {
			if p == "08-queries" && i+1 < len(parts) {
				category = parts[i+1]
				break
			}
		}

		for _, item := range items {
			name, _ := item["name"].(string)
			cypher, _ := item["query"].(string)
			if cypher == "" {
				cypher, _ = item["cypher"].(string)
			}
			if name == "" || cypher == "" {
				continue
			}
			groups[category] = append(groups[category], presetQuery{
				Name:   name,
				Cypher: strings.TrimSpace(cypher),
			})
		}
	}
	return groups
}

// reMultiLabel matches multi-label MATCH patterns like (n:Label1:Label2) that kglite
// does not support.
var reMultiLabel = regexp.MustCompile(`\(\w*:\w+:\w+`)

// reVarLengthEdge matches variable-length edge patterns. Two forms:
//
//   - [*1..4], [*..3], [*] — bare wildcard variable-length, e.g. [*] or [*1..3]
//   - [:Type*1..3], [:TypeA|TypeB*1..2] — typed variable-length: one or more
//     relationship types followed by a hop-count quantifier
//
// Both forms can produce divergent results across backends on large graphs and are
// excluded from comparison runs.  The character class [\w:|]* matches the optional
// variable name, colon, type names, and pipe separators that precede the *.
var reVarLengthEdge = regexp.MustCompile(`\[[\w:|]*\*`)

// filterKgliteCompatible removes queries that use Cypher features that produce
// non-comparable results or that can hang indefinitely on large graphs:
//   - Multi-label MATCH patterns: (n:Label1:Label2)
//   - Bidirectional edges: <-[]->, <-[:Type]->
//   - Variable-length path patterns: [*], [*1..4], [:Type*1..3], etc.
func filterKgliteCompatible(queries []presetQuery) (compatible, skipped []presetQuery) {
	for _, q := range queries {
		switch {
		case reMultiLabel.MatchString(q.Cypher):
			skipped = append(skipped, q)
		case strings.Contains(q.Cypher, "<-[") && strings.Contains(q.Cypher, "]->"):
			skipped = append(skipped, q)
		case reVarLengthEdge.MatchString(q.Cypher):
			skipped = append(skipped, q)
		default:
			compatible = append(compatible, q)
		}
	}
	return
}

// TestCompareKNexusOpenGraph loads the k-nexus-global dataset and runs the
// bundled OpenGraph queries (GitHound, OktaHound, JamfHound, hybrid) against
// both kglite and Neo4j.
func TestCompareKNexusOpenGraph(t *testing.T) {
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

	// Ingest into both
	t.Log("=== Ingesting k-nexus-global data into kglite ===")
	kIngestDur := ingestZipTolerant(ctx, t, kgliteDB, knexusZip, ingestSchema)
	t.Logf("  kglite ingest: %s", kIngestDur.Round(time.Millisecond))

	t.Log("=== Ingesting k-nexus-global data into Neo4j ===")
	nIngestDur := ingestZipTolerant(ctx, t, neo4jDB, knexusZip, ingestSchema)
	t.Logf("  Neo4j ingest: %s", nIngestDur.Round(time.Millisecond))

	// Analysis on both
	t.Log("=== Running analysis on kglite ===")
	runAnalysis(ctx, t, kgliteDB)
	t.Log("=== Running analysis on Neo4j ===")
	runAnalysis(ctx, t, neo4jDB)

	t.Logf("  Ingest: kglite %s, Neo4j %s",
		kIngestDur.Round(time.Millisecond), nIngestDur.Round(time.Millisecond))

	// Load and run bundled queries by category, filtering out queries that use
	// Cypher features kglite cannot parse (multi-label patterns, bidirectional edges).
	queryGroups := loadQueriesFromZip(t, knexusZip)
	t.Logf("Loaded %d query categories from zip", len(queryGroups))

	var totalMatch, totalMismatch, totalError, totalSkipped, totalNonDet int
	for _, category := range []string{"hybrid", "githound", "oktahound", "jamfhound", "oktahound-privilege-zones"} {
		queries, ok := queryGroups[category]
		if !ok || len(queries) == 0 {
			t.Logf("  %s: no queries found, skipping", category)
			continue
		}
		compatible, skipped := filterKgliteCompatible(queries)
		totalSkipped += len(skipped)
		if len(skipped) > 0 {
			t.Logf("  %s: skipped %d queries with unsupported Cypher patterns", category, len(skipped))
		}
		t.Logf("=== Comparing %s queries (%d of %d) ===", category, len(compatible), len(queries))
		results := compareQueries(ctx, t, kgliteDB, neo4jDB, compatible)
		reportComparison(t, results)

		for _, r := range results {
			switch {
			case r.KgliteErr != nil || r.Neo4jErr != nil:
				totalError++
			case !r.Match && r.NonDeterministic:
				totalNonDet++
			case !r.Match:
				totalMismatch++
			default:
				totalMatch++
			}
		}
	}

	t.Logf("")
	t.Logf("OpenGraph query totals: %d match, %d mismatch, %d error, %d nondeterministic, %d skipped (unsupported Cypher)",
		totalMatch, totalMismatch, totalError, totalNonDet, totalSkipped)
}

func init() {
	for _, q := range knexusPresetQueries {
		if q.Name == "" || q.Cypher == "" {
			panic(fmt.Sprintf("knexusPresetQueries has empty entry: %+v", q))
		}
	}
}
