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

// AD_Miner comparison queries.
//
// These Cypher queries are extracted from AD_Miner
// (https://github.com/AD-Security/AD_Miner), a popular BloodHound data
// analysis tool. We run them against both kglite and Neo4j to verify
// correctness and measure performance.
//
// Only read-only queries are included. Queries that use template variables
// ($extract_date$, $properties$, PARAM1/PARAM2), GDS, APOC, SHOW, CALL,
// or CREATE/SET/DELETE/REMOVE/MERGE are excluded.
//
// Aliases with spaces use backticks (Cypher standard) instead of the
// double-quoted strings found in the original AD_Miner source, since
// double-quoted strings are not valid alias identifiers in Cypher.

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

// adminerADQueries are read-only AD_Miner Cypher queries that target AD node types.
var adminerADQueries = []presetQuery{
	{
		Name:   "ADMiner: Checking relation types",
		Cypher: "MATCH ()-[r]->() RETURN DISTINCT type(r) as relationType",
	},
	{
		Name:   "ADMiner: Count number of domains collected",
		Cypher: "MATCH (m:Domain{collected:true}) RETURN m.name",
	},
	{
		Name:   "ADMiner: List of domains",
		Cypher: "MATCH (m:Domain) RETURN DISTINCT(m.name) AS domain ORDER BY m.name",
	},
	{
		Name:   "ADMiner: Number of AS-REP Roastable accounts",
		Cypher: "MATCH (u:User{enabled:true,dontreqpreauth: true}) RETURN u.domain AS domain,u.name AS name, u.is_da AS is_Domain_Admin",
	},
	{
		Name:   "ADMiner: Machines with unconstrained delegations",
		Cypher: "MATCH (c2:Computer{unconstraineddelegation:true,is_dc:FALSE}) RETURN DISTINCT(c2.domain) AS domain,c2.name AS name",
	},
	{
		Name:   "ADMiner: Users with unconstrained delegations",
		Cypher: "MATCH (c2:User{enabled:true,unconstraineddelegation:true,is_da:FALSE}) RETURN DISTINCT(c2.domain) AS domain,c2.name AS name",
	},
	{
		Name:   "ADMiner: Users with constrained delegations",
		Cypher: "MATCH (u:User)-[:AllowedToDelegate]->(c:Computer) WHERE u.name IS NOT NULL AND c.name IS NOT NULL RETURN u.name AS name, c.name AS computer,c.is_dc as to_DC ORDER BY name",
	},
	{
		Name:   "ADMiner: Accounts with cleartext password",
		Cypher: "MATCH (u:User) WHERE NOT u.userpassword IS null RETURN u.name AS user, u.is_da as is_Domain_Admin",
	},
	{
		Name:   "ADMiner: Objects with AdminSDHolder",
		Cypher: "MATCH (n{enabled:True, admincount:True}) RETURN n.domain as domain, labels(n) as type, n.name as name",
	},
	{
		Name:   "ADMiner: High privilege group computer member",
		Cypher: "MATCH(c:Computer{is_dc:false})-[r:MemberOf*1..4]->(g:Group{is_da:true}) WHERE NOT c.name IS NULL RETURN distinct(c.name) AS computer, g.name AS `group`, g.domain AS domain",
	},
	{
		Name:   "ADMiner: Computers admin of computers",
		Cypher: "MATCH (c1:Computer)-[:MemberOf*0..]->()-[:AdminTo]->(c2:Computer) WHERE c1 <> c2 RETURN DISTINCT c1.name AS source_computer, c2.name AS target_computer, c2.has_path_to_da AS has_path_to_da, c2.smbsigning AS smbsigning",
	},
	{
		Name:   "ADMiner: Domain map trust",
		Cypher: "MATCH p=shortestpath((d:Domain)-[:TrustedBy|AbuseTGTDelegation|SameForestTrust|SpoofSIDHistory|CrossForestTrust]->(m:Domain)) WHERE d<>m RETURN DISTINCT(p)",
	},
	{
		Name:   "ADMiner: Domain admin sessions on non-DC",
		Cypher: "MATCH p=(c:Computer{path_candidate:true})-[r:HasSession]->(u:User{enabled:true, is_da:true}) WHERE NOT c.name IS NULL and NOT u.name IS NULL and NOT c.is_dc=True RETURN distinct(p) AS p",
	},
	{
		Name: "ADMiner: Functional level vulnerabilities",
		Cypher: "MATCH (o:Domain) WHERE NOT(o.functionallevel IS NULL OR SIZE(o.functionallevel) < 1) RETURN " +
			"CASE WHEN toUpper(o.functionallevel) CONTAINS '2000' OR toUpper(o.functionallevel) CONTAINS '2003' " +
			"OR toUpper(o.functionallevel) CONTAINS '2008' OR toUpper(o.functionallevel) CONTAINS '2008 R2' THEN 1 " +
			"WHEN toUpper(o.functionallevel) CONTAINS '2012' THEN 2 " +
			"WHEN toUpper(o.functionallevel) CONTAINS '2016' OR toUpper(o.functionallevel) CONTAINS '2018' " +
			"OR toUpper(o.functionallevel) CONTAINS '2020' OR toUpper(o.functionallevel) CONTAINS '2022' THEN 5 " +
			"END as level_maturity, o.distinguishedname as full_name, o.functionallevel as functional_level",
	},
	{
		Name:   "ADMiner: SID history to privileged accounts",
		Cypher: "MATCH(o1)-[r:HasSIDHistory]->(o2{is_da:true}) RETURN o1.domain as parent_domain, o1.name as name, o1.sidhistory as sidhistory",
	},
	{
		Name: "ADMiner: ACL anomalies on groups",
		Cypher: "MATCH (gg:Group) WHERE gg.members_count IS NOT NULL with gg as g order by gg.members_count DESC " +
			"MATCH (g)-[r2{isacl:true}]->(n) WHERE ((g.is_da IS NULL OR g.is_da=FALSE) AND (g.is_dcg IS NULL OR g.is_dcg=FALSE) " +
			"AND (NOT g.is_adcs OR g.is_adcs IS NULL)) OR (NOT n.domain CONTAINS '.' + g.domain AND n.domain <> g.domain) " +
			"RETURN g.members_count,n.name,g.name,type(r2),LABELS(g),labels(n),ID(n) order by g.members_count DESC",
	},
	{
		Name:   "ADMiner: Empty groups",
		Cypher: "MATCH (g:Group) WHERE NOT EXISTS(()-[:MemberOf]->(g)) AND NOT g.distinguishedname CONTAINS 'CN=BUILTIN' RETURN g.name AS empty_group, COALESCE(g.distinguishedname, '-') AS full_reference",
	},
	{
		Name:   "ADMiner: Objects with SID History",
		Cypher: "MATCH (a)-[r:HasSIDHistory]->(b) RETURN a.name AS has_sid_history, LABELS(a) AS type_a, b.name AS target, LABELS(b) AS type_b",
	},
	{
		Name:   "ADMiner: Cross-domain local admins",
		Cypher: "MATCH p=(u{enabled:true})-[r:MemberOf*0..4]->()-[rr:AdminTo]->(c:Computer) WHERE c.ghost_computer IS NULL AND u.domain <> c.domain AND NOT c.domain CONTAINS u.domain RETURN DISTINCT p",
	},
	{
		Name:   "ADMiner: Cross-domain domain admins",
		Cypher: "MATCH p=(u{enabled:true})-[r:MemberOf*1..4]->(g:Group{is_da:true}) WHERE u.domain <> g.domain AND NOT g.domain CONTAINS u.domain return p",
	},
	{
		Name:   "ADMiner: Guest accounts enabled",
		Cypher: "MATCH (n:User) WHERE n.objectid ENDS WITH '-501' RETURN n.name, n.domain, n.enabled",
	},
	{
		Name:   "ADMiner: Unprivileged users with admincount",
		Cypher: "MATCH (u:User{enabled:true}) WHERE u.is_da=false AND u.admincount=true RETURN u.name, u.domain, u.da_type",
	},
	{
		Name: "ADMiner: FGPP applied to users",
		Cypher: "MATCH (u:User) WHERE u.fgpp_name IS NOT NULL RETURN u.fgpp_msds_psoappliesto, u.fgpp_name, " +
			"u.fgpp_msds_minimumpasswordlength, u.fgpp_msds_minimumpasswordage, u.fgpp_msds_maximumpasswordage, " +
			"u.fgpp_msds_passwordreversibleencryptionenabled, u.fgpp_msds_passwordhistorylength, " +
			"u.fgpp_msds_passwordcomplexityenabled, u.fgpp_msds_lockoutduration, u.fgpp_msds_lockoutthreshold, " +
			"u.fgpp_msds_lockoutobservationwindow",
	},
	{
		Name:   "ADMiner: Number of groups",
		Cypher: "MATCH p=(g:Group) WHERE NOT g.name IS NULL AND NOT g.domain IS NULL RETURN DISTINCT(g.domain) AS domain, g.name AS name, g.is_da AS da ORDER BY g.domain",
	},
	{
		Name:   "ADMiner: Number of computers",
		Cypher: "MATCH (c:Computer) WHERE NOT c.name IS NULL RETURN DISTINCT(c.domain) AS domain, c.name AS name, c.operatingsystem AS os, c.ghost_computer AS ghost, c.enabled as enabled ORDER BY c.domain",
	},
	{
		Name:   "ADMiner: Number of domain admin accounts",
		Cypher: "MATCH (n{enabled:true}) WHERE n.is_msol IS NULL AND n.is_da = TRUE RETURN n.domain AS domain, n.name AS name, n.da_types AS admin_type, n.admincount AS admincount",
	},
	{
		Name:   "ADMiner: DCSync capable objects",
		Cypher: "MATCH (n{can_dcsync:true}) RETURN n.domain as domain, n.name as name",
	},
	{
		Name:   "ADMiner: LDAP/LDAPS configuration",
		Cypher: "MATCH (c) WHERE c.ldapavailable OR c.ldapsavailable RETURN c.name AS name, c.domain AS domain, c.ldapavailable AS ldap, c.ldapsavailable AS ldaps, c.ldapsigning AS ldapsigning, c.ldapsepa AS ldapsepa",
	},
	{
		Name:   "ADMiner: Pre-Windows 2000 Compatible Access group",
		Cypher: "MATCH (n:Group) WHERE n.name STARTS WITH 'PRE-WINDOWS 2000 COMPATIBLE ACCESS@' MATCH (m)-[r:MemberOf]->(n) WHERE NOT m.objectid ENDS WITH '-S-1-5-11' return m.domain, m.name, m.objectid, labels(m) as type",
	},
	{
		Name:   "ADMiner: Domain Organisational Units",
		Cypher: "MATCH (o:OU)-[:Contains]->(c) RETURN o.name AS OU, c.name AS name",
	},
	{
		Name:   "ADMiner: Empty OUs",
		Cypher: "MATCH (o:OU) WHERE NOT ()<-[:Contains]-(o) RETURN o.name AS `Empty Organizational Unit`, COALESCE(o.distinguishedname, '-') AS `Full Reference`",
	},
	{
		Name:   "ADMiner: ACL anomalies on non-Group enabled objects",
		Cypher: "MATCH (gg) WHERE NOT gg:Group AND ((gg:User AND gg.enabled) OR (gg:Computer AND gg.enabled) OR (NOT (gg:User OR gg:Computer))) WITH gg as g MATCH (g)-[r2{isacl:true}]->(n) WHERE ((g.is_da IS NULL OR g.is_da=FALSE) AND (g.is_dc IS NULL OR g.is_dc=FALSE) AND (NOT g.is_adcs OR g.is_adcs IS NULL)) OR (NOT n.domain CONTAINS '.' + g.domain AND n.domain <> g.domain) RETURN n.name,g.name,type(r2),LABELS(g),labels(n),ID(n)",
	},
	{
		Name:   "ADMiner: PrimaryGroupID lower than 1000",
		Cypher: "MATCH (n) WHERE (n:Group OR n:User) AND toInteger(split(n.objectid, '-')[-1]) < 1000 AND (n.enabled = true or n:Group) return toInteger(split(n.objectid, '-')[-1]) as sid, n.name, n.domain, n.is_da",
	},
}

