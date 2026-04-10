//go:build e2e

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

// K-Nexus Global end-to-end tests (kglite only, no Neo4j required).
//
// These tests load the k-nexusglobal multi-platform dataset into kglite,
// run analysis, and verify query results.

import (
	"context"
	"testing"
)

// TestLoadAndQueryKNexus uses the shared pre-loaded k-nexus-global graph and
// runs preset queries covering AD, Azure, and cross-platform node types.
func TestLoadAndQueryKNexus(t *testing.T) {
	ctx := context.Background()
	db := sharedKNexusGraph(t)

	mem := []memSnapshot{takeMemSnapshot("pre-loaded")}

	t.Log("=== Preset Cypher queries (data pre-loaded) ===")
	runPresetQueries(ctx, t, db, knexusQueries)
	mem = append(mem, takeMemSnapshot("after queries"))

	logMemTable(t, mem)
}

// knexusPresetQueries is defined in knexus_comparison_test.go (build tag: comparison && e2e).
// Redeclare the query slice here for the non-comparison build so the kglite-only
// test can run without the comparison tag.

// knexusQueries are the preset queries for kglite-only k-nexus-global tests.
var knexusQueries = []presetQuery{
	// Overall counts
	{Name: "Total nodes", Cypher: `MATCH (n) RETURN count(n) AS nodes`},
	{Name: "Total relationships", Cypher: `MATCH ()-[r]->() RETURN count(r) AS relationships`},

	// AD counts
	{Name: "Domains", Cypher: `MATCH (n:Domain) RETURN n.name AS domain, n.objectid AS sid ORDER BY domain`},
	{Name: "Computers", Cypher: `MATCH (n:Computer) RETURN count(n) AS computers`},
	{Name: "Users", Cypher: `MATCH (n:User) RETURN count(n) AS users`},
	{Name: "Groups", Cypher: `MATCH (n:Group) RETURN count(n) AS groups`},
	{Name: "OUs", Cypher: `MATCH (n:OU) RETURN count(n) AS ous`},
	{Name: "GPOs", Cypher: `MATCH (n:GPO) RETURN count(n) AS gpos`},
	{Name: "ADCS cert templates", Cypher: `MATCH (n:CertTemplate) RETURN count(n) AS cert_templates`},
	{Name: "Enterprise CAs", Cypher: `MATCH (n:EnterpriseCA) RETURN count(n) AS enterprise_cas`},

	// AD security queries
	{Name: "Kerberoastable users", Cypher: `MATCH (u:User) WHERE u.hasspn = true AND u.enabled = true RETURN count(u) AS kerberoastable`},
	{Name: "AS-REP roastable users", Cypher: `MATCH (u:User) WHERE u.dontreqpreauth = true AND u.enabled = true RETURN count(u) AS asrep_roastable`},
	{Name: "AdminCount users", Cypher: `MATCH (u:User) WHERE u.admincount = true RETURN count(u) AS admin_count_users`},
	{Name: "Unconstrained delegation computers", Cypher: `MATCH (c:Computer) WHERE c.unconstraineddelegation = true RETURN count(c) AS unconstrained`},
	{Name: "Enabled domain admin users", Cypher: `MATCH (u:User)-[:MemberOf*1..]->(g:Group) WHERE g.objectid ENDS WITH '-512' AND u.enabled = true RETURN count(DISTINCT u) AS domain_admins`},

	// AD relationships
	{Name: "DCSync relationships", Cypher: `MATCH ()-[r:DCSync]->() RETURN count(r) AS dcsync`},
	{Name: "HasSession relationships", Cypher: `MATCH ()-[r:HasSession]->() RETURN count(r) AS sessions`},
	{Name: "AdminTo relationships", Cypher: `MATCH ()-[r:AdminTo]->() RETURN count(r) AS admin_tos`},
	{Name: "MemberOf relationships", Cypher: `MATCH ()-[r:MemberOf]->() RETURN count(r) AS member_ofs`},

	// Azure counts
	{Name: "Azure tenants", Cypher: `MATCH (n:AZTenant) RETURN count(n) AS tenants`},
	{Name: "Azure users", Cypher: `MATCH (n:AZUser) RETURN count(n) AS az_users`},
	{Name: "Azure groups", Cypher: `MATCH (n:AZGroup) RETURN count(n) AS az_groups`},
	{Name: "Azure apps", Cypher: `MATCH (n:AZApp) RETURN count(n) AS apps`},
	{Name: "Azure service principals", Cypher: `MATCH (n:AZServicePrincipal) RETURN count(n) AS service_principals`},
	{Name: "Azure VMs", Cypher: `MATCH (n:AZVM) RETURN count(n) AS vms`},

	// Azure relationships
	{Name: "AZGlobalAdmin relationships", Cypher: `MATCH ()-[r:AZGlobalAdmin]->() RETURN count(r) AS global_admins`},
	{Name: "AZOwns relationships", Cypher: `MATCH ()-[r:AZOwns]->() RETURN count(r) AS az_owns`},
	{Name: "AZMemberOf relationships", Cypher: `MATCH ()-[r:AZMemberOf]->() RETURN count(r) AS az_member_ofs`},

	// Cross-domain
	{Name: "Domain trusts", Cypher: `MATCH ()-[r:TrustedBy]->() RETURN count(r) AS trusts`},
	{Name: "Distinct relationship types", Cypher: `MATCH ()-[r]->() RETURN DISTINCT type(r) AS relationType ORDER BY relationType`},

	// Dedup invariant: no objectid should appear on more than one node
	{Name: "Duplicate objectids", Cypher: `MATCH (n) WHERE n.objectid IS NOT NULL WITH n.objectid AS oid, count(n) AS cnt WHERE cnt > 1 RETURN count(oid) AS duplicates`},
}
