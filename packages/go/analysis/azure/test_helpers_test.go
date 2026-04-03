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

//go:build standalone

package azure_test

import (
	"context"
	"fmt"
	"testing"

	azschema "github.com/specterops/bloodhound/packages/go/graphschema/azure"
	"github.com/specterops/bloodhound/packages/go/graphschema/common"
	kglitedawgs "github.com/specterops/bloodhound/packages/go/kglite/dawgs"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/ops"
	"github.com/specterops/dawgs/query"
	"github.com/stretchr/testify/require"
)

// openTestGraph opens an in-memory kglite graph and registers cleanup to close it.
// It returns the graph.Database interface used by the azure analysis package.
func openTestGraph(t *testing.T) graph.Database {
	t.Helper()
	d, err := kglitedawgs.Open("")
	if err != nil {
		t.Fatalf("kglitedawgs.Open(\"\") failed: %v", err)
	}
	t.Cleanup(func() {
		if err := d.Close(context.Background()); err != nil {
			t.Errorf("Close failed: %v", err)
		}
	})
	return d
}

// ─── Node creation helpers ────────────────────────────────────────────────────

// createTenant creates an AZTenant node with the given objectID and name.
// The tenant is marked as collected so FetchCollectedTenants will return it.
func createTenant(t *testing.T, db graph.Database, objectID, name string) *graph.Node {
	t.Helper()
	var node *graph.Node
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String():  objectID,
			common.Name.String():      name,
			common.Collected.String(): true,
		})
		var err error
		node, err = tx.CreateNode(props, azschema.Entity, azschema.Tenant)
		return err
	}))
	return node
}

// createUser creates an AZUser node belonging to the given tenantID.
func createUser(t *testing.T, db graph.Database, tenantID, objectID, name string) *graph.Node {
	t.Helper()
	var node *graph.Node
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): objectID,
			common.Name.String():     name,
			azschema.TenantID.String(): tenantID,
		})
		var err error
		node, err = tx.CreateNode(props, azschema.Entity, azschema.User)
		return err
	}))
	return node
}

// createGroup creates an AZGroup node. isRoleAssignable controls the
// IsAssignableToRole property used in role-descent filtering.
func createGroup(t *testing.T, db graph.Database, objectID, name string, isRoleAssignable bool) *graph.Node {
	t.Helper()
	var node *graph.Node
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String():             objectID,
			common.Name.String():                 name,
			azschema.IsAssignableToRole.String(): isRoleAssignable,
		})
		var err error
		node, err = tx.CreateNode(props, azschema.Entity, azschema.Group)
		return err
	}))
	return node
}

// createServicePrincipal creates an AZServicePrincipal node.
func createServicePrincipal(t *testing.T, db graph.Database, objectID, name string) *graph.Node {
	t.Helper()
	var node *graph.Node
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): objectID,
			common.Name.String():     name,
		})
		var err error
		node, err = tx.CreateNode(props, azschema.Entity, azschema.ServicePrincipal)
		return err
	}))
	return node
}

// createApplication creates an AZApp node.
func createApplication(t *testing.T, db graph.Database, objectID, name string) *graph.Node {
	t.Helper()
	var node *graph.Node
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): objectID,
			common.Name.String():     name,
		})
		var err error
		node, err = tx.CreateNode(props, azschema.Entity, azschema.App)
		return err
	}))
	return node
}

// createRole creates an AZRole node with the given templateID (roleTemplateID property).
// The tenantID should be set so TenantRoles can locate the role.
func createRole(t *testing.T, db graph.Database, objectID, name, templateID string) *graph.Node {
	t.Helper()
	var node *graph.Node
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String():          objectID,
			common.Name.String():              name,
			azschema.RoleTemplateID.String():  templateID,
		})
		var err error
		node, err = tx.CreateNode(props, azschema.Entity, azschema.Role)
		return err
	}))
	return node
}

// createDevice creates an AZDevice node.
func createDevice(t *testing.T, db graph.Database, objectID, name string) *graph.Node {
	t.Helper()
	var node *graph.Node
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): objectID,
			common.Name.String():     name,
		})
		var err error
		node, err = tx.CreateNode(props, azschema.Entity, azschema.Device)
		return err
	}))
	return node
}

// createKeyVault creates an AZKeyVault node.
func createKeyVault(t *testing.T, db graph.Database, objectID, name string) *graph.Node {
	t.Helper()
	var node *graph.Node
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): objectID,
			common.Name.String():     name,
		})
		var err error
		node, err = tx.CreateNode(props, azschema.Entity, azschema.KeyVault)
		return err
	}))
	return node
}

// createManagementGroup creates an AZManagementGroup node.
func createManagementGroup(t *testing.T, db graph.Database, objectID, name string) *graph.Node {
	t.Helper()
	var node *graph.Node
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): objectID,
			common.Name.String():     name,
		})
		var err error
		node, err = tx.CreateNode(props, azschema.Entity, azschema.ManagementGroup)
		return err
	}))
	return node
}