// adminerAzureQueries are read-only AD_Miner Cypher queries that target Azure node types.
var adminerAzureQueries = []presetQuery{
	{
		Name:   "ADMiner: Azure Users",
		Cypher: "MATCH (n:AZUser) RETURN n.name AS Name, n.tenantid AS tenant_id, n.onpremisesyncenabled AS onpremisesynced, n.onpremisesecurityidentifier AS SID",
	},
	{
		Name:   "ADMiner: Azure Admins",
		Cypher: "MATCH p =(n)-[r:AZGlobalAdmin*1..]->(m) RETURN n.name AS Name, n.tenantid AS tenant_id",
	},
	{
		Name:   "ADMiner: Azure Groups",
		Cypher: "MATCH (n:AZGroup) RETURN n.tenantid AS tenant_id, n.name AS Name, COALESCE(n.description, '-') AS Description",
	},
	{
		Name:   "ADMiner: Azure VMs",
		Cypher: "MATCH (n:AZVM) RETURN n.tenantid AS tenant_id, n.name AS Name, n.operatingsystem AS os",
	},
	{
		Name:   "ADMiner: Azure Apps",
		Cypher: "MATCH (n:AZApp) WHERE n.name IS NOT NULL AND SIZE(n.name) > 1 RETURN n.tenantid AS tenant_id, n.name AS Name",
	},
	{
		Name:   "ADMiner: Azure Devices",
		Cypher: "MATCH (n:AZDevice) RETURN n.tenantid AS tenant_id, n.name AS Name, n.operatingsystem AS os",
	},
	{
		Name:   "ADMiner: Azure MS Graph controllers",
		Cypher: "MATCH p = (n)-[r:AZAddOwner|AZAddSecret|AZAppAdmin|AZCloudAppAdmin|AZMGAddOwner|AZMGAddSecret|AZOwns]->(g:AZServicePrincipal {appdisplayname: 'Microsoft Graph'}) RETURN p",
	},
	{
		Name:   "ADMiner: Azure admins also on premise",
		Cypher: "MATCH (u:User{is_da:true})-[:SyncedToEntraUser]->(a:AZUser)-[r:AZGlobalAdmin]->() RETURN u.name as Name",
	},
	{
		Name:   "ADMiner: Azure role listing",
		Cypher: "MATCH (a:AZRole) return distinct a.name AS Name, a.description AS Description",
	},
	{
		Name:   "ADMiner: Azure role paths",
		Cypher: "MATCH p=(a:AZUser)-[r:AZHasRole]->(x) return distinct p",
	},
	{
		Name: "ADMiner: Azure accounts disabled on premise",
		Cypher: "MATCH (a:AZUser{enabled:TRUE})-[:SyncedToADUser]->(u:User{enabled:FALSE}) " +
			"RETURN a.name AS azure_name, a.enabled AS enabled_on_azure, u.name AS onprem_name, u.enabled AS enabled_on_premise " +
			"UNION " +
			"MATCH (a:AZUser{enabled:FALSE})-[:SyncedToADUser]->(u:User{enabled:TRUE}) " +
			"RETURN a.name AS azure_name, a.enabled AS enabled_on_azure, u.name AS onprem_name, u.enabled AS enabled_on_premise",
	},
	{
		Name:   "ADMiner: Azure tenants",
		Cypher: "MATCH (t:AZTenant) RETURN t.name AS Name, t.tenantid AS ID",
	},
	{
		Name:   "ADMiner: AADConnect users",
		Cypher: "MATCH (u) WHERE (u:User OR u:AZUser) AND (u.name =~ '(?i)^MSOL_|.*AADConnect.*' OR u.userprincipalname =~ '(?i)^sync_.*') OPTIONAL MATCH (u)-[:HasSession]->(s:Session) RETURN u.name AS Name, s AS Session, u.tenantid AS `Tenant ID`",
	},
	{
		Name:   "ADMiner: Azure accounts not found on premise",
		Cypher: "MATCH (azUser:AZUser{onpremisesyncenabled:true}) WHERE NOT EXISTS {MATCH (user:User) WHERE user.objectid = azUser.onpremisesecurityidentifier} RETURN azUser.name AS Name",
	},
}

