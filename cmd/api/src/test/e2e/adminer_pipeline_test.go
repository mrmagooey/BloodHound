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

// AD_Miner full-pipeline parity test.
//
// Unlike adminer_comparison_test.go (which compares only read-only Cypher),
// this test executes the entire upstream AD_Miner Cypher pipeline — including
// every mutating SET / MERGE / DELETE / CREATE — against both kglite and
// Neo4j, then asserts that downstream reads match. Each mutating step is
// followed by a "verify" read query that probes the property the SET wrote;
// the two engines' verify results are diffed.
//
// Source: github.com/AD-Security/AD_Miner ad_miner/sources/modules/requests.json
//
// Steps are emitted in JSON-insertion order, with placeholders substituted
// per the brief:
//
//   $extract_date$       -> 1714435200 (2024-04-30 UTC)
//   $password_renewal$   -> 90
//   $properties$         -> 37-edge OR-list (see edgeListProperties)
//   $path_to_group_operators_props$ -> same as $properties$
//   $inbound_control_edges$         -> 13-edge OR-list
//   $recursive_level$    -> 5
//   "SKIP PARAM1 LIMIT PARAM2"      -> dropped
//
// extract_date justification: the AD fixture's enabled-computer
// lastlogontimestamp values span 2023-03-07 through 2024-03-23. The
// set_ghost_computer threshold is (extract_date - lastlogontimestamp)/86400
// > 90. With extract_date=1714435200 (2024-04-30 00:00 UTC), the fixture's
// 19 enabled-with-timestamp computers split 5 ghost / 14 non-ghost — a
// non-trivial result that exercises the SET predicate without trivial
// universality.
//
// Drop list (per architecture decision):
//
//   - check_if_GDS_installed       (GDS-only)
//   - check_unknown_relations      (postProcess sets r.cost for GDS)
//   - set_default_exploitability_rating (GDS-only)
//   - template                     (placeholder entry)
//
// The Python-side postProcess setDangerousInboundOnGPOs has no pure-Cypher
// equivalent in requests.json and is NOT reconstructed; the four downstream
// unpriv_users_to_GPO_*_enforced reads will return empty-equals-empty (parity
// pass) on both engines.
//
// For multi-pass fixed-point chains (set_groups_indirect_admin_1..4), all
// passes execute back-to-back on each engine before any verify runs.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	schema "github.com/specterops/bloodhound/packages/go/graphschema"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

// pipelineStep is one step of the AD_Miner pipeline. For writes, Verify is a
// follow-up read query whose results are compared between engines after the
// write executes.
type pipelineStep struct {
	Name    string
	Cypher  string
	IsWrite bool
	// Verify is a read query that probes the property the SET mutated.
	// Compared between engines (sorted, normalized) after the write.
	// Empty for read steps (the step itself is the comparison).
	Verify string
}

