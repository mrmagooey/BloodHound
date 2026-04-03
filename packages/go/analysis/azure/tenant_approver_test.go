//go:build standalone

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

package azure_test

import (
	"context"
	"testing"

	"github.com/specterops/bloodhound/packages/go/analysis"
	"github.com/specterops/bloodhound/packages/go/analysis/azure"
	azschema "github.com/specterops/bloodhound/packages/go/graphschema/azure"
	"github.com/specterops/bloodhound/packages/go/graphschema/common"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// tenant.go Tests
// ============================================================================

func TestTenantEntityDetails_WithoutCounts(t *testing.T) {
	g := seedAzureGraph(t)

	details, err := azure.TenantEntityDetails(context.Background(), g.DB, nil, TenantObjectID, false)
	require.NoError(t, err)

	assert.NotNil(t, details.Node)
	assert.Equal(t, "Test Tenant", details.Node.Properties[common.Name.String()])
	assert.Equal(t, TenantObjectID, details.Node.Properties[common.ObjectID.String()])
	// When hydrateCounts=false, Descendents should be empty
	assert.Equal(t, 0, len(details.Descendents.DescendentCounts))
	assert.Equal(t, 0, details.InboundObjectControl)
}

func TestTenantEntityDetails_WithCounts(t *testing.T) {
	g := seedAzureGraph(t)

	details, err := azure.TenantEntityDetails(context.Background(), g.DB, nil, TenantObjectID, true)
	require.NoError(t, err)

	assert.NotNil(t, details.Node)
	assert.Equal(t, "Test Tenant", details.Node.Properties[common.Name.String()])
	// With hydrateCounts=true, Descendents should be populated
	assert.NotNil(t, details.Descendents.DescendentCounts)
	// Should have counts for users, groups, service principals, roles
	assert.Greater(t, len(details.Descendents.DescendentCounts), 0)
}

func TestTenantEntityDetails_InvalidObjectID(t *testing.T) {
	g := seedAzureGraph(t)

	details, err := azure.TenantEntityDetails(context.Background(), g.DB, nil, "nonexistent-id", false)
	require.Error(t, err)
	assert.True(t, graph.IsErrNotFound(err))
	assert.Equal(t, 0, len(details.Node.Properties))
}

func TestPopulateTenantEntityDetailsCounts(t *testing.T) {
	g := seedAzureGraph(t)

	var details azure.TenantDetails
	require.NoError(t, g.DB.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		var err error
		details, err = azure.PopulateTenantEntityDetailsCounts(tx, g.Tenant, details)
		return err
	}))

	// Verify that descendents were populated
	assert.NotNil(t, details.Descendents.DescendentCounts)
	// Check that we have counts for various kinds
	// seedAzureGraph creates: 3 users, 2 groups, 2 service principals, 3 roles
	assert.Greater(t, len(details.Descendents.DescendentCounts), 0)

	// Users (3) should be counted
	userCount, ok := details.Descendents.DescendentCounts[azschema.User.String()]
	assert.True(t, ok, "user count should be in descendents")
	assert.Equal(t, 3, userCount)

	// Groups (2) should be counted
	groupCount, ok := details.Descendents.DescendentCounts[azschema.Group.String()]
	assert.True(t, ok, "group count should be in descendents")
	assert.Equal(t, 2, groupCount)
}