// TestCompareADMiner loads AD sample data into both kglite and Neo4j, runs
// analysis, then compares AD_Miner read-only queries side-by-side.
func TestCompareADMiner(t *testing.T) {
	ctx := context.Background()
	adZip := filepath.Join(testdataDir(), "ad_sampledata.zip")
	skipIfMissing(t, adZip)

	kgliteDB := openGraph(t)
	neo4jDB := openNeo4j(t)

	// Prepare Neo4j
	clearNeo4j(ctx, t, neo4jDB)
	require.NoError(t, neo4jDB.AssertSchema(ctx, schema.DefaultGraphSchema()))

	ingestSchema := loadIngestSchema(t)

	// Ingest into both
	t.Log("=== Ingesting AD data into kglite ===")
	kIngestDur := ingestZip(ctx, t, kgliteDB, adZip, ingestSchema)
	t.Logf("  kglite ingest: %s", kIngestDur.Round(time.Millisecond))

	t.Log("=== Ingesting AD data into Neo4j ===")
	nIngestDur := ingestZip(ctx, t, neo4jDB, adZip, ingestSchema)
	t.Logf("  Neo4j ingest: %s", nIngestDur.Round(time.Millisecond))

	// Analysis on both
	t.Log("=== Running analysis on kglite ===")
	kAnalysisDur := runAnalysis(ctx, t, kgliteDB)
	t.Log("=== Running analysis on Neo4j ===")
	nAnalysisDur := runAnalysis(ctx, t, neo4jDB)

	// Performance summary
	t.Log("")
	t.Log("=== Performance Summary: AD (ADMiner) ===")
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

	// Compare AD_Miner AD queries
	t.Log("=== Comparing AD_Miner AD queries ===")
	results := compareQueries(ctx, t, kgliteDB, neo4jDB, adminerADQueries)
	requireComparisonPass(t, results)
}