// createSubscription creates an AZSubscription node.
func createSubscription(t *testing.T, db graph.Database, objectID, name string) *graph.Node {
	t.Helper()
	var node *graph.Node
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): objectID,
			common.Name.String():     name,
		})
		var err error
		node, err = tx.CreateNode(props, azschema.Entity, azschema.Subscription)
		return err
	}))
	return node
}

// createResourceGroup creates an AZResourceGroup node.
func createResourceGroup(t *testing.T, db graph.Database, objectID, name string) *graph.Node {
	t.Helper()
	var node *graph.Node
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): objectID,
			common.Name.String():     name,
		})
		var err error
		node, err = tx.CreateNode(props, azschema.Entity, azschema.ResourceGroup)
		return err
	}))
	return node
}

// createVM creates an AZVM node.
func createVM(t *testing.T, db graph.Database, objectID, name string) *graph.Node {
	t.Helper()
	var node *graph.Node
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): objectID,
			common.Name.String():     name,
		})
		var err error
		node, err = tx.CreateNode(props, azschema.Entity, azschema.VM)
		return err
	}))
	return node
}

// ─── Relationship helpers ─────────────────────────────────────────────────────

// createRel creates a directed relationship of the given kind from startNode to endNode.
func createRel(t *testing.T, db graph.Database, startNode, endNode *graph.Node, kind graph.Kind) *graph.Relationship {
	t.Helper()
	var rel *graph.Relationship
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		var err error
		rel, err = tx.CreateRelationshipByIDs(startNode.ID, endNode.ID, kind, nil)
		return err
	}))
	return rel
}

// ─── Assertion helpers ────────────────────────────────────────────────────────

// requireRelExists asserts that at least one relationship of the given kind exists
// between startID and endID.
func requireRelExists(t *testing.T, db graph.Database, startID, endID graph.ID, kind graph.Kind) {
	t.Helper()
	var found bool
	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				query.Equals(query.StartID(), startID),
				query.Equals(query.EndID(), endID),
				query.Kind(query.Relationship(), kind),
			)
		}))
		if err != nil {
			return err
		}
		found = len(rels) > 0
		return nil
	}))
	require.True(t, found, "expected relationship %s from %d to %d to exist", kind.String(), startID, endID)
}

// requireRelNotExists asserts that no relationship of the given kind exists
// between startID and endID.
func requireRelNotExists(t *testing.T, db graph.Database, startID, endID graph.ID, kind graph.Kind) {
	t.Helper()
	var found bool
	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				query.Equals(query.StartID(), startID),
				query.Equals(query.EndID(), endID),
				query.Kind(query.Relationship(), kind),
			)
		}))
		if err != nil {
			return err
		}
		found = len(rels) > 0
		return nil
	}))
	require.False(t, found, "expected no relationship %s from %d to %d, but one exists", kind.String(), startID, endID)
}

// countNodes returns the number of nodes with the given kind label.
func countNodes(t *testing.T, db graph.Database, kind graph.Kind) int {
	t.Helper()
	var count int
	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		nodes, err := ops.FetchNodeSet(tx.Nodes().Filterf(func() graph.Criteria {
			return query.Kind(query.Node(), kind)
		}))
		if err != nil {
			return err
		}
		count = nodes.Len()
		return nil
	}))
	return count
}

// countRels returns the number of relationships with the given kind label.
func countRels(t *testing.T, db graph.Database, kind graph.Kind) int {
	t.Helper()
	var count int
	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.Kind(query.Relationship(), kind)
		}))
		if err != nil {
			return err
		}
		count = len(rels)
		return nil
	}))
	return count
}

// ─── Standard seeded graph ────────────────────────────────────────────────────

// AzureTestGraph holds a seeded in-memory Azure graph for use in tests.
type AzureTestGraph struct {
	DB               graph.Database
	Tenant           *graph.Node
	Users            []*graph.Node
	Groups           []*graph.Node
	ServicePrincipals []*graph.Node
	Application      *graph.Node
	Roles            []*graph.Node
}

// TenantObjectID is the stable objectID for the test tenant.
const TenantObjectID = "test-tenant-0001"