// adminerPipelineSteps are the AD_Miner pipeline operations in upstream
// requests.json insertion order, with placeholders substituted.
var adminerPipelineSteps = []pipelineStep{
	// --- preparation / cleanup ---
	{
		Name:    "delete_orphans",
		Cypher:  `MATCH (n) WHERE labels(n) = ["Base"] OR labels(n) = ["AZBase"] OR labels(n) = ["Base", "AZBase"] DETACH DELETE n`,
		IsWrite: true,
		Verify:  `MATCH (n) RETURN count(n) AS c`,
	},
	{
		Name:    "preparation_request_nodes",
		Cypher:  `MATCH (n) REMOVE n.is_server,n.is_dc,n.is_da,n.is_dag,n.can_dcsync,n.path_candidate,n.ou_candidate,n.contains_da_dc,n.is_da_dc,n.ghost_computer,n.has_path_to_da,n.is_admin,n.is_group_operator,n.members_count,n.has_members,n.user_members_count,n.is_operator_member,n.is_group_account_operator,n.is_group_backup_operator,n.is_group_server_operator,n.is_group_print_operator,n.is_account_operator,n.is_backup_operator,n.is_server_operator,n.is_print_operator,n.gpolinks_count,n.has_links,n.dangerous_inbound, n.is_adminsdholder,n.is_dnsadmin,n.da_types,n.vulnerable_ou,n.can_abuse_adcs,n.dac,n.dac_types,n.is_adcs,n.target_kud,n.is_gag,n.is_msol,n.is_rbcd_target,n.is_dcg,n.esc7`,
		IsWrite: true,
		Verify:  `MATCH (n) WHERE n.is_server IS NOT NULL OR n.is_dc IS NOT NULL OR n.is_da IS NOT NULL OR n.path_candidate IS NOT NULL RETURN count(n) AS c`,
	},
	{
		Name:    "delete_unresolved",
		Cypher:  `MATCH (n) WHERE ((n.domain IS NULL AND NOT (n:Domain)) OR n.name IS NULL) AND n.tenantid IS NULL DETACH DELETE n`,
		IsWrite: true,
		Verify:  `MATCH (n) RETURN count(n) AS c`,
	},
	{
		Name:   "check_relation_types",
		Cypher: `MATCH ()-[r]->() RETURN DISTINCT type(r) as relationType`,
	},
	{
		Name:    "set_upper_domain_name",
		Cypher:  `MATCH (g) where g.domain <> toUpper(g.domain) SET g.domain=toUpper(g.domain)`,
		IsWrite: true,
		Verify:  `MATCH (g) WHERE g.domain IS NOT NULL AND g.domain <> toUpper(g.domain) RETURN count(g) AS c`,
	},
	{
		Name:    "set_domain_attributes_to_domains",
		Cypher:  `MATCH (d:Domain) where d.domain IS NULL SET d.domain = toUpper(d.name)`,
		IsWrite: true,
		Verify:  `MATCH (d:Domain) WHERE d.domain IS NULL RETURN count(d) AS c`,
	},
	{
		Name:    "check_if_all_domain_objects_exist",
		Cypher:  `MATCH (d:Domain) WITH DISTINCT d.domain AS domain WITH COLLECT(domain) AS domains MATCH (o) WHERE NOT o.domain IN domains RETURN count(o)`,
		IsWrite: true, // upstream marks write but contains only RETURN; treat as write to honor metadata.
		Verify:  `MATCH (d:Domain) WITH DISTINCT d.domain AS domain WITH COLLECT(domain) AS domains MATCH (o) WHERE NOT o.domain IN domains RETURN count(o) AS c`,
	},
	{
		Name:    "check_if_all_group_objects_have_domain_attribute",
		Cypher:  `MATCH (g:Group) WHERE g.domain <> split(g.name, "@")[-1] SET g.domain=split(g.name, "@")[-1]`,
		IsWrite: true,
		Verify:  `MATCH (g:Group) WHERE g.name IS NOT NULL AND g.domain <> split(g.name, "@")[-1] RETURN count(g) AS c`,
	},
	{
		Name:    "preparation_request_relations",
		Cypher:  `MATCH (g:Group)-[r:CanExtractDCSecrets|CanLoadCode|CanLogOnLocallyOnDC]->(c:Computer) DELETE r`,
		IsWrite: true,
		Verify:  `MATCH ()-[r:CanExtractDCSecrets|CanLoadCode|CanLogOnLocallyOnDC]->() RETURN count(r) AS c`,
	},
	{
		Name:    "set_server",
		Cypher:  `MATCH (c:Computer)  WHERE toUpper(c.operatingsystem) CONTAINS "SERVER" SET c.is_server=TRUE`,
		IsWrite: true,
		Verify:  `MATCH (c:Computer) WHERE c.is_server=true RETURN c.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_non_server",
		Cypher:  `MATCH (c:Computer) WHERE c.is_server IS NULL  SET c.is_server=FALSE`,
		IsWrite: true,
		Verify:  `MATCH (c:Computer) WHERE c.is_server IS NULL RETURN count(c) AS c`,
	},
	{
		Name:    "set_dc",
		Cypher:  `MATCH (c:Computer)-[:MemberOf*1..3]->(g:Group) WHERE g.objectid ENDS WITH "-516" OR g.objectid ENDS WITH "-521" SET c.is_dc=TRUE`,
		IsWrite: true,
		Verify:  `MATCH (c:Computer) WHERE c.is_dc=true RETURN c.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_nondc",
		Cypher:  `MATCH (c:Computer) WHERE c.is_dc IS NULL SET c.is_dc=FALSE`,
		IsWrite: true,
		Verify:  `MATCH (c:Computer) WHERE c.is_dc IS NULL RETURN count(c) AS c`,
	},
	{
		Name:    "set_dcg",
		Cypher:  `MATCH (g:Group) WHERE g.objectid ENDS WITH "-516" OR g.objectid ENDS WITH "-521" SET g.is_dcg=TRUE`,
		IsWrite: true,
		Verify:  `MATCH (g:Group) WHERE g.is_dcg=true RETURN g.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_nondcg",
		Cypher:  `MATCH (g:Group) WHERE g.is_dcg IS NULL SET g.is_dcg=FALSE`,
		IsWrite: true,
		Verify:  `MATCH (g:Group) WHERE g.is_dcg IS NULL RETURN count(g) AS c`,
	},
	{
		Name:    "set_isacl_adcs",
		Cypher:  `MATCH (u)-[r]->(g) WHERE r.isacl IS NULL AND type(r) CONTAINS 'ADCSESC' SET r.isacl=TRUE`,
		IsWrite: true,
		Verify:  `MATCH ()-[r]->() WHERE r.isacl IS NULL AND type(r) CONTAINS 'ADCSESC' RETURN count(r) AS c`,
	},
	{
		Name:    "onpremid_ompremsesecurityidentifier",
		Cypher:  `MATCH (a)  WHERE NOT a.onpremisesecurityidentifier IS NULL set a.onpremid=a.onpremisesecurityidentifier`,
		IsWrite: true,
		Verify:  `MATCH (a) WHERE a.onpremisesecurityidentifier IS NOT NULL AND (a.onpremid IS NULL OR a.onpremid <> a.onpremisesecurityidentifier) RETURN count(a) AS c`,
	},
	{
		Name:    "set_can_extract_dc_secrets",
		Cypher:  `MATCH (g:Group) WHERE g.objectid ENDS WITH "-551" OR g.objectid ENDS WITH "-549" MATCH (c:Computer{is_dc:true}) WHERE g.domain = c.domain MERGE (g)-[:CanExtractDCSecrets]->(c)`,
		IsWrite: true,
		Verify:  `MATCH ()-[r:CanExtractDCSecrets]->() RETURN count(r) AS c`,
	},
	{
		Name:    "set_is_adminsdholder",
		Cypher:  `MATCH (c:Container) WHERE c.name STARTS WITH "ADMINSDHOLDER@" SET c.is_adminsdholder=true`,
		IsWrite: true,
		Verify:  `MATCH (c:Container) WHERE c.is_adminsdholder=true RETURN c.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_is_dnsadmin",
		Cypher:  `MATCH (g:Group) WHERE g.name STARTS WITH "DNSADMINS@" SET g.is_dnsadmin=true`,
		IsWrite: true,
		Verify:  `MATCH (g:Group) WHERE g.is_dnsadmin=true RETURN g.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_can_load_code",
		Cypher:  `MATCH (g:Group) WHERE g.objectid ENDS WITH "-550" MATCH (c:Computer{is_dc:true}) WHERE g.domain = c.domain MERGE (g)-[:CanLoadCode]->(c)`,
		IsWrite: true,
		Verify:  `MATCH ()-[r:CanLoadCode]->() RETURN count(r) AS c`,
	},
	{
		Name:    "set_can_logon_dc",
		Cypher:  `MATCH (g:Group) WHERE g.objectid ENDS WITH "-548" MATCH (c:Computer{is_dc:true}) WHERE g.domain = c.domain MERGE (g)-[:CanLogOnLocallyOnDC]->(c)`,
		IsWrite: true,
		Verify:  `MATCH ()-[r:CanLogOnLocallyOnDC]->() RETURN count(r) AS c`,
	},
	{
		Name:    "set_da",
		Cypher:  `MATCH (c:User)-[:MemberOf*1..3]->(g:Group) WHERE g.objectid ENDS WITH "-512" OR g.objectid ENDS WITH "-518" OR g.objectid ENDS WITH "-519" OR g.objectid ENDS WITH "-526" OR g.objectid ENDS WITH "-527" OR g.objectid ENDS WITH "-544" SET c.is_da=TRUE, c.da_types=[]`,
		IsWrite: true,
		Verify:  `MATCH (c:User) WHERE c.is_da=true RETURN c.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_msol",
		Cypher:  `MATCH (c:User) where c.name STARTS WITH 'MSOL_' SET c.is_da=TRUE, c.is_msol=true`,
		IsWrite: true,
		Verify:  `MATCH (c:User) WHERE c.is_msol=true RETURN c.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_da_types",
		Cypher:  `MATCH (c:User)-[:MemberOf*1..3]->(g:Group) WHERE g.objectid ENDS WITH "-512" OR g.objectid ENDS WITH "-518" OR g.objectid ENDS WITH "-519" OR g.objectid ENDS WITH "-525" OR g.objectid ENDS WITH "-526" OR g.objectid ENDS WITH "-527" OR g.objectid ENDS WITH "-544" WITH c,g, CASE WHEN g.objectid ENDS WITH "-512" THEN "Domain Admin" WHEN g.objectid ENDS WITH "-518" THEN "Schema Admin" WHEN g.objectid ENDS WITH "-519" THEN "Enterprise Admin" WHEN g.objectid ENDS WITH "-525" THEN "Protected Users" WHEN g.objectid ENDS WITH "-526" THEN "_ Key Admin" WHEN g.objectid ENDS WITH "-527" THEN "Enterprise Key Admin" WHEN g.objectid ENDS WITH "-544" THEN "Builtin Administrator" ELSE null END AS da_type SET c.da_types = c.da_types + da_type`,
		IsWrite: true,
		Verify:  `MATCH (c:User) WHERE c.da_types IS NOT NULL AND size(c.da_types) > 0 RETURN c.objectid AS o, size(c.da_types) AS n ORDER BY o`,
	},
	{
		Name:    "set_dag",
		Cypher:  `MATCH (c:Group)-[:MemberOf*1..3]->(g:Group) WHERE g.objectid ENDS WITH "-512" OR g.objectid ENDS WITH "-518" OR g.objectid ENDS WITH "-519" OR g.objectid ENDS WITH "-526" OR g.objectid ENDS WITH "-527" OR g.objectid ENDS WITH "-544" SET c.is_da=TRUE`,
		IsWrite: true,
		Verify:  `MATCH (c:Group) WHERE c.is_da=true RETURN c.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_dag_types",
		Cypher:  `MATCH (c:Group)-[:MemberOf*1..3]->(g:Group) WHERE g.objectid ENDS WITH "-512" OR g.objectid ENDS WITH "-518" OR g.objectid ENDS WITH "-519" OR g.objectid ENDS WITH "-525" OR g.objectid ENDS WITH "-526" OR g.objectid ENDS WITH "-527" OR g.objectid ENDS WITH "-544" WITH c,g, CASE WHEN g.objectid ENDS WITH "-512" THEN "Domain Admin" WHEN g.objectid ENDS WITH "-518" THEN "Schema Admin" WHEN g.objectid ENDS WITH "-519" THEN "Enterprise Admin" WHEN g.objectid ENDS WITH "-525" THEN "Protected Users" WHEN g.objectid ENDS WITH "-526" THEN "_ Key Admin" WHEN g.objectid ENDS WITH "-527" THEN "Enterprise Key Admin" WHEN g.objectid ENDS WITH "-544" THEN "Builtin Administrator" ELSE null END AS da_type SET c.da_types = c.da_types + da_type`,
		IsWrite: true,
		Verify:  `MATCH (c:Group) WHERE c.da_types IS NOT NULL AND size(c.da_types) > 0 RETURN c.objectid AS o, size(c.da_types) AS n ORDER BY o`,
	},
	{
		Name:    "set_dagg",
		Cypher:  `MATCH (g:Group) WHERE g.objectid ENDS WITH "-512" OR g.objectid ENDS WITH "-518" OR g.objectid ENDS WITH "-519" OR g.objectid ENDS WITH "-526" OR g.objectid ENDS WITH "-527" OR g.objectid ENDS WITH "-544" SET g.is_da=TRUE`,
		IsWrite: true,
		Verify:  `MATCH (g:Group) WHERE g.is_da=true RETURN g.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_dagg_types",
		Cypher:  `MATCH (g:Group) WHERE g.objectid ENDS WITH "-512" OR g.objectid ENDS WITH "-518" OR g.objectid ENDS WITH "-519" OR g.objectid ENDS WITH "-525" OR g.objectid ENDS WITH "-526" OR g.objectid ENDS WITH "-527" OR g.objectid ENDS WITH "-544" WITH g, CASE WHEN g.objectid ENDS WITH "-512" THEN "Domain Admin" WHEN g.objectid ENDS WITH "-518" THEN "Schema Admin" WHEN g.objectid ENDS WITH "-519" THEN "Enterprise Admin" WHEN g.objectid ENDS WITH "-525" THEN "Protected Users" WHEN g.objectid ENDS WITH "-526" THEN "_ Key Admin" WHEN g.objectid ENDS WITH "-527" THEN "Enterprise Key Admin" WHEN g.objectid ENDS WITH "-544" THEN "Builtin Administrator" ELSE null END AS da_type SET g.da_types = g.da_types + da_type`,
		IsWrite: true,
		Verify:  `MATCH (g:Group) WHERE g.da_types IS NOT NULL AND size(g.da_types) > 0 RETURN g.objectid AS o, size(g.da_types) AS n ORDER BY o`,
	},
	{
		Name:    "set_daggg",
		Cypher:  `MATCH (g:Group) WHERE g.objectid ENDS WITH "-512"  SET g.is_dag=TRUE`,
		IsWrite: true,
		Verify:  `MATCH (g:Group) WHERE g.is_dag=true RETURN g.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_dac",
		Cypher:  `MATCH (c:Computer{is_dc:False})-[:MemberOf*1..3]->(g:Group) WHERE g.objectid ENDS WITH "-512" OR g.objectid ENDS WITH "-518" OR g.objectid ENDS WITH "-519" OR g.objectid ENDS WITH "-526" OR g.objectid ENDS WITH "-527" OR g.objectid ENDS WITH "-544" SET c.is_dac=TRUE, c.dac_types=[]`,
		IsWrite: true,
		Verify:  `MATCH (c:Computer) WHERE c.is_dac=true RETURN c.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_dac_types",
		Cypher:  `MATCH (c:Computer)-[:MemberOf*1..3]->(g:Group) WHERE g.objectid ENDS WITH "-512" OR g.objectid ENDS WITH "-518" OR g.objectid ENDS WITH "-519" OR g.objectid ENDS WITH "-525" OR g.objectid ENDS WITH "-526" OR g.objectid ENDS WITH "-527" OR g.objectid ENDS WITH "-544" WITH c,g, CASE WHEN g.objectid ENDS WITH "-512" THEN "Domain Admin" WHEN g.objectid ENDS WITH "-518" THEN "Schema Admin" WHEN g.objectid ENDS WITH "-519" THEN "Enterprise Admin" WHEN g.objectid ENDS WITH "-525" THEN "Protected Users" WHEN g.objectid ENDS WITH "-526" THEN "_ Key Admin" WHEN g.objectid ENDS WITH "-527" THEN "Enterprise Key Admin" WHEN g.objectid ENDS WITH "-544" THEN "Builtin Administrator" ELSE null END AS da_type SET c.da_types = c.da_types + da_type`,
		IsWrite: true,
		Verify:  `MATCH (c:Computer) WHERE c.da_types IS NOT NULL AND size(c.da_types) > 0 RETURN c.objectid AS o, size(c.da_types) AS n ORDER BY o`,
	},
	{
		Name:    "set_nonda",
		Cypher:  `MATCH (c) WHERE c.is_da IS NULL SET c.is_da=FALSE`,
		IsWrite: true,
		Verify:  `MATCH (c) WHERE c.is_da IS NULL RETURN count(c) AS c`,
	},
	{
		Name:    "set_nondag",
		Cypher:  `MATCH (g) WHERE g.is_dag IS NULL SET g.is_dag=FALSE`,
		IsWrite: true,
		Verify:  `MATCH (g) WHERE g.is_dag IS NULL RETURN count(g) AS c`,
	},
	{
		Name:    "set_is_group_operator",
		Cypher:  `MATCH (g:Group) WHERE g.objectid ENDS WITH "-551" OR g.objectid ENDS WITH "-549" OR g.objectid ENDS WITH "-548" OR g.objectid ENDS WITH "-550" SET g.is_group_operator=True SET g.is_group_account_operator = CASE WHEN g.objectid ENDS WITH "-548" THEN true END, g.is_group_backup_operator = CASE WHEN g.objectid ENDS WITH "-551" THEN true END, g.is_group_server_operator = CASE WHEN g.objectid ENDS WITH "-549" THEN true END, g.is_group_print_operator = CASE WHEN g.objectid ENDS WITH "-550" THEN true END`,
		IsWrite: true,
		Verify:  `MATCH (g:Group) WHERE g.is_group_operator=true RETURN g.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_is_operator_member",
		Cypher:  `MATCH (o:User)-[r:MemberOf*1..5]->(g:Group{is_group_operator:True}) WHERE o.is_da=false OR o.domain <> g.domain SET o.is_operator_member=true SET o.is_account_operator = CASE WHEN g.objectid ENDS WITH "-548" THEN true ELSE o.is_account_operator END, o.is_type_operator = CASE WHEN g.objectid ENDS WITH "-548" THEN "ACCOUNT OPERATOR" ELSE o.is_type_operator END, o.is_backup_operator = CASE WHEN g.objectid ENDS WITH "-551" THEN true ELSE o.is_backup_operator END, o.is_type_operator = CASE WHEN g.objectid ENDS WITH "-548" THEN "BACKUP OPERATOR" ELSE o.is_type_operator END, o.is_server_operator = CASE WHEN g.objectid ENDS WITH "-549" THEN true ELSE o.is_server_operator END, o.is_type_operator = CASE WHEN g.objectid ENDS WITH "-548" THEN "SERVER OPERATOR" ELSE o.is_type_operator END, o.is_print_o`,
		IsWrite: true,
		Verify:  `MATCH (o:User) WHERE o.is_operator_member=true RETURN o.objectid AS o ORDER BY o`,
	},
	// set_dcsync1 / set_dcsync2 — heavy variable-length path queries; engine
	// support varies for SKIP/LIMIT-stripped forms. Verify on the property.
	{
		Name:    "set_dcsync1",
		Cypher:  `MATCH (n1) WITH n1 ORDER BY ID(n1) MATCH p=allShortestPaths((n1)-[:MemberOf|GetChanges*1..5]->(u:Domain)) WHERE n1 <> u WITH n1 MATCH p2=(n1)-[:MemberOf|GetChangesAll*1..5]->(u:Domain) WHERE n1 <> u AND NOT n1.name IS NULL AND (((n1.is_da IS NULL OR n1.is_da=FALSE) AND (n1.is_dc IS NULL OR n1.is_dc=FALSE)) OR (NOT u.domain CONTAINS '.' + n1.domain AND n1.domain <> u.domain)) SET n1.can_dcsync=TRUE RETURN DISTINCT p2 as p`,
		IsWrite: true,
		Verify:  `MATCH (n) WHERE n.can_dcsync=true RETURN n.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_dcsync2",
		Cypher:  `MATCH (n2) WITH n2 ORDER BY ID(n2) MATCH p3=allShortestPaths((n2)-[:MemberOf|GenericAll|AllExtendedRights*1..5]->(u:Domain)) WHERE n2 <> u AND NOT n2.name IS NULL AND (((n2.is_da IS NULL OR n2.is_da=FALSE) AND (n2.is_dc IS NULL OR n2.is_dc=FALSE)) OR (NOT u.domain CONTAINS '.' + n2.domain AND n2.domain <> u.domain)) SET n2.can_dcsync=TRUE RETURN DISTINCT p3 as p`,
		IsWrite: true,
		Verify:  `MATCH (n) WHERE n.can_dcsync=true RETURN n.objectid AS o ORDER BY o`,
	},
	{
		Name:   "dcsync_list",
		Cypher: `MATCH (n{can_dcsync:true}) RETURN n.domain as domain, n.name as name`,
	},
	{
		Name:    "set_ou_candidate",
		Cypher:  `MATCH (m) WHERE NOT m.name IS NULL AND ((m:Computer AND m.enabled AND (m.is_dc=false OR m.is_dc IS NULL)) OR (m:User AND m.enabled AND (m.is_da=false OR m.is_da IS NULL))) SET m.ou_candidate=TRUE`,
		IsWrite: true,
		Verify:  `MATCH (m) WHERE m.ou_candidate=true RETURN m.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_containsda",
		Cypher:  `MATCH p=(o:OU)-[r:Contains*1..]->(x{is_da:true}) SET o.contains_da_dc=true RETURN p`,
		IsWrite: true,
		Verify:  `MATCH (o:OU) WHERE o.contains_da_dc=true RETURN o.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_containsdc",
		Cypher:  `MATCH p=(o:OU)-[r:Contains*1..]->(x{is_dc:true}) SET o.contains_da_dc=true RETURN p`,
		IsWrite: true,
		Verify:  `MATCH (o:OU) WHERE o.contains_da_dc=true RETURN o.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_is_da_dc",
		Cypher:  `MATCH (u) WHERE (u.is_da=true OR u.is_dc=true) SET u.is_da_dc=true`,
		IsWrite: true,
		Verify:  `MATCH (u) WHERE u.is_da_dc=true RETURN u.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_is_not_da_dc",
		Cypher:  `MATCH (o:Base) WHERE o.is_da_dc IS NULL SET o.is_da_dc = FALSE`,
		IsWrite: true,
		Verify:  `MATCH (o:Base) WHERE o.is_da_dc IS NULL RETURN count(o) AS c`,
	},
	{
		Name:    "set_is_adcs",
		Cypher:  `MATCH (g:Group) WHERE g.objectid ENDS WITH '-517' MATCH (c:Computer)-[r:MemberOf*1..4]->(g) SET c.is_adcs=TRUE RETURN c.domain AS domain, c.name AS name`,
		IsWrite: true,
		Verify:  `MATCH (c:Computer) WHERE c.is_adcs=true RETURN c.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_path_candidate",
		Cypher:  `MATCH (o{is_da_dc:false}) WHERE NOT o:Domain AND ((o.enabled=True AND o:User) OR NOT o:User) AND (NOT o.is_adcs OR o.is_adcs is null) SET o.path_candidate=TRUE`,
		IsWrite: true,
		Verify:  `MATCH (o) WHERE o.path_candidate=true RETURN o.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_groups_members_count",
		Cypher:  `MATCH  (g:Group) WITH g ORDER BY g.name MATCH (u:User)-[:MemberOf*1..5]->(g) WHERE NOT u.name IS NULL AND NOT g.name IS NULL WITH g AS g1, count(u) AS memberscount SET g1.members_count=memberscount`,
		IsWrite: true,
		Verify:  `MATCH (g:Group) WHERE g.members_count IS NOT NULL RETURN g.objectid AS o, g.members_count AS m ORDER BY o`,
	},
	{
		Name:    "set_groups_members_count_computers",
		Cypher:  `MATCH (g:Group) WITH g ORDER BY g.name MATCH (u:Computer)-[:MemberOf*1..5]->(g) WHERE NOT u.name IS NULL AND NOT g.name IS NULL WITH g AS g1, count(u) AS memberscount SET g1.members_count= COALESCE(g1.members_count, 0) + memberscount`,
		IsWrite: true,
		Verify:  `MATCH (g:Group) WHERE g.members_count IS NOT NULL RETURN g.objectid AS o, g.members_count AS m ORDER BY o`,
	},
	{
		Name:    "set_groups_has_members",
		Cypher:  `MATCH (g:Group) SET g.has_members=(CASE WHEN g.members_count>0 THEN TRUE ELSE FALSE END)`,
		IsWrite: true,
		Verify:  `MATCH (g:Group) WHERE g.has_members IS NOT NULL RETURN g.objectid AS o, g.has_members AS h ORDER BY o`,
	},
	{
		Name:    "set_gpo_links_count",
		Cypher:  `MATCH p=(g:GPO)-[:GPLink]->(o) WITH g.name as gponame, count(p) AS gpolinkscount MATCH (g1:GPO) WHERE g1.name=gponame AND gpolinkscount IS NOT NULL SET g1.gpolinks_count=gpolinkscount`,
		IsWrite: true,
		Verify:  `MATCH (g:GPO) WHERE g.gpolinks_count IS NOT NULL RETURN g.objectid AS o, g.gpolinks_count AS n ORDER BY o`,
	},
	{
		Name:    "set_gpos_has_links",
		Cypher:  `MATCH (g:GPO) SET g.has_links=(CASE WHEN g.gpolinks_count>0 THEN TRUE ELSE FALSE END)`,
		IsWrite: true,
		Verify:  `MATCH (g:GPO) WHERE g.has_links IS NOT NULL RETURN g.objectid AS o, g.has_links AS h ORDER BY o`,
	},
	{
		Name:    "set_groups_direct_admin",
		Cypher:  `MATCH (g:Group)-[r:AdminTo]->(c:Computer) SET g.is_admin=true RETURN DISTINCT g`,
		IsWrite: true,
		Verify:  `MATCH (g:Group) WHERE g.is_admin=true RETURN g.objectid AS o ORDER BY o`,
	},
	// set_groups_indirect_admin_1..4 — fixed-point chain. All four passes
	// execute back-to-back on each engine; verify only after the 4th pass.
	{
		Name:    "set_groups_indirect_admin_1",
		Cypher:  `MATCH (g:Group)-[r:MemberOf]->(gg:Group{is_admin:true}) SET g.is_admin=true RETURN DISTINCT g`,
		IsWrite: true,
		// no Verify — grouped with the next 3 passes
	},
	{
		Name:    "set_groups_indirect_admin_2",
		Cypher:  `MATCH (g:Group)-[r:MemberOf]->(gg:Group{is_admin:true}) WHERE g.is_admin IS NULL SET g.is_admin=true RETURN DISTINCT g`,
		IsWrite: true,
	},
	{
		Name:    "set_groups_indirect_admin_3",
		Cypher:  `MATCH (g:Group)-[r:MemberOf]->(gg:Group{is_admin:true}) WHERE g.is_admin IS NULL SET g.is_admin=true RETURN DISTINCT g`,
		IsWrite: true,
	},
	{
		Name:    "set_groups_indirect_admin_4",
		Cypher:  `MATCH (g:Group)-[r:MemberOf]->(gg:Group{is_admin:true}) WHERE g.is_admin IS NULL SET g.is_admin=true RETURN DISTINCT g`,
		IsWrite: true,
		// Verify the fixed-point of all four passes on Group.is_admin
		Verify: `MATCH (g:Group) WHERE g.is_admin=true RETURN g.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_user_indirect_admin",
		Cypher:  `MATCH (u:User)-[:MemberOf]->(:Group{is_admin:true}) SET u.is_admin=true`,
		IsWrite: true,
		Verify:  `MATCH (u:User) WHERE u.is_admin=true RETURN u.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_users_direct_admin",
		Cypher:  `MATCH (u:User)-[:AdminTo]->() SET u.is_admin=true`,
		IsWrite: true,
		Verify:  `MATCH (u:User) WHERE u.is_admin=true RETURN u.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_target_kud",
		Cypher:  `MATCH (o{unconstraineddelegation:true}) WHERE ((o:User AND o.enabled=true) OR (o:Computer AND o.is_dc=false)) SET o.target_kud=TRUE`,
		IsWrite: true,
		Verify:  `MATCH (o) WHERE o.target_kud=true RETURN o.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_az_privileged",
		Cypher:  `MATCH (n:AZBase) WHERE 'admin_tier_0' IN split(n.system_tags, ' ') AND n.name =~ '(?i)Global Administrator.*|User Administrator.*|Cloud Application Administrator.*|Authentication Policy Administrator.*|Exchange Administrator.*|Helpdesk Administrator.*|Privileged Authentication Administrator.*'  SET n.is_priv=true`,
		IsWrite: true,
		Verify:  `MATCH (n:AZBase) WHERE n.is_priv=true RETURN n.objectid AS o ORDER BY o`,
	},
	{
		Name:    "set_az_not_privileged",
		Cypher:  `MATCH (n:AZBase) WHERE n.is_priv IS NULL SET n.is_priv=false`,
		IsWrite: true,
		Verify:  `MATCH (n:AZBase) WHERE n.is_priv IS NULL RETURN count(n) AS c`,
	},
	{
		Name:    "azure_set_apps_name",
		Cypher:  `MATCH (a:AZApp) WHERE a.name IS NULL AND a.displayname IS NOT NULL SET a.name = a.displayname`,
		IsWrite: true,
		Verify:  `MATCH (a:AZApp) WHERE a.name IS NULL AND a.displayname IS NOT NULL RETURN count(a) AS c`,
	},
	{
		Name:   "nb_domain_collected",
		Cypher: `MATCH (m:Domain{collected:true}) RETURN m.name`,
	},
	{
		Name:    "set_ghost_computer",
		Cypher:  `MATCH (n:Computer{enabled:true}) WHERE toInteger((1714435200 - n.lastlogontimestamp)/86400)>90 SET   n.ghost_computer=TRUE`,
		IsWrite: true,
		Verify:  `MATCH (n:Computer) WHERE n.ghost_computer=true RETURN n.objectid AS o ORDER BY o`,
	},
	// get_all_nodes is dropped: upstream returns ID(o) as a column, and the
	// raw integer IDs from kglite vs Neo4j are disjoint by construction —
	// the step would deterministically mismatch on every run, polluting the
	// signal. Stable identity is already exercised via objectid in many
	// other steps.
	{
		Name:   "domains",
		Cypher: `MATCH (m:Domain) RETURN DISTINCT(m.name) AS domain ORDER BY m.name`,
	},
	{
		Name:   "nb_domain_controllers",
		Cypher: `MATCH (c1:Computer{is_dc:TRUE}) RETURN DISTINCT(c1.domain) AS domain, c1.name AS name, COALESCE(c1.operatingsystem, 'Unknown') AS os, COALESCE(c1.ghost_computer, False) AS ghost, toInteger((1714435200 - c1.lastlogontimestamp)/86400) as lastLogon`,
	},
	{
		Name:   "domain_OUs",
		Cypher: `MATCH (o:OU)-[:Contains]->(c) RETURN o.name AS OU, c.name AS name`,
	},
	{
		Name:   "users_shadow_credentials",
		Cypher: `MATCH (u:User{enabled:true,is_da:false}) WITH u ORDER BY ID(u) MATCH p=(u)-[:MemberOf*0..3]->()-[r:AddKeyCredentialLink|WriteProperty|GenericAll|GenericWrite|Owns|WriteDacl]->(m:User{is_da:true,enabled:true}) RETURN p`,
	},
	{
		Name:   "users_shadow_credentials_to_non_admins",
		Cypher: `MATCH (s) WHERE (s:User AND s.enabled AND NOT s.is_da) OR (s:Group AND NOT s.is_dag AND NOT s.is_da) WITH s ORDER BY ID(s) MATCH p=shortestPath((s)-[r:AddKeyCredentialLink|WriteProperty|GenericAll|GenericWrite|Owns|WriteDacl*1..3]->(t:User{enabled:true})) WHERE s <> t AND s.is_group_account_operator IS NULL RETURN p`,
	},
	{
		Name:   "nb_enabled_accounts",
		Cypher: `MATCH p=(u:User{enabled:true} ) RETURN DISTINCT(u.domain) AS domain, u.name AS name, toInteger((1714435200 - u.lastlogontimestamp)/86400) AS logon ORDER BY u.domain`,
	},
	{
		Name:   "nb_disabled_accounts",
		Cypher: `MATCH p=(u:User{enabled:false} ) RETURN DISTINCT(u.domain) AS domain, u.name AS name, toInteger((1714435200 - u.lastlogontimestamp)/86400) AS logon ORDER BY u.domain`,
	},
	{
		Name:   "nb_groups",
		Cypher: `MATCH p=(g:Group) WHERE NOT g.name IS NULL AND NOT g.domain IS NULL RETURN DISTINCT(g.domain) AS domain, g.name AS name, g.is_da AS da ORDER BY g.domain`,
	},
	{
		Name:   "nb_computers",
		Cypher: `MATCH (c:Computer) WHERE NOT c.name IS NULL RETURN DISTINCT(c.domain) AS domain, c.name AS name, c.operatingsystem AS os, c.ghost_computer AS ghost, c.enabled as enabled ORDER BY c.domain`,
	},
	{
		Name:   "computers_not_connected_since",
		Cypher: `MATCH (c:Computer) WHERE NOT c.lastlogontimestamp IS NULL AND c.name IS NOT NULL AND c.enabled RETURN c.name AS name, toInteger((1714435200 - c.lastlogontimestamp)/86400) as days, toInteger((1714435200 - c.pwdlastset)/86400) as pwdlastset, c.enabled as enabled ORDER BY days DESC`,
	},
	{
		Name:   "nb_domain_admins",
		Cypher: "MATCH (n{enabled:true}) WHERE n.is_msol IS NULL AND n.is_da = TRUE RETURN n.domain AS domain, n.name AS name, n.da_types AS `admin type`, n.admincount AS `admincount`",
	},
	{
		Name:   "os",
		Cypher: `MATCH (c:Computer{enabled:true}) WHERE  NOT c.enabled IS NULL AND NOT c.operatingsystem IS NULL RETURN DISTINCT(c.operatingsystem) AS os, toInteger((1714435200 - c.lastlogontimestamp)/86400) as lastLogon, c.name AS name, c.domain AS domain ORDER BY c.operatingsystem`,
	},
	{
		Name:   "krb_pwd_last_change",
		Cypher: `MATCH(u:User) WHERE u.name STARTS WITH "KRBTGT@" RETURN u.domain as domain, u.name as name, toInteger((1714435200 - u.pwdlastset)/86400) as pass_last_change, toInteger((1714435200 - u.whencreated)/86400) AS accountCreationDate`,
	},
	{
		Name:   "nb_kerberoastable_accounts",
		Cypher: `MATCH (u:User{hasspn:true,enabled:true}) WHERE u.gmsa IS NULL AND u.name IS NOT NULL RETURN u.domain AS domain, u.name AS name, toInteger((1714435200 - u.pwdlastset)/86400) AS pass_last_change, u.is_da AS is_Domain_Admin, u.serviceprincipalnames AS SPN, toInteger((1714435200 - u.whencreated)/86400) AS accountCreationDate ORDER BY pass_last_change DESC`,
	},
	{
		Name:   "nb_as-rep_roastable_accounts",
		Cypher: `MATCH (u:User{enabled:true,dontreqpreauth: true}) RETURN u.domain AS domain,u.name AS name, u.is_da AS is_Domain_Admin`,
	},
	{
		Name:   "nb_computer_unconstrained_delegations",
		Cypher: `MATCH (c2:Computer{unconstraineddelegation:true,is_dc:FALSE}) RETURN DISTINCT(c2.domain) AS domain,c2.name AS name`,
	},
	{
		Name:   "nb_users_unconstrained_delegations",
		Cypher: `MATCH (c2:User{enabled:true,unconstraineddelegation:true,is_da:FALSE}) RETURN DISTINCT(c2.domain) AS domain,c2.name AS name`,
	},
	{
		Name:   "users_constrained_delegations",
		Cypher: `MATCH (u:User)-[:AllowedToDelegate]->(c:Computer) WHERE u.name IS NOT NULL AND c.name IS NOT NULL RETURN u.name AS name, c.name AS computer,c.is_dc as to_DC ORDER BY name`,
	},
	{
		Name:   "dormant_accounts",
		Cypher: `MATCH (n:User{enabled:true}) WHERE toInteger((1714435200 - n.lastlogontimestamp)/86400)>90 RETURN n.domain as domain, n.name as name, n.displayname as displayname, toInteger((1714435200 - n.lastlogontimestamp)/86400) AS days, toInteger((1714435200 - n.whencreated)/86400) AS accountCreationDate, n.distinguishedname as distinguishedname ORDER BY days DESC`,
	},
	{
		Name:   "password_last_change",
		Cypher: `MATCH (c:User {enabled:TRUE}) RETURN DISTINCT(c.name) AS user,toInteger((1714435200 - c.pwdlastset )/ 86400) AS days, toInteger((1714435200 - c.whencreated)/86400) AS accountCreationDate ORDER BY days DESC`,
	},
	{
		Name:   "nb_user_password_cleartext",
		Cypher: "MATCH (u:User) WHERE NOT u.userpassword IS null RETURN u.name AS user,\"[redacted for security purposes]\" AS password, u.is_da as `is Domain Admin`",
	},
	{
		Name:   "get_users_password_not_required",
		Cypher: `MATCH (u:User{enabled:true,passwordnotreqd:true}) RETURN DISTINCT (u.domain) as domain, (u.name) AS user,toInteger((1714435200 - u.pwdlastset )/ 86400) AS pwdlastset,toInteger((1714435200 - u.lastlogontimestamp)/86400) AS lastlogon`,
	},
	{
		Name:   "objects_admincount",
		Cypher: `MATCH (n{enabled:True, admincount:True}) RETURN n.domain as domain, labels(n) as type, n.name as name`,
	},
	{
		Name:   "user_password_never_expires",
		Cypher: `MATCH (u:User{enabled:true})WHERE u.pwdneverexpires = true RETURN DISTINCT(u.domain) AS domain, u.name AS name, toInteger((1714435200 - u.lastlogontimestamp)/86400) AS LastLogin, toInteger((1714435200 - u.pwdlastset )/ 86400) AS LastPasswChange,toInteger((1714435200 - u.whencreated)/86400) AS accountCreationDate`,
	},
	{
		Name:   "computers_members_high_privilege",
		Cypher: "MATCH(c:Computer{is_dc:false})-[r:MemberOf*1..4]->(g:Group{is_da:true}) WHERE NOT c.name IS NULL RETURN distinct(c.name) AS computer, g.name AS `group`, g.domain AS domain",
	},
	{
		Name:    "objects_to_domain_admin",
		Cypher:  `MATCH (m{path_candidate:true}) WHERE NOT m.name IS NULL WITH m ORDER BY ID(m) MATCH p = shortestPath((m)-[r:MemberOf|HasSession|AdminTo|AllExtendedRights|AddMember|ForceChangePassword|GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner|ExecuteDCOM|AllowedToDelegate|ReadLAPSPassword|Contains|GPLink|AddAllowedToAct|AllowedToAct|SQLAdmin|ReadGMSAPassword|HasSIDHistory|CanPSRemote|AddSelf|WriteSPN|AddKeyCredentialLink|SyncLAPSPassword|UnconstrainedDelegations|WriteAccountRestrictions|DumpSMSAPassword|Synced|GoldenCert|WriteGPLink|DCSync|CoerceToTGT|SameForestTrust|SpoofSIDHistory|HasTrustKeys*1..5]->(g:Group{is_dag:true})) WHERE m<>g SET m.has_path_to_da=true RETURN DISTINCT(p) as p`,
		IsWrite: true,
		Verify:  `MATCH (m) WHERE m.has_path_to_da=true RETURN m.objectid AS o ORDER BY o`,
	},
	{
		Name:   "objects_to_adcs",
		Cypher: `MATCH (o{path_candidate:true}) WHERE NOT o:Group AND NOT o.name IS NULL WITH o ORDER BY o.name MATCH p=(o)-[rrr:MemberOf*0..4]->()-[rr:AdminTo]->(c{is_adcs:true}) RETURN DISTINCT(p) as p`,
	},
	{
		Name:   "users_admin_on_computers",
		Cypher: `MATCH (u:User{enabled:true}) WITH u ORDER BY ID(u) MATCH p=(u)-[:MemberOf*0..3]->()-[r:AdminTo]->(c:Computer) RETURN u.name AS user, u.displayname as displayname, c.name AS computer, c.has_path_to_da AS has_path_to_da, ID(u) as user_id, u.distinguishedname AS distinguishedname, p`,
	},
	{
		Name:   "users_admin_on_servers_1",
		Cypher: `MATCH (n:User{enabled:true,is_da:false}) WHERE NOT n.name IS NULL WITH n ORDER BY ID(n) MATCH p=(n)-[r:MemberOf*1..2]->(g:Group)-[r1:MemberOf|HasSession|AdminTo|AllExtendedRights|AddMember|ForceChangePassword|GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner|ExecuteDCOM|AllowedToDelegate|ReadLAPSPassword|Contains|GPLink|AddAllowedToAct|AllowedToAct|SQLAdmin|ReadGMSAPassword|HasSIDHistory|CanPSRemote|AddSelf|WriteSPN|AddKeyCredentialLink|SyncLAPSPassword|UnconstrainedDelegations|WriteAccountRestrictions|DumpSMSAPassword|Synced|GoldenCert|WriteGPLink|DCSync|CoerceToTGT|SameForestTrust|SpoofSIDHistory|HasTrustKeys]->(u:Computer) WITH LENGTH(p) as pathLength, p, n, u WHERE NONE (x in NODES(p)[1..(pathLength-1)] WHERE x.objectid = u.objectid) AND NOT n.objectid = u.objectid RETURN n.name AS user, u.name AS computer, u.has_path_to_da as has_path_to_da`,
	},
	{
		Name:   "users_admin_on_servers_2",
		Cypher: `MATCH (n:User{enabled:true,is_da:false}) WHERE NOT n.name IS NULL WITH n ORDER BY ID(n) MATCH p=(n)-[r1:MemberOf|HasSession|AdminTo|AllExtendedRights|AddMember|ForceChangePassword|GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner|ExecuteDCOM|AllowedToDelegate|ReadLAPSPassword|Contains|GPLink|AddAllowedToAct|AllowedToAct|SQLAdmin|ReadGMSAPassword|HasSIDHistory|CanPSRemote|AddSelf|WriteSPN|AddKeyCredentialLink|SyncLAPSPassword|UnconstrainedDelegations|WriteAccountRestrictions|DumpSMSAPassword|Synced|GoldenCert|WriteGPLink|DCSync|CoerceToTGT|SameForestTrust|SpoofSIDHistory|HasTrustKeys]->(u:Computer) WITH LENGTH(p) as pathLength, p, n, u WHERE NONE (x in NODES(p)[1..(pathLength-1)] WHERE x.objectid = u.objectid) AND NOT n.objectid = u.objectid RETURN n.name AS user, u.name AS computer, u.has_path_to_da as has_path_to_da`,
	},
	{
		Name:   "computers_admin_on_computers",
		Cypher: `MATCH (c1:Computer)-[:MemberOf*0..]->()-[:AdminTo]->(c2:Computer) WHERE c1 <> c2 RETURN DISTINCT c1.name AS source_computer, c2.name AS target_computer, c2.has_path_to_da AS has_path_to_da, c2.smbsigning AS smbsigning`,
	},
	{
		Name:   "domain_map_trust",
		Cypher: `MATCH p=shortestpath((d:Domain)-[:TrustedBy|AbuseTGTDelegation|SameForestTrust|SpoofSIDHistory|CrossForestTrust]->(m:Domain)) WHERE d<>m RETURN DISTINCT(p)`,
	},
	{
		Name:   "kud",
		Cypher: `MATCH (n) WHERE (n:Computer OR (n:User AND n.enabled=true))  AND (n.is_da IS NULL OR n.is_da=FALSE) AND (n.is_dc IS NULL OR n.is_dc=FALSE) WITH n ORDER BY n.name MATCH p=shortestPath((n)-[:MemberOf|HasSession|AdminTo|AllExtendedRights|AddMember|ForceChangePassword|GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner|ExecuteDCOM|AllowedToDelegate|ReadLAPSPassword|Contains|GPLink|AddAllowedToAct|AllowedToAct|SQLAdmin|ReadGMSAPassword|HasSIDHistory|CanPSRemote|AddSelf|WriteSPN|AddKeyCredentialLink|SyncLAPSPassword|UnconstrainedDelegations|WriteAccountRestrictions|DumpSMSAPassword|Synced|GoldenCert|WriteGPLink|DCSync|CoerceToTGT|SameForestTrust|SpoofSIDHistory|HasTrustKeys*1..5]->(m{target_kud:true})) WHERE NOT n=m AND (((n.is_da IS NULL OR n.is_da=FALSE) AND (n.is_dc IS NULL OR n.is_dc=FALSE)) OR (NOT m.domain CONTAINS '.' + n.domain AND n.domain <> m.domain)) RETURN DISTINCT(p)`,
	},
	{
		Name:   "nb_computers_laps",
		Cypher: `MATCH (c:Computer) WHERE NOT c.name is NULL and NOT c.haslaps IS NULL AND toUpper(c.operatingsystem) CONTAINS 'WINDOWS' RETURN DISTINCT(c.domain) AS domain, toInteger((1714435200 - c.lastlogontimestamp)/86400) as lastLogon, c.name AS name, toString(c.haslaps) AS LAPS`,
	},
	{
		Name:   "can_read_laps",
		Cypher: `MATCH (n{path_candidate:true}) WHERE n:User OR n:Group OR n:Computer WITH n ORDER BY ID(n) MATCH p = (n)-[r1:MemberOf*0..3]->()-[r2:GenericAll|ReadLAPSPassword|AllExtendedRights|SyncLAPSPassword]->(t:Computer{haslaps:true}) WHERE NOT (n)-[:MemberOf*0..3]->()-[:AdminTo]->(t) RETURN DISTINCT n.domain AS source_domain, n.name AS source_name, labels(n) as source_labels, t.domain as target_domain, t.name as target_name`,
	},
	{
		Name:   "objects_to_dcsync",
		Cypher: `MATCH (n{path_candidate:true}) WHERE n.can_dcsync IS NULL AND NOT n.name IS NULL WITH n ORDER BY n.name MATCH p = shortestPath((n)-[r:MemberOf|HasSession|AdminTo|AllExtendedRights|AddMember|ForceChangePassword|GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner|ExecuteDCOM|AllowedToDelegate|ReadLAPSPassword|Contains|GPLink|AddAllowedToAct|AllowedToAct|SQLAdmin|ReadGMSAPassword|HasSIDHistory|CanPSRemote|AddSelf|WriteSPN|AddKeyCredentialLink|SyncLAPSPassword|UnconstrainedDelegations|WriteAccountRestrictions|DumpSMSAPassword|Synced|GoldenCert|WriteGPLink|DCSync|CoerceToTGT|SameForestTrust|SpoofSIDHistory|HasTrustKeys*1..5]->(target{can_dcsync:TRUE})) WHERE n<>target RETURN distinct(p) AS p`,
	},
	{
		Name:   "dom_admin_on_non_dc",
		Cypher: `MATCH p=(c:Computer{path_candidate:true})-[r:HasSession]->(u:User{enabled:true, is_da:true}) WHERE NOT c.name IS NULL and NOT u.name IS NULL and NOT c.is_dc=True RETURN distinct(p) AS p`,
	},
	{
		Name:   "unpriv_to_dnsadmins",
		Cypher: `MATCH (u:User{path_candidate:true}) WITH u ORDER BY u.name MATCH p=(u)-[r:MemberOf*1..5]->(g:Group{is_dnsadmin:true}) RETURN distinct(p) AS p`,
	},
	{
		Name:   "rdp_access",
		Cypher: `MATCH (u:User{enabled:true,is_da:false}) WITH u ORDER BY ID(u) MATCH p=(u)-[r1:MemberOf*0..5]->()-[r2:CanRDP]->(c:Computer) RETURN u.name as user, c.name as computer`,
	},
	{
		Name:   "dc_impersonation",
		Cypher: `MATCH (u{ou_candidate:true}) WITH u ORDER BY ID(u) MATCH p=(u)-[r:MemberOf*0..3]->()-[r3:AddKeyCredentialLink|WriteProperty|GenericAll|GenericWrite|Owns|WriteDacl]->(m:Computer{is_dc:true}) RETURN DISTINCT p`,
	},
	{
		Name:    "graph_rbcd",
		Cypher:  `MATCH (m:Computer{is_server:true}) WITH m MATCH p=(u:User{path_candidate:true})-[rr:MemberOf|AddMember*0..5]->()-[r:GenericAll|GenericWrite|WriteDACL|AllExtendedRights|Owns]->(m) SET m.is_rbcd_target=TRUE RETURN p`,
		IsWrite: true,
		Verify:  `MATCH (m:Computer) WHERE m.is_rbcd_target=true RETURN m.objectid AS o ORDER BY o`,
	},
	{
		Name:   "graph_rbcd_to_da",
		Cypher: `MATCH (m:Computer{is_rbcd_target:true}) WHERE NOT m.name IS NULL WITH m ORDER BY m.name MATCH p = shortestPath((m)-[r:MemberOf|HasSession|AdminTo|AllExtendedRights|AddMember|ForceChangePassword|GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner|ExecuteDCOM|AllowedToDelegate|ReadLAPSPassword|Contains|GPLink|AddAllowedToAct|AllowedToAct|SQLAdmin|ReadGMSAPassword|HasSIDHistory|CanPSRemote|AddSelf|WriteSPN|AddKeyCredentialLink|SyncLAPSPassword|UnconstrainedDelegations|WriteAccountRestrictions|DumpSMSAPassword|Synced|GoldenCert|WriteGPLink|DCSync|CoerceToTGT|SameForestTrust|SpoofSIDHistory|HasTrustKeys*1..5]->(g:Group{is_dag:true})) WHERE m<>g RETURN DISTINCT(p) as p`,
	},
	{
		// compromise_paths_of_OUs is upstream-marked write=False but the
		// query body contains a SET. Treat as write per the actual Cypher.
		Name:    "compromise_paths_of_OUs",
		Cypher:  `MATCH (o:OU) WITH o ORDER BY ID(o) MATCH p=shortestPath((u{ou_candidate:true})-[:MemberOf|GenericAll|GenericWrite|Owns|WriteOwner|WriteDacl|WriteGPLink*1..8]->(o:OU)) SET o.vulnerable_OU = TRUE RETURN p`,
		IsWrite: true,
		Verify:  `MATCH (o:OU) WHERE o.vulnerable_OU=true RETURN o.objectid AS o ORDER BY o`,
	},
	{
		Name:   "vulnerable_OU_impact",
		Cypher: `MATCH (o:OU{vulnerable_OU:true}) WITH o ORDER BY o.name MATCH p=shortestPath((o)-[:Contains|MemberOf*1..]->(e)) WHERE o <> e AND (e:User OR e:Computer) RETURN p`,
	},
	{
		Name:   "vuln_functional_level",
		Cypher: "MATCH (o:Domain) WHERE NOT(o.functionallevel IS NULL OR SIZE(o.functionallevel) < 1) RETURN CASE WHEN toUpper(o.functionallevel) CONTAINS \"2000\" OR toUpper(o.functionallevel) CONTAINS \"2003\" OR toUpper(o.functionallevel) CONTAINS \"2008\" OR toUpper(o.functionallevel) CONTAINS \"2008 R2\" THEN 1 WHEN toUpper(o.functionallevel) CONTAINS \"2012\" THEN 2 WHEN toUpper(o.functionallevel) CONTAINS \"2016\" OR toUpper(o.functionallevel) CONTAINS \"2018\" OR toUpper(o.functionallevel) CONTAINS \"2020\" OR toUpper(o.functionallevel) CONTAINS \"2022\" THEN 5 END as `Level maturity`, o.distinguishedname as `Full name`, o.functionallevel as `Functional level`",
	},
	{
		Name:   "vuln_sidhistory_dangerous",
		Cypher: `MATCH(o1)-[r:HasSIDHistory]->(o2{is_da:true}) RETURN o1.domain as parent_domain, o1.name as name, o1.sidhistory as sidhistory`,
	},
	{
		Name:   "can_read_gmsapassword_of_adm",
		Cypher: `MATCH (o{path_candidate:true}) WITH o ORDER BY ID(o) MATCH p=((o)-[:MemberOf*0..5]->()-[:ReadGMSAPassword]->(u:User{is_admin:true})) WHERE o.name<>u.name RETURN DISTINCT(p)`,
	},
	{
		Name:   "objects_to_operators_member",
		Cypher: `MATCH (m:User{path_candidate:true}) WITH m ORDER BY m.name MATCH p = shortestPath((m)-[r:MemberOf|HasSession|AdminTo|AllExtendedRights|AddMember|ForceChangePassword|GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner|ExecuteDCOM|AllowedToDelegate|ReadLAPSPassword|Contains|GPLink|AddAllowedToAct|AllowedToAct|SQLAdmin|ReadGMSAPassword|HasSIDHistory|CanPSRemote|AddSelf|WriteSPN|AddKeyCredentialLink|SyncLAPSPassword|UnconstrainedDelegations|WriteAccountRestrictions|DumpSMSAPassword|Synced|GoldenCert|WriteGPLink|DCSync|CoerceToTGT|SameForestTrust|SpoofSIDHistory|HasTrustKeys*1..5]->(o:User{is_operator_member:true})) WHERE m<>o AND ((o.is_da=true AND o.domain<>m.domain) OR (o.is_da=false)) RETURN DISTINCT(p) as p`,
	},
	{
		Name:   "objects_to_operators_groups",
		Cypher: `MATCH (m:User{is_operator_member:true}) WITH m ORDER BY ID(m) MATCH p = shortestPath((m)-[r:MemberOf*1..5]->(o:Group{is_group_operator:true})) WHERE (m.is_da=true AND o.domain<>m.domain) OR (m.is_da=false) RETURN DISTINCT(p) as p`,
	},
	{
		Name:   "vuln_permissions_adminsdholder",
		Cypher: `MATCH (n:User{path_candidate:true}) WITH n ORDER BY n.name MATCH p = shortestPath((n)-[r:MemberOf|HasSession|AdminTo|AllExtendedRights|AddMember|ForceChangePassword|GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner|ExecuteDCOM|AllowedToDelegate|ReadLAPSPassword|Contains|GPLink|AddAllowedToAct|AllowedToAct|SQLAdmin|ReadGMSAPassword|HasSIDHistory|CanPSRemote|AddSelf|WriteSPN|AddKeyCredentialLink|SyncLAPSPassword|UnconstrainedDelegations|WriteAccountRestrictions|DumpSMSAPassword|Synced|GoldenCert|WriteGPLink|DCSync|CoerceToTGT|SameForestTrust|SpoofSIDHistory|HasTrustKeys*1..4]->(target1{is_adminsdholder:true})) WHERE n<>target1 AND NOT ANY(no in nodes(p) WHERE (no.is_da=true AND (no.domain=target1.domain OR target1.domain CONTAINS "." + no.domain))) RETURN distinct(p) AS p`,
	},
	{
		Name:   "da_to_da",
		Cypher: `MATCH p=shortestPath((g:Group{is_dag:true})-[r:MemberOf|HasSession|AdminTo|AllExtendedRights|AddMember|ForceChangePassword|GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner|ExecuteDCOM|AllowedToDelegate|ReadLAPSPassword|Contains|GPLink|AddAllowedToAct|AllowedToAct|SQLAdmin|ReadGMSAPassword|HasSIDHistory|CanPSRemote|AddSelf|WriteSPN|AddKeyCredentialLink|SyncLAPSPassword|UnconstrainedDelegations|WriteAccountRestrictions|DumpSMSAPassword|Synced|GoldenCert|WriteGPLink|DCSync|CoerceToTGT|SameForestTrust|SpoofSIDHistory|HasTrustKeys*1..5]->(gg:Group{is_dag:true})) WHERE g<>gg AND g.domain <> gg.domain RETURN p`,
	},
	{
		Name:   "anomaly_acl_1",
		Cypher: `MATCH (gg) WHERE NOT gg:Group AND ((gg:User AND gg.enabled) OR (gg:Computer AND gg.enabled) OR (NOT (gg:User OR gg:Computer))) WITH gg as g MATCH (g)-[r2{isacl:true}]->(n) WHERE ((g.is_da IS NULL OR g.is_da=FALSE) AND (g.is_dc IS NULL OR g.is_dc=FALSE) AND (NOT g.is_adcs OR g.is_adcs IS NULL)) OR (NOT n.domain CONTAINS '.' + g.domain AND n.domain <> g.domain) RETURN n.name,g.name,type(r2),LABELS(g),labels(n),ID(n)`,
	},
	{
		Name:   "anomaly_acl_2",
		Cypher: `MATCH (gg:Group) WHERE gg.members_count IS NOT NULL with gg as g order by gg.members_count DESC MATCH (g)-[r2{isacl:true}]->(n) WHERE ((g.is_da IS NULL OR g.is_da=FALSE) AND (g.is_dcg IS NULL OR g.is_dcg=FALSE) AND (NOT g.is_adcs OR g.is_adcs IS NULL)) OR (NOT n.domain CONTAINS '.' + g.domain AND n.domain <> g.domain) RETURN g.members_count,n.name,g.name,type(r2),LABELS(g),labels(n),ID(n) order by g.members_count DESC`,
	},
	{
		Name:   "get_empty_groups",
		Cypher: "MATCH (g:Group) WHERE NOT EXISTS(()-[:MemberOf]->(g)) AND NOT g.distinguishedname CONTAINS 'CN=BUILTIN' RETURN g.name AS `Empty group`, COALESCE(g.distinguishedname, '-') AS `Full Reference`",
	},
	{
		Name:   "get_empty_ous",
		Cypher: "MATCH (o:OU) WHERE NOT ()<-[:Contains]-(o) RETURN o.name AS `Empty Organizational Unit`, COALESCE(o.distinguishedname, '-') AS `Full Reference`",
	},
	{
		Name:   "has_sid_history",
		Cypher: "MATCH (a)-[r:HasSIDHistory]->(b) RETURN a.name AS `Has SID History`, LABELS(a) AS `Type_a`, b.name AS `Target`, LABELS(b) AS `Type_b`",
	},
	{
		Name:   "unpriv_users_to_GPO_init",
		Cypher: `MATCH (n:User{path_candidate:true}) WITH n ORDER BY n.name MATCH p = shortestPath((n)-[r:MemberOf|AddSelf|WriteSPN|AddKeyCredentialLink|AddMember|AllExtendedRights|ForceChangePassword|GenericAll|GenericWrite|WriteDacl|WriteOwner|Owns*1..]->(g:GPO)) WHERE NOT n=g AND NOT g.name IS NULL RETURN p`,
	},
	// The next four reads depend on g.dangerous_inbound being set by the
	// upstream Python postProcess setDangerousInboundOnGPOs, which is NOT
	// reproduced here. Both engines will return empty (parity pass).
	{
		Name:   "unpriv_users_to_GPO_user_enforced",
		Cypher: `MATCH (n:User{enabled:true}) WHERE n.name IS NOT NULL WITH n ORDER BY ID(n) MATCH p = (g:GPO{dangerous_inbound:true})-[r1:GPLink {enforced:true}]->(container2)-[r2:Contains*1..]->(n) RETURN p`,
	},
	{
		Name:   "unpriv_users_to_GPO_user_not_enforced",
		Cypher: `MATCH (n:User{enabled:true}) WHERE n.name IS NOT NULL WITH n ORDER BY ID(n) MATCH p = (g:GPO{dangerous_inbound:true})-[r1:GPLink{enforced:false}]->(container1)-[r2:Contains*1..]->(n) WHERE NONE(x in NODES(p) WHERE x.blocksinheritance = true AND (x:OU)) RETURN p`,
	},
	{
		Name:   "unpriv_users_to_GPO_computer_enforced",
		Cypher: `MATCH (n:Computer) WITH n ORDER BY ID(n) WITH n MATCH p = (g:GPO{dangerous_inbound:true})-[r1:GPLink {enforced:true}]->(container2)-[r2:Contains*1..]->(n) RETURN p`,
	},
	{
		Name:   "unpriv_users_to_GPO_computer_not_enforced",
		Cypher: `MATCH (n:Computer) WITH n ORDER BY ID(n) WITH n MATCH p = (g:GPO{dangerous_inbound:true})-[r1:GPLink{enforced:false}]->(container1)-[r2:Contains*1..]->(n) WHERE NONE(x in NODES(p) WHERE x.blocksinheritance = true AND (x:OU)) RETURN p`,
	},
	{
		Name:   "unpriv_users_to_GPO",
		Cypher: `MATCH (g:GPO) WITH g ORDER BY ID(g) OPTIONAL MATCH (g)-[r1:GPLink {enforced:false}]->(container1) WITH g,container1 OPTIONAL MATCH (g)-[r2:GPLink {enforced:true}]->(container2) WITH g,container1,container2 OPTIONAL MATCH p = (g)-[r1:GPLink]->(container1)-[r2:Contains*1..8]->(n1:Computer) WHERE NONE(x in NODES(p) WHERE x.blocksinheritance = true AND (x:OU)) WITH g,p,container2,n1 OPTIONAL MATCH p2 = (g)-[r1:GPLink]->(container2)-[r2:Contains*1..8]->(n2:Computer) RETURN p`,
	},
	{
		Name:   "cross_domain_local_admins",
		Cypher: `MATCH p=(u{enabled:true})-[r:MemberOf*0..4]->()-[rr:AdminTo]->(c:Computer) WHERE c.ghost_computer IS NULL AND u.domain <> c.domain AND NOT c.domain CONTAINS u.domain RETURN DISTINCT p`,
	},
	{
		Name:   "cross_domain_domain_admins",
		Cypher: `MATCH p=(u{enabled:true})-[r:MemberOf*1..4]->(g:Group{is_da:true}) WHERE u.domain <> g.domain AND NOT g.domain CONTAINS u.domain return p`,
	},
	{
		Name:   "primaryGroupID_lower_than_1000",
		Cypher: `MATCH (n) WHERE (n:Group OR n:User) AND toInteger(split(n.objectid, "-")[-1]) < 1000 AND (n.enabled = true or n:Group) return toInteger(split(n.objectid, "-")[-1]) as sid, n.name, n.domain, n.is_da`,
	},
	{
		Name:   "pre_windows_2000_compatible_access_group",
		Cypher: `MATCH (n:Group) WHERE n.name STARTS WITH "PRE-WINDOWS 2000 COMPATIBLE ACCESS@" MATCH (m)-[r:MemberOf]->(n) WHERE NOT m.objectid ENDS WITH "-S-1-5-11" return m.domain, m.name, m.objectid, labels(m) as type`,
	},
	{
		Name:   "guest_accounts",
		Cypher: `MATCH (n:User) WHERE n.objectid ENDS WITH "-501" RETURN n.name, n.domain, n.enabled`,
	},
	{
		Name:   "unpriviledged_users_with_admincount",
		Cypher: `MATCH (u:User{enabled:true}) WHERE u.is_da=false AND u.admincount=true RETURN u.name, u.domain, u.da_type`,
	},
	{
		Name:   "get_fgpp",
		Cypher: `MATCH (u:User) WHERE u.fgpp_name IS NOT NULL RETURN u.fgpp_msds_psoappliesto, u.fgpp_name, u.fgpp_msds_minimumpasswordlength, u.fgpp_msds_minimumpasswordage, u.fgpp_msds_maximumpasswordage, u.fgpp_msds_passwordreversibleencryptionenabled, u.fgpp_msds_passwordhistorylength, u.fgpp_msds_passwordcomplexityenabled, u.fgpp_msds_lockoutduration, u.fgpp_msds_lockoutthreshold, u.fgpp_msds_lockoutobservationwindow`,
	},
	{
		Name:    "esc15_adcs_privilege_escalation",
		Cypher:  `MATCH p=(x:Base)-[:MemberOf*0..]->()-[:Enroll|AllExtendedRights]->(ct:CertTemplate)-[:PublishedTo]->(:EnterpriseCA)-[:TrustedForNTAuth]->(:NTAuthStore)-[:NTAuthStoreFor]->(d:Domain) WHERE ct.enrolleesuppliessubject = True AND ct.authenticationenabled = False AND ct.requiresmanagerapproval = False AND ct.schemaversion = 1 CREATE (x)-[:ADCSESC15]->(d)`,
		IsWrite: true,
		Verify:  `MATCH ()-[r:ADCSESC15]->() RETURN count(r) AS c`,
	},
	{
		Name:   "smb_signing",
		Cypher: `MATCH (c:Computer) RETURN c.name AS name, c.domain AS domain, c.smbsigning AS smbsigning, c.is_dc AS dc, c.is_server AS server, toInteger((1714435200 - c.lastlogontimestamp)/86400) AS lastlogontimestamp`,
	},
	{
		Name:   "ldap_server_configuration",
		Cypher: `MATCH (c) WHERE c.ldapavailable OR c.ldapsavailable RETURN c.name AS name, c.domain AS domain, c.ldapavailable AS ldap, c.ldapsavailable AS ldaps, c.ldapsigning AS ldapsigning, c.ldapsepa AS ldapsepa`,
	},
	{
		Name:    "azure_set_gag",
		Cypher:  `MATCH (a:AZRole) WHERE a.name STARTS WITH 'GLOBAL ADMINISTRATOR@' SET a.is_gag=TRUE`,
		IsWrite: true,
		Verify:  `MATCH (a:AZRole) WHERE a.is_gag=true RETURN a.objectid AS o ORDER BY o`,
	},
	{
		Name:   "azure_user",
		Cypher: "MATCH (n:AZUser) RETURN n.name AS Name, n.tenantid AS `Tenant ID`, n.onpremisesyncenabled AS onpremisesynced, n.onpremisesecurityidentifier AS SID",
	},
	{
		Name:   "azure_admin",
		Cypher: "MATCH p =(n)-[r:AZGlobalAdmin*1..]->(m) RETURN n.name AS Name, n.tenantid AS `Tenant ID`",
	},
	{
		Name:   "azure_groups",
		Cypher: "MATCH (n:AZGroup) RETURN n.tenantid AS `Tenant ID`, n.name AS Name, COALESCE(n.description, '-') AS Description",
	},
	{
		Name:   "azure_vm",
		Cypher: "MATCH (n:AZVM) RETURN n.tenantid AS `Tenant ID`, n.name AS Name, n.operatingsystem AS os",
	},
	{
		Name:   "azure_apps",
		Cypher: "MATCH (n:AZApp) WHERE n.name IS NOT NULL AND SIZE(n.name) > 1 RETURN n.tenantid AS `Tenant ID`, n.name AS Name",
	},
	{
		Name:   "azure_devices",
		Cypher: "MATCH (n:AZDevice) RETURN n.tenantid AS `Tenant ID`, n.name AS Name, n.operatingsystem AS os",
	},
	{
		Name:   "azure_users_paths_high_target",
		Cypher: `MATCH (n:AZBase{is_priv:false}) WITH n ORDER BY n.name MATCH p=shortestPath((n)-[r:MemberOf|HasSession|AdminTo|AllExtendedRights|AddMember|ForceChangePassword|GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner|ExecuteDCOM|AllowedToDelegate|ReadLAPSPassword|Contains|GPLink|AddAllowedToAct|AllowedToAct|SQLAdmin|ReadGMSAPassword|HasSIDHistory|CanPSRemote|AddSelf|WriteSPN|AddKeyCredentialLink|SyncLAPSPassword|UnconstrainedDelegations|WriteAccountRestrictions|DumpSMSAPassword|Synced|GoldenCert|WriteGPLink|DCSync|CoerceToTGT|SameForestTrust|SpoofSIDHistory|HasTrustKeys*1..5]->(m:AZBase{is_priv:true})) WHERE m<>n RETURN p`,
	},
	{
		Name:   "azure_ms_graph_controllers",
		Cypher: `MATCH p = (n)-[r:AZAddOwner|AZAddSecret|AZAppAdmin|AZCloudAppAdmin|AZMGAddOwner|AZMGAddSecret|AZOwns]->(g:AZServicePrincipal {appdisplayname: "Microsoft Graph"}) RETURN p`,
	},
	{
		Name:   "azure_aadconnect_users",
		Cypher: "MATCH (u) WHERE (u:User OR u:AZUser) AND (u.name =~ '(?i)^MSOL_|.*AADConnect.*' OR u.userprincipalname =~ '(?i)^sync_.*') OPTIONAL MATCH (u)-[:HasSession]->(s:Session) RETURN u.name AS Name, s AS Session, u.tenantid AS `Tenant ID`",
	},
	{
		Name:   "azure_admin_on_prem",
		Cypher: `MATCH (u:User{is_da:true})-[:SyncedToEntraUser]->(a:AZUser)-[r:AZGlobalAdmin]->() RETURN u.name as Name`,
	},
	{
		Name:   "azure_role_listing",
		Cypher: `MATCH (a:AZRole) return distinct a.name AS Name, a.description AS Description`,
	},
	{
		Name:   "azure_role_paths",
		Cypher: `MATCH p=(a:AZUser)-[r:AZHasRole]->(x) return distinct p`,
	},
	{
		Name:   "azure_reset_passwd",
		Cypher: `MATCH (m:AZBase) WITH m ORDER BY ID(m) MATCH p=(n)-[r:AZResetPassword]->(m) return distinct p`,
	},
	{
		Name:   "azure_last_passwd_change",
		Cypher: "MATCH (u:User {enabled:TRUE}),(a:AZUser) WHERE a.onpremisesecurityidentifier = u.objectid RETURN DISTINCT(u.name) AS Name, toInteger((1714435200 - u.pwdlastset )/ 86400) AS `Last password set on premise`, toInteger((1714435200 - (datetime('1970-01-01T00:00:00').epochMillis + duration.inSeconds(datetime('1970-01-01T00:00:00'), a.pwdlastset).seconds)) / 86400) AS `Last password set on Azure`",
	},
	{
		Name:   "azure_dormant_accounts",
		Cypher: `MATCH (a:AZUser)-[:SyncedToADUser]->(u:User{enabled:TRUE}) RETURN a.name AS Name, toInteger((1714435200 - u.lastlogontimestamp)/86400) AS lastlogon, toInteger((1714435200 - u.whencreated)/86400) AS whencreated`,
	},
	{
		Name:   "azure_accounts_disabled_on_prem",
		Cypher: "MATCH (a:AZUser{enabled:TRUE})-[:SyncedToADUser]->(u:User{enabled:FALSE}) RETURN a.name AS `Azure name`, a.enabled AS `Enabled on Azure`, u.name AS `On premise name`, u.enabled AS `Enabled on premise` UNION MATCH (a:AZUser{enabled:FALSE})-[:SyncedToADUser]->(u:User{enabled:TRUE}) RETURN a.name AS `Azure name`, a.enabled AS `Enabled on Azure`, u.name AS `On premise name`, u.enabled AS `Enabled on premise`",
	},
	{
		Name:   "azure_accounts_not_found_on_prem",
		Cypher: `MATCH (azUser:AZUser{onpremisesyncenabled:true}) WHERE NOT EXISTS {MATCH (user:User) WHERE user.objectid = azUser.onpremisesecurityidentifier} return azUser.name AS Name`,
	},
	{
		Name:   "azure_tenants",
		Cypher: `MATCH (t:AZTenant) RETURN t.name AS Name, t.tenantid AS ID`,
	},
	{
		Name:   "azure_ga_to_ga",
		Cypher: `MATCH p=allShortestPaths((g:AZRole{is_gag:TRUE})-[r:MemberOf|HasSession|AdminTo|AllExtendedRights|AddMember|ForceChangePassword|GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner|ExecuteDCOM|AllowedToDelegate|ReadLAPSPassword|Contains|GPLink|AddAllowedToAct|AllowedToAct|SQLAdmin|ReadGMSAPassword|HasSIDHistory|CanPSRemote|AddSelf|WriteSPN|AddKeyCredentialLink|SyncLAPSPassword|UnconstrainedDelegations|WriteAccountRestrictions|DumpSMSAPassword|Synced|GoldenCert|WriteGPLink|DCSync|CoerceToTGT|SameForestTrust|SpoofSIDHistory|HasTrustKeys*1..5]->(gg:AZRole{is_gag:TRUE})) WHERE g<>gg AND g.tenantid <> gg.tenantid RETURN p`,
	},
	{
		Name:   "azure_cross_ga_da",
		Cypher: `MATCH p=allShortestPaths((g:AZRole{is_gag:TRUE})-[r:MemberOf|HasSession|AdminTo|AllExtendedRights|AddMember|ForceChangePassword|GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner|ExecuteDCOM|AllowedToDelegate|ReadLAPSPassword|Contains|GPLink|AddAllowedToAct|AllowedToAct|SQLAdmin|ReadGMSAPassword|HasSIDHistory|CanPSRemote|AddSelf|WriteSPN|AddKeyCredentialLink|SyncLAPSPassword|UnconstrainedDelegations|WriteAccountRestrictions|DumpSMSAPassword|Synced|GoldenCert|WriteGPLink|DCSync|CoerceToTGT|SameForestTrust|SpoofSIDHistory|HasTrustKeys*1..5]->(gg:Group{is_dag:TRUE})) RETURN p UNION MATCH p=allShortestPaths((gg:Group{is_dag:TRUE})-[r:MemberOf|HasSession|AdminTo|AllExtendedRights|AddMember|ForceChangePassword|GenericAll|GenericWrite|Owns|WriteDacl|WriteOwner|ExecuteDCOM|AllowedToDelegate|ReadLAPSPassword|Contains|GPLink|AddAllowedToAct|AllowedToAct|SQLAdmin|ReadGMSAPassword|HasSIDHistory|CanPSRemote|AddSelf|WriteSPN|AddKeyCredentialLink|SyncLAPSPassword|UnconstrainedDelegations|WriteAccountRestrictions|DumpSMSAPassword|Synced|GoldenCert|WriteGPLink|DCSync|CoerceToTGT|SameForestTrust|SpoofSIDHistory|HasTrustKeys*1..5]->(g:AZRole{is_gag:TRUE})) RETURN p`,
	},
}

// pipelineStepResult records the outcome of one step's parity check.
type pipelineStepResult struct {
	Name        string
	IsWrite     bool
	Cypher      string
	WriteKErr   error
	WriteNErr   error
	VerifyQuery string
	KResult     string
	NResult     string
	KDur        time.Duration
	NDur        time.Duration
	KErr        error
	NErr        error
	Match       bool
	Skipped     bool // skipped because grouped step has no Verify
}

// runWriteCypher executes a write-side Cypher statement on a graph database
// and returns any error. Read columns from RETURN are drained but discarded.
//
// Mutating statements deliberately do NOT use retryNeo4j: several pipeline
// SETs append to a list (e.g. `SET c.da_types = c.da_types + da_type`), and a
// transient Neo4j error during result drain after a successful commit would
// double-append on retry, producing a permanent kglite-vs-Neo4j divergence on
// list-valued properties. Idempotency is not guaranteed across all 66 writes,
// so retry is unsafe here.
func runWriteCypher(ctx context.Context, t *testing.T, db graph.Database, cypher string) (time.Duration, error) {
	t.Helper()
	start := time.Now()
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw(cypher, nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		for result.Next() {
			_ = result.Values()
		}
		return result.Error()
	})
	return time.Since(start), err
}

// runVerifyOnBoth executes a verify read query on both engines and returns the
// normalized + sorted serializations plus durations and errors.
func runVerifyOnBoth(ctx context.Context, t *testing.T, kgliteDB, neo4jDB graph.Database, verify string) (string, time.Duration, error, string, time.Duration, error) {
	kRaw, kDur, kErr := runQueryValues(ctx, t, kgliteDB, verify)
	nRaw, nDur, nErr := runQueryValues(ctx, t, neo4jDB, verify)
	kNorm := normalizeResult(kRaw)
	nNorm := normalizeResult(nRaw)
	if !hasOrderBy(verify) {
		kNorm = sortLines(kNorm)
		nNorm = sortLines(nNorm)
	}
	return kNorm, kDur, kErr, nNorm, nDur, nErr
}

// TestCompareADMinerPipeline runs the full upstream AD_Miner Cypher pipeline
// against both kglite and Neo4j, asserting result parity at every step.
//
// Mutating steps execute on each engine; their effects are then probed with a
// per-step verify query whose results are diffed across engines. Read steps
// are diffed directly. Per-test fresh fixtures: this test does NOT mutate the
// shared fixtureADGraph from shared_fixtures_test.go.
func TestCompareADMinerPipeline(t *testing.T) {
	// 30-minute ceiling guards against runaway path queries (compromise_paths_of_OUs,
	// kud, the *1..5 path search variants) outlasting Neo4j's transaction timeout
	// and hanging the test indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	adZip := filepath.Join(testdataDir(), "ad_sampledata.zip")
	skipIfMissing(t, adZip)

	kgliteDB := openGraph(t)
	neo4jDB := openNeo4j(t)

	// Prepare Neo4j
	clearNeo4j(ctx, t, neo4jDB)
	require.NoError(t, retryNeo4j(t, "AssertSchema", func() error {
		return neo4jDB.AssertSchema(ctx, schema.DefaultGraphSchema())
	}))

	ingestSchema := loadIngestSchema(t)

	t.Log("=== Ingesting AD data into kglite ===")
	kIngestDur := ingestZip(ctx, t, kgliteDB, adZip, ingestSchema)
	t.Logf("  kglite ingest: %s", kIngestDur.Round(time.Millisecond))

	t.Log("=== Ingesting AD data into Neo4j ===")
	nIngestDur := ingestZip(ctx, t, neo4jDB, adZip, ingestSchema)
	t.Logf("  Neo4j ingest: %s", nIngestDur.Round(time.Millisecond))

	// Run the production analysis pipeline on both engines first. The
	// AD_Miner queries assume bloodhound has already populated downstream
	// edges (ADCSESC*, etc.) and computed properties; running them on raw
	// ingest produces nonsense.
	t.Log("=== Running analysis on kglite ===")
	kAnalysisDur := runAnalysis(ctx, t, kgliteDB)
	t.Log("=== Running analysis on Neo4j ===")
	nAnalysisDur := runAnalysis(ctx, t, neo4jDB)

	t.Logf("Setup complete: kglite ingest=%s analysis=%s; Neo4j ingest=%s analysis=%s",
		kIngestDur.Round(time.Millisecond), kAnalysisDur.Round(time.Millisecond),
		nIngestDur.Round(time.Millisecond), nAnalysisDur.Round(time.Millisecond))

	// Run the AD_Miner pipeline.
	t.Logf("=== Running AD_Miner pipeline (%d steps) ===", len(adminerPipelineSteps))
	results := make([]pipelineStepResult, 0, len(adminerPipelineSteps))
	for i, step := range adminerPipelineSteps {
		t.Logf("step %3d/%d: %s", i+1, len(adminerPipelineSteps), step.Name)
		res := pipelineStepResult{
			Name:    step.Name,
			IsWrite: step.IsWrite,
			Cypher:  step.Cypher,
		}

		if step.IsWrite {
			// Execute mutation on both engines.
			kDur, kErr := runWriteCypher(ctx, t, kgliteDB, step.Cypher)
			nDur, nErr := runWriteCypher(ctx, t, neo4jDB, step.Cypher)
			res.WriteKErr = kErr
			res.WriteNErr = nErr
			res.KDur = kDur
			res.NDur = nDur

			if step.Verify == "" {
				// Grouped step (e.g. fixed-point intermediate pass);
				// skip parity check until the verify-bearing tail step.
				res.Skipped = true
				results = append(results, res)
				continue
			}

			// Verify the write's effect on both engines.
			kNorm, kvDur, kvErr, nNorm, nvDur, nvErr := runVerifyOnBoth(ctx, t, kgliteDB, neo4jDB, step.Verify)
			res.VerifyQuery = step.Verify
			res.KResult = kNorm
			res.NResult = nNorm
			res.KDur += kvDur
			res.NDur += nvDur
			res.KErr = kvErr
			res.NErr = nvErr
			res.Match = kErr == nil && nErr == nil && kvErr == nil && nvErr == nil && kNorm == nNorm
		} else {
			// Read-only step: compare its results directly.
			kNorm, kDur, kErr, nNorm, nDur, nErr := runVerifyOnBoth(ctx, t, kgliteDB, neo4jDB, step.Cypher)
			res.KResult = kNorm
			res.NResult = nNorm
			res.KDur = kDur
			res.NDur = nDur
			res.KErr = kErr
			res.NErr = nErr
			res.Match = kErr == nil && nErr == nil && kNorm == nNorm
		}

		results = append(results, res)
	}

	// Report per-step. Failing steps are accumulated and reported once at
	// the end (continue-and-report, not abort-first).
	var matches, mismatches, errors, skipped int
	t.Log("")
	t.Log("=== AD_Miner pipeline parity per step ===")
	t.Logf("%-50s %-7s %-8s %12s %12s | %s", "Step", "Kind", "Status", "kglite", "Neo4j", "Detail")
	t.Logf("%s", strings.Repeat("-", 130))

	for _, r := range results {
		if r.Skipped {
			skipped++
			t.Logf("%-50s %-7s %-8s %12s %12s | (no verify; grouped fixed-point pass)",
				truncateStr(r.Name, 50),
				kindLabel(r.IsWrite),
				"GROUPED",
				r.KDur.Round(time.Microsecond),
				r.NDur.Round(time.Microsecond))
			continue
		}

		status := "MATCH"
		switch {
		case r.WriteKErr != nil || r.WriteNErr != nil:
			status = "WRITE_ERR"
			errors++
		case r.KErr != nil || r.NErr != nil:
			status = "READ_ERR"
			errors++
		case !r.Match:
			status = "MISMATCH"
			mismatches++
		default:
			matches++
		}

		t.Logf("%-50s %-7s %-8s %12s %12s | k=%s n=%s",
			truncateStr(r.Name, 50),
			kindLabel(r.IsWrite),
			status,
			r.KDur.Round(time.Microsecond),
			r.NDur.Round(time.Microsecond),
			truncateStr(r.KResult, 40),
			truncateStr(r.NResult, 40))

		if status == "WRITE_ERR" {
			t.Logf("    write_err: kglite=%v neo4j=%v", r.WriteKErr, r.WriteNErr)
		}
		if status == "READ_ERR" {
			t.Logf("    read_err: kglite=%v neo4j=%v", r.KErr, r.NErr)
		}
		if status == "MISMATCH" {
			kLines := strings.Count(r.KResult, "\n")
			nLines := strings.Count(r.NResult, "\n")
			t.Logf("    verify=%s", truncateStr(r.VerifyQuery, 400))
			t.Logf("    kglite (%d lines)=%s", kLines, truncateStr(r.KResult, 4000))
			t.Logf("    neo4j  (%d lines)=%s", nLines, truncateStr(r.NResult, 4000))
		}
	}

	t.Logf("%s", strings.Repeat("-", 130))
	t.Logf("Summary: %d match, %d mismatch, %d error, %d grouped (out of %d steps)",
		matches, mismatches, errors, skipped, len(results))

	if mismatches > 0 || errors > 0 {
		t.Errorf("AD_Miner pipeline parity failed: %d mismatches, %d errors", mismatches, errors)
	}
}

func kindLabel(isWrite bool) string {
	if isWrite {
		return "WRITE"
	}
	return "READ"
}

func init() {
	// Validate that pipeline entries are well-formed.
	seen := make(map[string]struct{}, len(adminerPipelineSteps))
	for _, s := range adminerPipelineSteps {
		if s.Name == "" || s.Cypher == "" {
			panic(fmt.Sprintf("adminerPipelineSteps has empty entry: %+v", s))
		}
		if _, dup := seen[s.Name]; dup {
			panic(fmt.Sprintf("adminerPipelineSteps has duplicate step name: %s", s.Name))
		}
		seen[s.Name] = struct{}{}
		// Read-only steps must not have a Verify (the step IS the comparison).
		if !s.IsWrite && s.Verify != "" {
			panic(fmt.Sprintf("adminerPipelineSteps[%s]: read step must not have Verify", s.Name))
		}
		// Verify queries must be read-only — a Verify with SET/MERGE/DELETE/CREATE
		// would silently corrupt subsequent steps' state. Verifies are short,
		// hand-authored, and don't contain backtick-quoted aliases, so a naive
		// keyword scan is safe here. (We deliberately do NOT scan read-step
		// Cypher: upstream queries have aliases like `Last password set on
		// premise` that would trip a naive scan.)
		if s.Verify != "" && containsMutation(s.Verify) {
			panic(fmt.Sprintf("adminerPipelineSteps[%s]: Verify query contains a mutation keyword", s.Name))
		}
	}
}

// containsMutation returns true if the Cypher contains a top-level mutating
// keyword (SET / MERGE / DELETE / CREATE / REMOVE). Word-boundary matched and
// case-insensitive. Caller is responsible for not feeding strings with
// quoted aliases or string literals — see init() comment.
func containsMutation(cypher string) bool {
	upper := strings.ToUpper(cypher)
	for _, kw := range []string{" SET ", " MERGE ", " DELETE ", " CREATE ", " REMOVE "} {
		if strings.Contains(" "+upper+" ", kw) {
			return true
		}
	}
	return false
}