// TestCompareADMinerAzure loads Azure sample data into both kglite and Neo4j,
// runs analysis, then compares AD_Miner Azure-focused queries side-by-side.
func TestCompareADMinerAzure(t *testing.T) {
	ctx := context.Background()
	azureZip := filepath.Join(testdataDir(), "entra_sampledata.zip")
	skipIfMissing(t, azureZip)

	kgliteDB := openGraph(t)
	neo4jDB := openNeo4j(t)

	clearNeo4j(ctx, t, neo4jDB)
	require.NoError(t, neo4jDB.AssertSchema(ctx, schema.DefaultGraphSchema()))

	ingestSchema := loadIngestSchema(t)

	t.Log("=== Ingesting Azure data into kglite ===")
	kIngestDur := ingestZip(ctx, t, kgliteDB, azureZip, ingestSchema)
	t.Logf("  kglite ingest: %s", kIngestDur.Round(time.Millisecond))

	t.Log("=== Ingesting Azure data into Neo4j ===")
	nIngestDur := ingestZip(ctx, t, neo4jDB, azureZip, ingestSchema)
	t.Logf("  Neo4j ingest: %s", nIngestDur.Round(time.Millisecond))

	// Debug: check label matching before analysis
	debugResult1, _, _ := runQueryValues(ctx, t, kgliteDB, "MATCH (n:AZUser) RETURN count(n)")
	debugResult2, _, _ := runQueryValues(ctx, t, kgliteDB, "MATCH (n:AZBase) RETURN count(n)")
	debugResult3, _, _ := runQueryValues(ctx, t, kgliteDB, "MATCH (n:AZBase) WHERE n.__kinds IS NOT NULL RETURN count(n)")
	debugResult4, _, _ := runQueryValues(ctx, t, kgliteDB, "MATCH (n) WHERE n.__kinds IS NOT NULL RETURN count(n)")
	debugResult5, _, _ := runQueryValues(ctx, t, kgliteDB, "MATCH (n:AZUser) RETURN n.name, n.objectid LIMIT 3")
	t.Logf("DEBUG AZUser count: %s", debugResult1)
	t.Logf("DEBUG AZBase count: %s", debugResult2)
	t.Logf("DEBUG AZBase with __kinds: %s", debugResult3)
	t.Logf("DEBUG all with __kinds: %s", debugResult4)
	t.Logf("DEBUG AZUser sample: %s", debugResult5)

	t.Log("=== Running analysis on kglite ===")
	kAnalysisDur := runAnalysis(ctx, t, kgliteDB)
	t.Log("=== Running analysis on Neo4j ===")
	nAnalysisDur := runAnalysis(ctx, t, neo4jDB)

	// Performance summary
	t.Log("")
	t.Log("=== Performance Summary: Azure (ADMiner) ===")
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

	// Compare AD_Miner Azure queries
	t.Log("=== Comparing AD_Miner Azure queries ===")
	results := compareQueries(ctx, t, kgliteDB, neo4jDB, adminerAzureQueries)
	requireComparisonPass(t, results)
}

