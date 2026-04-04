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

	for _, stmt := range []string{createDatapipeStatus, seedDatapipeStatus, createAnalysisRequestSwitch} {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}

	return seedRolesAndPermissions(db)
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

		role := model.Role{}
		if result := db.Where("name = ?", roleTemplate.Name).
			Attrs(model.Role{Description: roleTemplate.Description}).
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