func TestTenantPrincipals(t *testing.T) {
	g := seedAzureGraph(t)

	var principals graph.NodeSet
	require.NoError(t, g.DB.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		var err error
		principals, err = azure.TenantPrincipals(tx, g.Tenant)
		return err
	}))

	// seedAzureGraph creates 3 users, 2 groups, 2 service principals
	// TenantPrincipals should return all of these (7 nodes total)
	assert.Equal(t, 7, principals.Len())

	// Verify that all users, groups, and service principals are included
	userIDs := make(map[graph.ID]bool)
	groupIDs := make(map[graph.ID]bool)
	spIDs := make(map[graph.ID]bool)

	for _, user := range g.Users {
		userIDs[user.ID] = true
	}
	for _, group := range g.Groups {
		groupIDs[group.ID] = true
	}
	for _, sp := range g.ServicePrincipals {
		spIDs[sp.ID] = true
	}

	for _, node := range principals {
		if _, ok := userIDs[node.ID]; ok {
			delete(userIDs, node.ID)
		} else if _, ok := groupIDs[node.ID]; ok {
			delete(groupIDs, node.ID)
		} else if _, ok := spIDs[node.ID]; ok {
			delete(spIDs, node.ID)
		}
	}

	// All should have been found
	assert.Equal(t, 0, len(userIDs), "all users should be in principals")
	assert.Equal(t, 0, len(groupIDs), "all groups should be in principals")
	assert.Equal(t, 0, len(spIDs), "all service principals should be in principals")
}

func TestTenantPrincipals_NonTenantNode(t *testing.T) {
	g := seedAzureGraph(t)

	var err error
	require.NoError(t, g.DB.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// Use a user node instead of tenant node
		_, err = azure.TenantPrincipals(tx, g.Users[0])
		return nil
	}))

	// Should return error because Users[0] is not a tenant
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must contain kind")
}

func TestTenantApplicationsAndServicePrincipals(t *testing.T) {
	g := seedAzureGraph(t)

	var appsAndSPs graph.NodeSet
	require.NoError(t, g.DB.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		var err error
		appsAndSPs, err = azure.TenantApplicationsAndServicePrincipals(tx, g.Tenant)
		return err
	}))

	// seedAzureGraph creates 2 service principals that have Contains relationships with tenant
	// The application doesn't have a Contains relationship in the seeded graph
	assert.Equal(t, 2, appsAndSPs.Len())

	// Verify that service principals are included
	spFound := 0
	for _, node := range appsAndSPs {
		for _, sp := range g.ServicePrincipals {
			if node.ID == sp.ID {
				spFound++
			}
		}
	}

	assert.Equal(t, 2, spFound, "all service principals should be in apps and service principals")
}

func TestTenantApplicationsAndServicePrincipals_NonTenantNode(t *testing.T) {
	g := seedAzureGraph(t)

	var err error
	require.NoError(t, g.DB.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// Use a group node instead of tenant node
		_, err = azure.TenantApplicationsAndServicePrincipals(tx, g.Groups[0])
		return nil
	}))

	// Should return error because Groups[0] is not a tenant
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must contain kind")
}

// ============================================================================
// role_approver.go Tests
// ============================================================================

func TestCreateApproverEdge_NoApproversConfigured(t *testing.T) {
	g := seedAzureGraph(t)
	ctx := context.Background()

	// Create a role that requires approval but has no specific approvers
	// This should create edges to default admin roles
	var role *graph.Node
	require.NoError(t, g.DB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String():                                TenantObjectID + "/test-role",
			common.Name.String():                                   "Test Role",
			azschema.TenantID.String():                            TenantObjectID,
			azschema.RoleTemplateID.String():                      "test-template-id",
			azschema.EndUserAssignmentRequiresApproval.String():   true,
			azschema.EndUserAssignmentUserApprovers.String():      nil,
			azschema.EndUserAssignmentGroupApprovers.String():     nil,
		})
		var err error
		role, err = tx.CreateNode(props, azschema.Entity, azschema.Role)
		return err
	}))

	// Create the Contains relationship
	require.NoError(t, g.DB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateRelationshipByIDs(g.Tenant.ID, role.ID, azschema.Contains, nil)
		return err
	}))

	// Create a real operation for tracking
	operation := analysis.NewPostRelationshipOperation(ctx, g.DB, "test-approver-edge")
	defer operation.Done()

	// Call CreateApproverEdge - it should not error
	err := azure.CreateApproverEdge(ctx, g.DB, g.Tenant, operation)
	require.NoError(t, err)

	// The function executes asynchronously, but we've verified it doesn't error
	// In a real scenario, the operation would create the edges in the graph
}

