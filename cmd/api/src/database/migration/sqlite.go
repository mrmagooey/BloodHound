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

package migration

import (
	"github.com/specterops/bloodhound/cmd/api/src/auth"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/bloodhound/cmd/api/src/model/appcfg"
	"gorm.io/gorm"
)

// MigrateSQLite uses GORM AutoMigrate to create/update the minimal schema required for
// standalone (SQLite-backed) operation. It does not run the PostgreSQL-specific stepwise
// migration files.
func MigrateSQLite(db *gorm.DB) error {
	if err := db.AutoMigrate(
		// Auth tables
		&model.Installation{},
		&model.Permission{},
		&model.Role{},
		&model.User{},
		&model.AuthSecret{},
		&model.AuthToken{},
		&model.UserSession{},
		&model.AuditLog{},
		&model.EnvironmentTargetedAccessControl{},
		// Ingest / asset group tables
		&model.Migration{},
		&model.IngestTask{},
		&model.IngestJob{},
		&model.AssetGroup{},
		&model.AssetGroupSelector{},
		&model.AssetGroupCollection{},
		&model.AssetGroupCollectionEntry{},
		&model.AssetGroupTag{},
		&model.AssetGroupTagSelector{},
		&model.SelectorSeed{},
		&model.AssetGroupSelectorNode{},
		&model.AssetGroupHistory{},
		&model.SavedQuery{},
		&model.SavedQueriesPermissions{},
		&appcfg.Parameter{},
		&appcfg.FeatureFlag{},
		// Graph schema tables: Kind table only (others created via raw SQL below
		// because their struct fields don't match the actual DB column names)
		&model.Kind{},
		&model.GraphSchemaExtension{},
		&model.GraphSchemaProperty{},
		&model.SchemaFinding{},
		&model.SchemaFindingsSubtype{},
		&model.SchemaEnvironmentPrincipalKind{},
	); err != nil {
		return err
	}

	// Create singleton tables that can't be expressed via GORM AutoMigrate due to
	// PostgreSQL-specific DDL in the model tags.
	const createDatapipeStatus = `
CREATE TABLE IF NOT EXISTS datapipe_status (
    singleton     integer DEFAULT 1 NOT NULL PRIMARY KEY CHECK (singleton = 1),
    status        text    NOT NULL DEFAULT 'idle',
    updated_at    datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_complete_analysis_at datetime,
    last_analysis_run_at      datetime
);`

	const seedDatapipeStatus = `
INSERT OR IGNORE INTO datapipe_status (singleton, status, updated_at)
VALUES (1, 'idle', CURRENT_TIMESTAMP);`

	const createAnalysisRequestSwitch = `
CREATE TABLE IF NOT EXISTS analysis_request_switch (
    singleton             integer DEFAULT 1 NOT NULL PRIMARY KEY CHECK (singleton = 1),
    request_type          text NOT NULL DEFAULT '',
    requested_by          text NOT NULL DEFAULT '',
    requested_at          datetime,
    delete_all_graph      integer NOT NULL DEFAULT 0,
    delete_sourceless_graph integer NOT NULL DEFAULT 0,
    delete_source_kinds   text NOT NULL DEFAULT '[]'
);`

	const createCustomNodeKinds = `
CREATE TABLE IF NOT EXISTS custom_node_kinds (
    id                  integer PRIMARY KEY AUTOINCREMENT,
    kind_name           text NOT NULL UNIQUE,
    schema_node_kind_id integer,
    config              text NOT NULL DEFAULT '{}',
    created_at          datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

	// schema_node_kinds: the struct's Name field is populated by a JOIN on the kind
	// table, but the actual column is kind_id. Must be created with raw SQL.
	const createSchemaNodeKinds = `
CREATE TABLE IF NOT EXISTS schema_node_kinds (
    id                  integer PRIMARY KEY AUTOINCREMENT,
    kind_id             integer,
    schema_extension_id integer,
    display_name        text NOT NULL DEFAULT '',
    description         text NOT NULL DEFAULT '',
    is_display_kind     integer NOT NULL DEFAULT 0,
    icon                text NOT NULL DEFAULT '',
    icon_color          text NOT NULL DEFAULT '',
    created_at          datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at          datetime
);`

	const createSchemaRelationshipKinds = `
CREATE TABLE IF NOT EXISTS schema_relationship_kinds (
    id                  integer PRIMARY KEY AUTOINCREMENT,
    kind_id             integer,
    schema_extension_id integer,
    name                text NOT NULL DEFAULT '',
    description         text NOT NULL DEFAULT '',
    is_traversable      integer NOT NULL DEFAULT 0,
    created_at          datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at          datetime
);`

	const createSchemaEnvironments = `
CREATE TABLE IF NOT EXISTS schema_environments (
    id                             integer PRIMARY KEY AUTOINCREMENT,
    schema_extension_id            integer,
    schema_extension_display_name  text NOT NULL DEFAULT '',
    environment_kind_id            integer,
    environment_kind_name          text NOT NULL DEFAULT '',
    source_kind_id                 integer,
    created_at                     datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                     datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at                     datetime
);`

	const createSourceKinds = `
CREATE TABLE IF NOT EXISTS source_kinds (
    id      integer PRIMARY KEY AUTOINCREMENT,
    kind_id integer NOT NULL,
    active  integer NOT NULL DEFAULT 1
);`

	const createADDataQualityStats = `
CREATE TABLE IF NOT EXISTS ad_data_quality_stats (
    id                       integer PRIMARY KEY AUTOINCREMENT,
    domain_sid               text NOT NULL DEFAULT '',
    users                    integer NOT NULL DEFAULT 0,
    groups                   integer NOT NULL DEFAULT 0,
    computers                integer NOT NULL DEFAULT 0,
    ous                      integer NOT NULL DEFAULT 0,
    containers               integer NOT NULL DEFAULT 0,
    gpos                     integer NOT NULL DEFAULT 0,
    aiacas                   integer NOT NULL DEFAULT 0,
    rootcas                  integer NOT NULL DEFAULT 0,
    enterprisecas            integer NOT NULL DEFAULT 0,
    ntauthstores             integer NOT NULL DEFAULT 0,
    certtemplates            integer NOT NULL DEFAULT 0,
    issuancepolicies         integer NOT NULL DEFAULT 0,
    acls                     integer NOT NULL DEFAULT 0,
    sessions                 integer NOT NULL DEFAULT 0,
    relationships            integer NOT NULL DEFAULT 0,
    session_completeness     real    NOT NULL DEFAULT 0,
    local_group_completeness real    NOT NULL DEFAULT 0,
    run_id                   text    NOT NULL DEFAULT '',
    created_at               datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at               datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

	const createADDataQualityAggregations = `
CREATE TABLE IF NOT EXISTS ad_data_quality_aggregations (
    id                       integer PRIMARY KEY AUTOINCREMENT,
    domains                  integer NOT NULL DEFAULT 0,
    users                    integer NOT NULL DEFAULT 0,
    groups                   integer NOT NULL DEFAULT 0,
    computers                integer NOT NULL DEFAULT 0,
    ous                      integer NOT NULL DEFAULT 0,
    containers               integer NOT NULL DEFAULT 0,
    gpos                     integer NOT NULL DEFAULT 0,
    aiacas                   integer NOT NULL DEFAULT 0,
    rootcas                  integer NOT NULL DEFAULT 0,
    enterprisecas            integer NOT NULL DEFAULT 0,
    ntauthstores             integer NOT NULL DEFAULT 0,
    certtemplates            integer NOT NULL DEFAULT 0,
    issuancepolicies         integer NOT NULL DEFAULT 0,
    acls                     integer NOT NULL DEFAULT 0,
    sessions                 integer NOT NULL DEFAULT 0,
    relationships            integer NOT NULL DEFAULT 0,
    session_completeness     real    NOT NULL DEFAULT 0,
    local_group_completeness real    NOT NULL DEFAULT 0,
    run_id                   text    NOT NULL DEFAULT '',
    created_at               datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at               datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

	const createAzureDataQualityStats = `
CREATE TABLE IF NOT EXISTS azure_data_quality_stats (
    id                  integer PRIMARY KEY AUTOINCREMENT,
    tenant_id           text    NOT NULL DEFAULT '',
    relationships       integer NOT NULL DEFAULT 0,
    users               integer NOT NULL DEFAULT 0,
    groups              integer NOT NULL DEFAULT 0,
    apps                integer NOT NULL DEFAULT 0,
    service_principals  integer NOT NULL DEFAULT 0,
    devices             integer NOT NULL DEFAULT 0,
    management_groups   integer NOT NULL DEFAULT 0,
    subscriptions       integer NOT NULL DEFAULT 0,
    resource_groups     integer NOT NULL DEFAULT 0,
    vms                 integer NOT NULL DEFAULT 0,
    key_vaults          integer NOT NULL DEFAULT 0,
    automation_accounts integer NOT NULL DEFAULT 0,
    container_registries integer NOT NULL DEFAULT 0,
    function_apps       integer NOT NULL DEFAULT 0,
    logic_apps          integer NOT NULL DEFAULT 0,
    managed_clusters    integer NOT NULL DEFAULT 0,
    vm_scale_sets       integer NOT NULL DEFAULT 0,
    web_apps            integer NOT NULL DEFAULT 0,
    run_id              text    NOT NULL DEFAULT '',
    created_at          datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

	const createAzureDataQualityAggregations = `
CREATE TABLE IF NOT EXISTS azure_data_quality_aggregations (
    id                  integer PRIMARY KEY AUTOINCREMENT,
    tenants             integer NOT NULL DEFAULT 0,
    relationships       integer NOT NULL DEFAULT 0,
    users               integer NOT NULL DEFAULT 0,
    groups              integer NOT NULL DEFAULT 0,
    apps                integer NOT NULL DEFAULT 0,
    service_principals  integer NOT NULL DEFAULT 0,
    devices             integer NOT NULL DEFAULT 0,
    management_groups   integer NOT NULL DEFAULT 0,
    subscriptions       integer NOT NULL DEFAULT 0,
    resource_groups     integer NOT NULL DEFAULT 0,
    vms                 integer NOT NULL DEFAULT 0,
    key_vaults          integer NOT NULL DEFAULT 0,
    automation_accounts integer NOT NULL DEFAULT 0,
    container_registries integer NOT NULL DEFAULT 0,
    function_apps       integer NOT NULL DEFAULT 0,
    logic_apps          integer NOT NULL DEFAULT 0,
    managed_clusters    integer NOT NULL DEFAULT 0,
    vm_scale_sets       integer NOT NULL DEFAULT 0,
    web_apps            integer NOT NULL DEFAULT 0,
    run_id              text    NOT NULL DEFAULT '',
    created_at          datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

	// completed_tasks: the Errors and Warnings fields use pq.StringArray (PostgreSQL text[])
	// which doesn't work with SQLite, so we store them as JSON text.
	const createCompletedTasks = `
CREATE TABLE IF NOT EXISTS completed_tasks (
    id               integer PRIMARY KEY AUTOINCREMENT,
    ingest_job_id    integer NOT NULL DEFAULT 0,
    file_name        text    NOT NULL DEFAULT '',
    parent_file_name text    NOT NULL DEFAULT '',
    errors           text    NOT NULL DEFAULT '[]',
    warnings         text    NOT NULL DEFAULT '[]',
    created_at       datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

	for _, stmt := range []string{createDatapipeStatus, seedDatapipeStatus, createAnalysisRequestSwitch, createCustomNodeKinds, createSchemaNodeKinds, createSchemaRelationshipKinds, createSchemaEnvironments, createSourceKinds, createADDataQualityStats, createADDataQualityAggregations, createAzureDataQualityStats, createAzureDataQualityAggregations, createCompletedTasks} {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}

	if err := seedRolesAndPermissions(db); err != nil {
		return err
	}
	return seedFeatureFlags(db)
}

// seedRolesAndPermissions populates the permissions, roles, and roles_permissions tables with
// the canonical set of roles/permissions defined in the auth package. This is idempotent —
// existing rows are left untouched (FirstOrCreate semantics).
func seedRolesAndPermissions(db *gorm.DB) error {
	// Ensure all permissions exist
	for _, p := range auth.Permissions().All() {
		perm := model.Permission{
			Authority: p.Authority,
			Name:      p.Name,
		}
		if result := db.Where(model.Permission{Authority: p.Authority, Name: p.Name}).
			FirstOrCreate(&perm); result.Error != nil {
			return result.Error
		}
	}

	// Ensure all roles exist (with their permission associations)
	for _, roleTemplate := range auth.Roles() {
		// Resolve the Permission rows for this role
		var permissions []model.Permission
		for _, p := range roleTemplate.Permissions {
			var perm model.Permission
			if result := db.Where("authority = ? AND name = ?", p.Authority, p.Name).First(&perm); result.Error != nil {
				return result.Error
			}
			permissions = append(permissions, perm)
		}

		role := model.Role{
			Name:        roleTemplate.Name,
			Description: roleTemplate.Description,
		}
		if result := db.Where(model.Role{Name: roleTemplate.Name}).
			Assign(model.Role{Description: roleTemplate.Description}).
			FirstOrCreate(&role); result.Error != nil {
			return result.Error
		}

		// Sync permission associations (append only; won't duplicate due to many2many uniqueness)
		if err := db.Model(&role).Association("Permissions").Append(permissions); err != nil {
			return err
		}
	}

	return nil
}

// seedFeatureFlags populates the feature_flags table with the canonical set of flags and their
// default values. This mirrors the inserts spread across the PostgreSQL versioned migration files.
// It is idempotent — existing rows (including any user-modified enabled state) are left untouched.
func seedFeatureFlags(db *gorm.DB) error {
	flags := []appcfg.FeatureFlag{
		{Key: appcfg.FeatureButterflyAnalysis, Name: "Enhanced Asset Inbound-Outbound Exposure Analysis", Description: "Enables more extensive analysis of attack path findings that allows BloodHound to help the user prioritize remediation of the most exposed assets.", Enabled: false, UserUpdatable: false},
		{Key: appcfg.FeatureEnableSAMLSSO, Name: "SAML Single Sign-On Support", Description: "Enables SSO authentication flows and administration panels to third party SAML identity providers.", Enabled: true, UserUpdatable: false},
		{Key: appcfg.FeatureScopeCollectionByOU, Name: "Enable SharpHound OU Scoped Collections", Description: "Enables scoping SharpHound collections to specific lists of OUs.", Enabled: true, UserUpdatable: false},
		{Key: appcfg.FeatureAzureSupport, Name: "Enable Azure Support", Description: "Enables Azure support.", Enabled: true, UserUpdatable: false},
		{Key: appcfg.FeatureEntityPanelCaching, Name: "Enable application level caching", Description: "Enables the use of application level caching for entity panel queries.", Enabled: true, UserUpdatable: false},
		{Key: appcfg.FeatureAdcs, Name: "Enable collection and processing of Active Directory Certificate Services Data", Description: "Enables the ability to collect, analyze, and explore Active Directory Certificate Services data and previews new attack paths.", Enabled: false, UserUpdatable: false},
		{Key: appcfg.FeatureClearGraphData, Name: "Clear Graph Data", Description: "Enables the ability to delete all nodes and edges from the graph database.", Enabled: true, UserUpdatable: false},
		{Key: appcfg.FeatureRiskExposureNewCalculation, Name: "Use new tier zero risk exposure calculation", Description: "Enables the use of new tier zero risk exposure metatree metrics.", Enabled: false, UserUpdatable: false},
		{Key: appcfg.FeatureFedRAMPEULA, Name: "FedRAMP EULA", Description: "Enables showing the FedRAMP EULA on every login. (Enterprise only)", Enabled: false, UserUpdatable: false},
		{Key: appcfg.FeatureDarkMode, Name: "Dark Mode", Description: "Allows users to enable or disable dark mode via a toggle in the settings menu.", Enabled: true, UserUpdatable: false},
		{Key: appcfg.FeatureAutoTagT0ParentObjects, Name: "Automatically add parent OUs and containers of Tier Zero AD objects to Tier Zero", Description: "Parent OUs and containers of Tier Zero AD objects are automatically added to Tier Zero during analysis.", Enabled: true, UserUpdatable: true},
		{Key: appcfg.FeatureOIDCSupport, Name: "OIDC Support", Description: "Enables OIDC authentication support.", Enabled: false, UserUpdatable: false},
		{Key: appcfg.FeatureNTLMPostProcessing, Name: "NTLM Post Processing", Description: "Enables NTLM post processing.", Enabled: true, UserUpdatable: false},
		{Key: appcfg.FeatureTierManagement, Name: "Tier Management Engine", Description: "Enables the tier management engine.", Enabled: false, UserUpdatable: false},
		{Key: appcfg.FeatureChangelog, Name: "Changelog", Description: "This flag allows the application to query the changelog daemon for deduplication of ingest payloads.", Enabled: false, UserUpdatable: false},
		{Key: appcfg.FeatureETAC, Name: "Environment Targeted Access Control", Description: "Enables environment targeted access control.", Enabled: false, UserUpdatable: false},
		{Key: appcfg.FeatureOpenGraphSearch, Name: "Open Graph Search", Description: "Enables open graph search.", Enabled: true, UserUpdatable: false},
		{Key: appcfg.FeatureOpenGraphFindings, Name: "Open Graph Findings", Description: "Enables open graph findings.", Enabled: false, UserUpdatable: false},
		{Key: appcfg.FeatureClientBearerAuth, Name: "Client Bearer Auth", Description: "Enables client bearer auth.", Enabled: false, UserUpdatable: false},
		{Key: appcfg.FeatureOpenGraphExtensionManagement, Name: "Open Graph Extension Management", Description: "Enables open graph extension management.", Enabled: false, UserUpdatable: false},
		{Key: appcfg.FeatureOGCollectorPlatformSupport, Name: "Open Graph Collector Platform Support", Description: "Enables open graph collector platform support.", Enabled: false, UserUpdatable: false},
		{Key: appcfg.FeatureOpenGraphPhase2, Name: "Open Graph Phase 2", Description: "Open Graph Phase 2 features", Enabled: true, UserUpdatable: false},
	}

	for _, f := range flags {
		flag := f
		if result := db.Where(appcfg.FeatureFlag{Key: flag.Key}).FirstOrCreate(&flag); result.Error != nil {
			return result.Error
		}
	}

	return nil
}