// TestCompareADMinerCombined loads both AD and Azure sample data, then runs
// all AD_Miner queries against the combined dataset. This tests queries that
// span both AD and Azure node types (e.g., azure_admin_on_prem).
func TestCompareADMinerCombined(t *testing.T) {
	ctx := context.Background()
	adZip := filepath.Join(testdataDir(), "ad_sampledata.zip")
	azureZip := filepath.Join(testdataDir(), "entra_sampledata.zip")
	skipIfMissing(t, adZip)
	skipIfMissing(t, azureZip)

	kgliteDB := openGraph(t)
	neo4jDB := openNeo4j(t)

	clearNeo4j(ctx, t, neo4jDB)
	require.NoError(t, neo4jDB.AssertSchema(ctx, schema.DefaultGraphSchema()))

	ingestSchema := loadIngestSchema(t)

	// Ingest AD data
	t.Log("=== Ingesting AD data into kglite ===")
	ingestZip(ctx, t, kgliteDB, adZip, ingestSchema)
	t.Log("=== Ingesting AD data into Neo4j ===")
	ingestZip(ctx, t, neo4jDB, adZip, ingestSchema)

	// Ingest Azure data
	t.Log("=== Ingesting Azure data into kglite ===")
	ingestZip(ctx, t, kgliteDB, azureZip, ingestSchema)
	t.Log("=== Ingesting Azure data into Neo4j ===")
	ingestZip(ctx, t, neo4jDB, azureZip, ingestSchema)

	// Analysis on both
	t.Log("=== Running analysis on kglite ===")
	runAnalysis(ctx, t, kgliteDB)
	t.Log("=== Running analysis on Neo4j ===")
	runAnalysis(ctx, t, neo4jDB)

	allQueries := make([]presetQuery, 0, len(adminerADQueries)+len(adminerAzureQueries))
	allQueries = append(allQueries, adminerADQueries...)
	allQueries = append(allQueries, adminerAzureQueries...)

	t.Logf("=== Comparing all AD_Miner queries (%d total) ===", len(allQueries))
	results := compareQueries(ctx, t, kgliteDB, neo4jDB, allQueries)
	requireComparisonPass(t, results)

	// Summary
	var matches, mismatches, errors int
	for _, r := range results {
		switch {
		case r.KgliteErr != nil || r.Neo4jErr != nil:
			errors++
		case !r.Match:
			mismatches++
		default:
			matches++
		}
	}
	t.Logf("")
	t.Logf("AD_Miner query comparison: %d match, %d mismatch, %d error (out of %d)",
		matches, mismatches, errors, len(results))

	if mismatches > 0 {
		t.Logf("")
		t.Logf("=== Mismatched queries detail ===")
		for _, r := range results {
			if !r.Match && r.KgliteErr == nil && r.Neo4jErr == nil {
				t.Logf("")
				t.Logf("MISMATCH: %s", r.QueryName)
				t.Logf("  Cypher: %s", truncateStr(r.Cypher, 120))
				t.Logf("  kglite: %s", truncateStr(r.KgliteResult, 200))
				t.Logf("  Neo4j:  %s", truncateStr(r.Neo4jResult, 200))
			}
		}
	}

	if errors > 0 {
		t.Logf("")
		t.Logf("=== Errored queries detail ===")
		for _, r := range results {
			if r.KgliteErr != nil || r.Neo4jErr != nil {
				t.Logf("")
				t.Logf("ERROR: %s", r.QueryName)
				t.Logf("  Cypher: %s", truncateStr(r.Cypher, 120))
				if r.KgliteErr != nil {
					t.Logf("  kglite err: %s", r.KgliteErr)
				}
				if r.Neo4jErr != nil {
					t.Logf("  Neo4j err:  %s", r.Neo4jErr)
				}
			}
		}
	}
}

func init() {
	// Validate that all queries compile (catch typos in the query slices).
	for _, q := range adminerADQueries {
		if q.Name == "" || q.Cypher == "" {
			panic(fmt.Sprintf("adminerADQueries has empty entry: %+v", q))
		}
	}
	for _, q := range adminerAzureQueries {
		if q.Name == "" || q.Cypher == "" {
			panic(fmt.Sprintf("adminerAzureQueries has empty entry: %+v", q))
		}
	}
}