func TestCreateApproverEdge_WithSpecificApprovers(t *testing.T) {
	g := seedAzureGraph(t)
	ctx := context.Background()

	// Create a role that requires approval with specific user approvers
	approverUserID := "user-approver-001"
	_ = createUser(t, g.DB, TenantObjectID, approverUserID, "Approver User")

	var role *graph.Node
	require.NoError(t, g.DB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String():                              TenantObjectID + "/test-role-specific",
			common.Name.String():                                 "Test Role With Approvers",
			azschema.TenantID.String():                          TenantObjectID,
			azschema.RoleTemplateID.String():                    "test-template-specific",
			azschema.EndUserAssignmentRequiresApproval.String(): true,
			azschema.EndUserAssignmentUserApprovers.String():    []string{approverUserID},
			azschema.EndUserAssignmentGroupApprovers.String():   nil,
		})
		var err error
		role, err = tx.CreateNode(props, azschema.Entity, azschema.Role)
		return err
	}))

	// Create the Contains relationship
	require.NoError(t, g.DB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateRelationshipByIDs(g.Tenant.ID, role.ID, azschema.Contains, nil)
		return err
	}))

	// Create operation for tracking edge creation
	operation := analysis.NewPostRelationshipOperation(ctx, g.DB, "test-approver-edge-specific")
	defer operation.Done()

	err := azure.CreateApproverEdge(ctx, g.DB, g.Tenant, operation)
	require.NoError(t, err, "should successfully process role with specific approvers")
}

func TestCreateApproverEdge_WithGroupApprovers(t *testing.T) {
	g := seedAzureGraph(t)
	ctx := context.Background()

	// Create a group that will be an approver
	approverGroupID := "group-approver-001"
	_ = createGroup(t, g.DB, approverGroupID, "Approver Group", false)

	// Create a role that requires approval with specific group approvers
	var role *graph.Node
	require.NoError(t, g.DB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String():                              TenantObjectID + "/test-role-group-approvers",
			common.Name.String():                                 "Test Role With Group Approvers",
			azschema.TenantID.String():                          TenantObjectID,
			azschema.RoleTemplateID.String():                    "test-template-group",
			azschema.EndUserAssignmentRequiresApproval.String(): true,
			azschema.EndUserAssignmentUserApprovers.String():    nil,
			azschema.EndUserAssignmentGroupApprovers.String():   []string{approverGroupID},
		})
		var err error
		role, err = tx.CreateNode(props, azschema.Entity, azschema.Role)
		return err
	}))

	// Create the Contains relationship
	require.NoError(t, g.DB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateRelationshipByIDs(g.Tenant.ID, role.ID, azschema.Contains, nil)
		return err
	}))

	// Create operation for tracking edge creation
	operation := analysis.NewPostRelationshipOperation(ctx, g.DB, "test-approver-edge-group")
	defer operation.Done()

	err := azure.CreateApproverEdge(ctx, g.DB, g.Tenant, operation)
	require.NoError(t, err, "should successfully process role with group approvers")
}

func TestCreateApproverEdge_NoRolesRequiringApproval(t *testing.T) {
	g := seedAzureGraph(t)
	ctx := context.Background()

	// Create operation for tracking edge creation
	operation := analysis.NewPostRelationshipOperation(ctx, g.DB, "test-no-approval")
	defer operation.Done()

	// The seeded graph's roles don't require approval, so no edges should be created
	err := azure.CreateApproverEdge(ctx, g.DB, g.Tenant, operation)
	require.NoError(t, err, "should handle tenants with no roles requiring approval")
}