// seedAzureGraph creates a standard test graph with:
//   - 1 tenant (collected=true)
//   - 3 users
//   - 2 groups (groups[1] is role-assignable)
//   - 2 service principals
//   - 1 application
//   - 3 roles (GlobalAdministrator, UserAccountAdministrator, HelpdeskAdministrator)
//
// Relationships wired up:
//   - Tenant -[Contains]-> each user, group, service principal
//   - Tenant -[Contains]-> each role
//   - users[0] -[HasRole]-> roles[0] (GlobalAdmin)
//   - users[1] -[HasRole]-> roles[1] (UserAccountAdmin)
//   - users[2] -[MemberOf]-> groups[1] (role-assignable group)
//   - groups[1] -[HasRole]-> roles[2] (HelpdeskAdmin)
//   - servicePrincipals[0] -[HasRole]-> roles[0] (GlobalAdmin)
func seedAzureGraph(t *testing.T) *AzureTestGraph {
	t.Helper()

	db := openTestGraph(t)

	tenant := createTenant(t, db, TenantObjectID, "Test Tenant")

	users := []*graph.Node{
		createUser(t, db, TenantObjectID, "user-0001", "Alice"),
		createUser(t, db, TenantObjectID, "user-0002", "Bob"),
		createUser(t, db, TenantObjectID, "user-0003", "Carol"),
	}

	groups := []*graph.Node{
		createGroup(t, db, "group-0001", "Regular Group", false),
		createGroup(t, db, "group-0002", "Role Assignable Group", true),
	}

	sps := []*graph.Node{
		createServicePrincipal(t, db, "sp-0001", "Service Principal 1"),
		createServicePrincipal(t, db, "sp-0002", "Service Principal 2"),
	}

	app := createApplication(t, db, "app-0001", "Test Application")

	roles := []*graph.Node{
		createRole(t, db, fmt.Sprintf("%s/%s", TenantObjectID, azschema.CompanyAdministratorRole), "Global Administrator", azschema.CompanyAdministratorRole),
		createRole(t, db, fmt.Sprintf("%s/%s", TenantObjectID, azschema.UserAccountAdministratorRole), "User Account Administrator", azschema.UserAccountAdministratorRole),
		createRole(t, db, fmt.Sprintf("%s/%s", TenantObjectID, azschema.HelpdeskAdministratorRole), "Helpdesk Administrator", azschema.HelpdeskAdministratorRole),
	}

	// Wire Tenant -[Contains]-> users, groups, service principals, roles
	for _, u := range users {
		createRel(t, db, tenant, u, azschema.Contains)
	}
	for _, g := range groups {
		createRel(t, db, tenant, g, azschema.Contains)
	}
	for _, sp := range sps {
		createRel(t, db, tenant, sp, azschema.Contains)
	}
	for _, r := range roles {
		createRel(t, db, tenant, r, azschema.Contains)
	}

	// Direct role assignments
	createRel(t, db, users[0], roles[0], azschema.HasRole)    // Alice -> GlobalAdmin
	createRel(t, db, users[1], roles[1], azschema.HasRole)    // Bob -> UserAccountAdmin
	createRel(t, db, sps[0], roles[0], azschema.HasRole)      // SP1 -> GlobalAdmin

	// Role-assignable group membership
	createRel(t, db, users[2], groups[1], azschema.MemberOf)  // Carol -> role-assignable group
	createRel(t, db, groups[1], roles[2], azschema.HasRole)   // role-assignable group -> HelpdeskAdmin

	return &AzureTestGraph{
		DB:                db,
		Tenant:            tenant,
		Users:             users,
		Groups:            groups,
		ServicePrincipals: sps,
		Application:       app,
		Roles:             roles,
	}
}

// ─── Smoke test ───────────────────────────────────────────────────────────────

func TestSeedAzureGraph(t *testing.T) {
	g := seedAzureGraph(t)

	require.NotNil(t, g.Tenant)
	require.Len(t, g.Users, 3)
	require.Len(t, g.Groups, 2)
	require.Len(t, g.ServicePrincipals, 2)
	require.NotNil(t, g.Application)
	require.Len(t, g.Roles, 3)

	// Verify node counts in the graph
	require.Equal(t, 1, countNodes(t, g.DB, azschema.Tenant))
	require.Equal(t, 3, countNodes(t, g.DB, azschema.User))
	require.Equal(t, 2, countNodes(t, g.DB, azschema.Group))
	require.Equal(t, 2, countNodes(t, g.DB, azschema.ServicePrincipal))
	require.Equal(t, 1, countNodes(t, g.DB, azschema.App))
	require.Equal(t, 3, countNodes(t, g.DB, azschema.Role))

	// Verify relationship counts
	// Contains: 3 users + 2 groups + 2 SPs + 3 roles = 10
	require.Equal(t, 10, countRels(t, g.DB, azschema.Contains))
	// HasRole: Alice->GlobalAdmin, Bob->UserAccountAdmin, SP1->GlobalAdmin, group->HelpdeskAdmin = 4
	require.Equal(t, 4, countRels(t, g.DB, azschema.HasRole))
	// MemberOf: Carol->role-assignable group = 1
	require.Equal(t, 1, countRels(t, g.DB, azschema.MemberOf))

	// Verify specific relationships
	requireRelExists(t, g.DB, g.Users[0].ID, g.Roles[0].ID, azschema.HasRole)
	requireRelExists(t, g.DB, g.Users[1].ID, g.Roles[1].ID, azschema.HasRole)
	requireRelExists(t, g.DB, g.ServicePrincipals[0].ID, g.Roles[0].ID, azschema.HasRole)
	requireRelExists(t, g.DB, g.Users[2].ID, g.Groups[1].ID, azschema.MemberOf)
	requireRelExists(t, g.DB, g.Groups[1].ID, g.Roles[2].ID, azschema.HasRole)

	requireRelNotExists(t, g.DB, g.Users[2].ID, g.Roles[0].ID, azschema.HasRole)
}