func TestCreateApproverEdge_MissingApproverNode(t *testing.T) {
	g := seedAzureGraph(t)
	ctx := context.Background()

	// Create a role with an approver ID that doesn't exist in the graph
	nonexistentApproverID := "nonexistent-approver-id"

	var role *graph.Node
	require.NoError(t, g.DB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String():                              TenantObjectID + "/test-role-missing-approver",
			common.Name.String():                                 "Test Role",
			azschema.TenantID.String():                          TenantObjectID,
			azschema.RoleTemplateID.String():                    "test-template-missing",
			azschema.EndUserAssignmentRequiresApproval.String(): true,
			azschema.EndUserAssignmentUserApprovers.String():    []string{nonexistentApproverID},
			azschema.EndUserAssignmentGroupApprovers.String():   nil,
		})
		var err error
		role, err = tx.CreateNode(props, azschema.Entity, azschema.Role)
		return err
	}))

	// Create the Contains relationship
	require.NoError(t, g.DB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateRelationshipByIDs(g.Tenant.ID, role.ID, azschema.Contains, nil)
		return err
	}))

	// Create operation for tracking edge creation
	operation := analysis.NewPostRelationshipOperation(ctx, g.DB, "test-missing-approver")
	defer operation.Done()

	// Should handle missing approver gracefully (skip it)
	err := azure.CreateApproverEdge(ctx, g.DB, g.Tenant, operation)
	require.NoError(t, err, "should handle missing approver gracefully")
}

func TestCreateApproverEdge_MultipleApproversUserAndGroup(t *testing.T) {
	g := seedAzureGraph(t)
	ctx := context.Background()

	// Create a user and group approver
	approverUserID := "user-multi-approver"
	_ = createUser(t, g.DB, TenantObjectID, approverUserID, "Multi Approver User")

	approverGroupID := "group-multi-approver"
	_ = createGroup(t, g.DB, approverGroupID, "Multi Approver Group", false)

	// Create a role with both user and group approvers
	var role *graph.Node
	require.NoError(t, g.DB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String():                              TenantObjectID + "/test-role-multi-approvers",
			common.Name.String():                                 "Test Role Multi Approvers",
			azschema.TenantID.String():                          TenantObjectID,
			azschema.RoleTemplateID.String():                    "test-template-multi",
			azschema.EndUserAssignmentRequiresApproval.String(): true,
			azschema.EndUserAssignmentUserApprovers.String():    []string{approverUserID},
			azschema.EndUserAssignmentGroupApprovers.String():   []string{approverGroupID},
		})
		var err error
		role, err = tx.CreateNode(props, azschema.Entity, azschema.Role)
		return err
	}))

	// Create the Contains relationship
	require.NoError(t, g.DB.WriteTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.CreateRelationshipByIDs(g.Tenant.ID, role.ID, azschema.Contains, nil)
		return err
	}))

	// Create operation for tracking edge creation
	operation := analysis.NewPostRelationshipOperation(ctx, g.DB, "test-multi-approvers")
	defer operation.Done()

	err := azure.CreateApproverEdge(ctx, g.DB, g.Tenant, operation)
	require.NoError(t, err, "should successfully process role with multiple approvers")
}

func TestCreateApproverEdge_WithUserNode(t *testing.T) {
	g := seedAzureGraph(t)
	ctx := context.Background()

	// Create operation for tracking edge creation
	operation := analysis.NewPostRelationshipOperation(ctx, g.DB, "test-user-node")
	defer operation.Done()

	// Using a user node as tenant will work (it has objectid property)
	// but will find no matching roles since roles are keyed by tenant.objectid
	// in the tenantId property
	err := azure.CreateApproverEdge(ctx, g.DB, g.Users[0], operation)
	require.NoError(t, err, "should not error when called with user node, just find no matching roles")
}

// ============================================================================
// Integration Tests Combining Both Functions
// ============================================================================

func TestTenantDetailsAndPrincipals(t *testing.T) {
	g := seedAzureGraph(t)

	// Get tenant details with counts
	details, err := azure.TenantEntityDetails(context.Background(), g.DB, nil, TenantObjectID, true)
	require.NoError(t, err)

	// Get principals in the tenant
	var principals graph.NodeSet
	require.NoError(t, g.DB.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		var err error
		principals, err = azure.TenantPrincipals(tx, g.Tenant)
		return err
	}))

	// Verify consistency: descendents should include principals
	assert.Greater(t, len(details.Descendents.DescendentCounts), 0, "should have descendent counts")

	// TenantPrincipals returns users, groups, and service principals
	// From the seeded graph: 3 users + 2 groups + 2 service principals = 7 total
	assert.Equal(t, 7, principals.Len())
}
