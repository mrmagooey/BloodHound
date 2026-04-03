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
	"fmt"
	"testing"

	"github.com/specterops/bloodhound/packages/go/analysis/azure"
	azschema "github.com/specterops/bloodhound/packages/go/graphschema/azure"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// ListEntityDescendents / ListEntityDescendentPaths
// ============================================================================

func TestListEntityDescendentPaths(t *testing.T) {
	tests := []struct {
		name              string
		relatedEntityType azure.RelatedEntityType
	}{
		{"DescendentUsers", azure.RelatedEntityTypeDescendentUsers},
		{"DescendentGroups", azure.RelatedEntityTypeDescendentGroups},
		{"DescendentServicePrincipals", azure.RelatedEntityTypeDescendentServicePrincipals},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := seedAzureGraph(t)
			_, err := azure.ListEntityDescendentPaths(context.Background(), g.DB, tc.relatedEntityType, TenantObjectID)
			require.NoError(t, err)
		})
	}
}

func TestListEntityDescendents(t *testing.T) {
	tests := []struct {
		name               string
		relatedEntityType  azure.RelatedEntityType
		expectedCount      int
	}{
		{
			name:               "DescendentUsers",
			relatedEntityType:  azure.RelatedEntityTypeDescendentUsers,
			expectedCount:      3,
		},
		{
			name:               "DescendentGroups",
			relatedEntityType:  azure.RelatedEntityTypeDescendentGroups,
			expectedCount:      2,
		},
		{
			name:               "DescendentServicePrincipals",
			relatedEntityType:  azure.RelatedEntityTypeDescendentServicePrincipals,
			expectedCount:      2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := seedAzureGraph(t)
			nodes, err := azure.ListEntityDescendents(context.Background(), g.DB, tc.relatedEntityType, TenantObjectID, 0, 0)
			require.NoError(t, err)
			require.NotNil(t, nodes)
			require.Equal(t, tc.expectedCount, nodes.Len())
		})
	}
}

func TestListEntityDescendents_InvalidType(t *testing.T) {
	g := seedAzureGraph(t)
	_, err := azure.ListEntityDescendents(context.Background(), g.DB, azure.RelatedEntityType("invalid-type"), TenantObjectID, 0, 0)
	require.Error(t, err)
	require.Equal(t, azure.ErrInvalidRelatedEntityType, err)
}

func TestListEntityDescendents_NonexistentEntity(t *testing.T) {
	g := seedAzureGraph(t)
	_, err := azure.ListEntityDescendents(context.Background(), g.DB, azure.RelatedEntityTypeDescendentUsers, "nonexistent-id", 0, 0)
	require.Error(t, err)
}

// ============================================================================
// ListEntityRoles / ListEntityRolePaths
// ============================================================================

func TestListEntityRolePaths(t *testing.T) {
	g := seedAzureGraph(t)
	userID := "user-0001"
	_, err := azure.ListEntityRolePaths(context.Background(), g.DB, userID)
	require.NoError(t, err)
}

func TestListEntityRoles(t *testing.T) {
	g := seedAzureGraph(t)
	userID := "user-0001"
	nodes, err := azure.ListEntityRoles(context.Background(), g.DB, userID, 0, 0)
	require.NoError(t, err)
	require.NotNil(t, nodes)
	require.GreaterOrEqual(t, nodes.Len(), 1)
}

func TestListEntityRoles_WithPagination(t *testing.T) {
	g := seedAzureGraph(t)
	userID := "user-0001"
	nodes1, err := azure.ListEntityRoles(context.Background(), g.DB, userID, 0, 1)
	require.NoError(t, err)
	require.NotNil(t, nodes1)
	require.Equal(t, 1, nodes1.Len())

	nodes2, err := azure.ListEntityRoles(context.Background(), g.DB, userID, 1, 1)
	require.NoError(t, err)
	require.NotNil(t, nodes2)
}

func TestListEntityRoles_NonexistentEntity(t *testing.T) {
	g := seedAzureGraph(t)
	_, err := azure.ListEntityRoles(context.Background(), g.DB, "nonexistent-id", 0, 0)
	require.Error(t, err)
}

// ============================================================================
// ListKeyVaultReaderPaths / ListKeyVaultReaders
// ============================================================================

func TestListKeyVaultReaderPaths(t *testing.T) {
	tests := []struct {
		name       string
		readerType azure.RelatedEntityType
	}{
		{"AllReaders", azure.RelatedEntityTypeVaultAllReaders},
		{"CertReaders", azure.RelatedEntityTypeVaultCertReaders},
		{"KeyReaders", azure.RelatedEntityTypeVaultKeyReaders},
		{"SecretReaders", azure.RelatedEntityTypeVaultSecretReaders},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := seedAzureGraph(t)
			kvID := "kv-0001"
			createKeyVault(t, g.DB, kvID, "Test KeyVault")
			_, err := azure.ListKeyVaultReaderPaths(context.Background(), g.DB, tc.readerType, kvID)
			require.NoError(t, err)
		})
	}
}

func TestListKeyVaultReaders(t *testing.T) {
	tests := []struct {
		name       string
		readerType azure.RelatedEntityType
	}{
		{"AllReaders", azure.RelatedEntityTypeVaultAllReaders},
		{"CertReaders", azure.RelatedEntityTypeVaultCertReaders},
		{"KeyReaders", azure.RelatedEntityTypeVaultKeyReaders},
		{"SecretReaders", azure.RelatedEntityTypeVaultSecretReaders},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := seedAzureGraph(t)
			kvID := "kv-0001"
			createKeyVault(t, g.DB, kvID, "Test KeyVault")
			nodes, err := azure.ListKeyVaultReaders(context.Background(), g.DB, tc.readerType, kvID, 0, 0)
			require.NoError(t, err)
			require.NotNil(t, nodes)
		})
	}
}

func TestListKeyVaultReaders_NonexistentKeyVault(t *testing.T) {
	g := seedAzureGraph(t)
	_, err := azure.ListKeyVaultReaders(context.Background(), g.DB, azure.RelatedEntityTypeVaultAllReaders, "nonexistent-id", 0, 0)
	require.Error(t, err)
}

// ============================================================================
// ListEntityGroupMembership / ListEntityGroupMemberships / ListEntityGroupMemberPaths / ListEntityGroupMembers
// ============================================================================

func TestListEntityGroupMembershipPaths(t *testing.T) {
	g := seedAzureGraph(t)
	groupID := "group-0002"
	_, err := azure.ListEntityGroupMembershipPaths(context.Background(), g.DB, groupID)
	require.NoError(t, err)
}

func TestListEntityGroupMembership(t *testing.T) {
	g := seedAzureGraph(t)
	groupID := "group-0002"
	nodes, err := azure.ListEntityGroupMembership(context.Background(), g.DB, groupID, 0, 0)
	require.NoError(t, err)
	require.NotNil(t, nodes)
}

func TestListEntityGroupMemberPaths(t *testing.T) {
	g := seedAzureGraph(t)
	groupID := "group-0002"
	_, err := azure.ListEntityGroupMemberPaths(context.Background(), g.DB, groupID)
	require.NoError(t, err)
}

func TestListEntityGroupMembers(t *testing.T) {
	g := seedAzureGraph(t)
	groupID := "group-0002"
	nodes, err := azure.ListEntityGroupMembers(context.Background(), g.DB, groupID, 0, 0)
	require.NoError(t, err)
	require.NotNil(t, nodes)
	require.GreaterOrEqual(t, nodes.Len(), 1)
}

func TestListEntityGroupMembers_WithPagination(t *testing.T) {
	g := seedAzureGraph(t)
	groupID := "group-0002"
	nodes1, err := azure.ListEntityGroupMembers(context.Background(), g.DB, groupID, 0, 1)
	require.NoError(t, err)
	require.NotNil(t, nodes1)
	require.Equal(t, 1, nodes1.Len())
}

// ============================================================================
// ListEntityExecutionPrivilege / ListEntityExecutionPrivilegePaths
// ============================================================================

func TestListEntityExecutionPrivilegePaths(t *testing.T) {
	tests := []struct {
		name      string
		direction graph.Direction
	}{
		{"Outbound", graph.DirectionOutbound},
		{"Inbound", graph.DirectionInbound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := seedAzureGraph(t)
			vmID := "vm-0001"
			createVM(t, g.DB, vmID, "Test VM")
			_, err := azure.ListEntityExecutionPrivilegePaths(context.Background(), g.DB, vmID, tc.direction)
			require.NoError(t, err)
		})
	}
}

func TestListEntityExecutionPrivileges(t *testing.T) {
	tests := []struct {
		name      string
		direction graph.Direction
	}{
		{"Outbound", graph.DirectionOutbound},
		{"Inbound", graph.DirectionInbound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := seedAzureGraph(t)
			vmID := "vm-0001"
			createVM(t, g.DB, vmID, "Test VM")
			nodes, err := azure.ListEntityExecutionPrivileges(context.Background(), g.DB, vmID, tc.direction, 0, 0)
			require.NoError(t, err)
			require.NotNil(t, nodes)
		})
	}
}

func TestListEntityExecutionPrivileges_NonexistentEntity(t *testing.T) {
	g := seedAzureGraph(t)
	_, err := azure.ListEntityExecutionPrivileges(context.Background(), g.DB, "nonexistent-id", graph.DirectionOutbound, 0, 0)
	require.Error(t, err)
}

// ============================================================================
// ListEntityAbusableAppRoleAssignments / ListEntityAbusableAppRoleAssignmentsPaths
// ============================================================================

func TestListEntityAbusableAppRoleAssignmentsPaths(t *testing.T) {
	tests := []struct {
		name      string
		direction graph.Direction
	}{
		{"Outbound", graph.DirectionOutbound},
		{"Inbound", graph.DirectionInbound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := seedAzureGraph(t)
			appID := "app-0001"
			_, err := azure.ListEntityAbusableAppRoleAssignmentsPaths(context.Background(), g.DB, appID, tc.direction)
			require.NoError(t, err)
		})
	}
}

func TestListEntityAbusableAppRoleAssignments(t *testing.T) {
	tests := []struct {
		name      string
		direction graph.Direction
	}{
		{"Outbound", graph.DirectionOutbound},
		{"Inbound", graph.DirectionInbound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := seedAzureGraph(t)
			appID := "app-0001"
			nodes, err := azure.ListEntityAbusableAppRoleAssignments(context.Background(), g.DB, appID, tc.direction, 0, 0)
			require.NoError(t, err)
			require.NotNil(t, nodes)
		})
	}
}

// ============================================================================
// ListEntityObjectControl / ListEntityObjectControlPaths
// ============================================================================

func TestListEntityObjectControlPaths(t *testing.T) {
	tests := []struct {
		name      string
		direction graph.Direction
	}{
		{"Outbound", graph.DirectionOutbound},
		{"Inbound", graph.DirectionInbound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := seedAzureGraph(t)
			userID := "user-0001"
			_, err := azure.ListEntityObjectControlPaths(context.Background(), g.DB, userID, tc.direction)
			require.NoError(t, err)
		})
	}
}

func TestListEntityObjectControl(t *testing.T) {
	tests := []struct {
		name      string
		direction graph.Direction
	}{
		{"Outbound", graph.DirectionOutbound},
		{"Inbound", graph.DirectionInbound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := seedAzureGraph(t)
			userID := "user-0001"
			nodes, err := azure.ListEntityObjectControl(context.Background(), g.DB, userID, tc.direction, 0, 0)
			require.NoError(t, err)
			require.NotNil(t, nodes)
		})
	}
}

// ============================================================================
// ListEntityActiveAssignmentPaths / ListEntityActiveAssignments
// ============================================================================

func TestListEntityActiveAssignmentPaths(t *testing.T) {
	g := seedAzureGraph(t)
	roleID := fmt.Sprintf("%s/%s", TenantObjectID, azschema.CompanyAdministratorRole)
	_, err := azure.ListEntityActiveAssignmentPaths(context.Background(), g.DB, roleID)
	require.NoError(t, err)
}

func TestListEntityActiveAssignments(t *testing.T) {
	g := seedAzureGraph(t)
	roleID := fmt.Sprintf("%s/%s", TenantObjectID, azschema.CompanyAdministratorRole)
	nodes, err := azure.ListEntityActiveAssignments(context.Background(), g.DB, roleID, 0, 0)
	require.NoError(t, err)
	require.NotNil(t, nodes)
}

// ============================================================================
// ListEntityPIMAssignmentPaths / ListEntityPIMAssignments
// ============================================================================

func TestListEntityPIMAssignmentPaths(t *testing.T) {
	g := seedAzureGraph(t)
	roleID := fmt.Sprintf("%s/%s", TenantObjectID, azschema.CompanyAdministratorRole)
	_, err := azure.ListEntityPIMAssignmentPaths(context.Background(), g.DB, roleID)
	require.NoError(t, err)
}

func TestListEntityPIMAssignments(t *testing.T) {
	g := seedAzureGraph(t)
	roleID := fmt.Sprintf("%s/%s", TenantObjectID, azschema.CompanyAdministratorRole)
	nodes, err := azure.ListEntityPIMAssignments(context.Background(), g.DB, roleID, 0, 0)
	require.NoError(t, err)
	require.NotNil(t, nodes)
}

// ============================================================================
// ListRoleApprover / ListRoleApproverPaths
// ============================================================================

func TestListRoleApproverPaths(t *testing.T) {
	g := seedAzureGraph(t)
	roleID := fmt.Sprintf("%s/%s", TenantObjectID, azschema.CompanyAdministratorRole)
	_, err := azure.ListRoleApproverPaths(context.Background(), g.DB, roleID)
	require.NoError(t, err)
}

func TestListRoleApprovers(t *testing.T) {
	g := seedAzureGraph(t)
	roleID := fmt.Sprintf("%s/%s", TenantObjectID, azschema.CompanyAdministratorRole)
	nodes, err := azure.ListRoleApprovers(context.Background(), g.DB, roleID, 0, 0)
	require.NoError(t, err)
	require.NotNil(t, nodes)
}

// ============================================================================
// ListAppFederatedIdentityCredentialPaths / ListAppFederatedIdentityCredentials
// ============================================================================

func TestListAppFederatedIdentityCredentialPaths(t *testing.T) {
	g := seedAzureGraph(t)
	appID := "app-0001"
	_, err := azure.ListAppFederatedIdentityCredentialPaths(context.Background(), g.DB, appID)
	require.NoError(t, err)
}

func TestListAppFederatedIdentityCredentials(t *testing.T) {
	g := seedAzureGraph(t)
	appID := "app-0001"
	nodes, err := azure.ListAppFederatedIdentityCredentials(context.Background(), g.DB, appID, 0, 0)
	require.NoError(t, err)
	require.NotNil(t, nodes)
}

func TestListAppFederatedIdentityCredentials_WithPagination(t *testing.T) {
	g := seedAzureGraph(t)
	appID := "app-0001"
	nodes1, err := azure.ListAppFederatedIdentityCredentials(context.Background(), g.DB, appID, 0, 10)
	require.NoError(t, err)
	require.NotNil(t, nodes1)

	nodes2, err := azure.ListAppFederatedIdentityCredentials(context.Background(), g.DB, appID, 10, 10)
	require.NoError(t, err)
	require.NotNil(t, nodes2)
}

// ============================================================================
// Error cases and edge cases
// ============================================================================

func TestListEntity_EmptyGraph(t *testing.T) {
	db := openTestGraph(t)
	defer db.Close(context.Background())
	_, err := azure.ListEntityDescendents(context.Background(), db, azure.RelatedEntityTypeDescendentUsers, "nonexistent-id", 0, 0)
	require.Error(t, err)
}

func TestListEntity_NilDatabase(t *testing.T) {
	require.Panics(t, func() {
		_, _ = azure.ListEntityDescendents(context.Background(), nil, azure.RelatedEntityTypeDescendentUsers, "id", 0, 0)
	})
}

func TestListEntityRoles_EmptyGraph(t *testing.T) {
	db := openTestGraph(t)
	defer db.Close(context.Background())
	_, err := azure.ListEntityRoles(context.Background(), db, "nonexistent-id", 0, 0)
	require.Error(t, err)
}

// ============================================================================
// Integration tests: verify all entity type mappings work
// ============================================================================

func TestListEntityDescendents_AllTypes(t *testing.T) {
	g := seedAzureGraph(t)

	testCases := []struct {
		name       string
		entityType azure.RelatedEntityType
	}{
		{"DescendentUsers", azure.RelatedEntityTypeDescendentUsers},
		{"DescendentGroups", azure.RelatedEntityTypeDescendentGroups},
		{"DescendentServicePrincipals", azure.RelatedEntityTypeDescendentServicePrincipals},
		{"DescendentDevices", azure.RelatedEntityTypeDescendentDevices},
		{"DescendentSubscriptions", azure.RelatedEntityTypeDescendentSubscriptions},
		{"DescendentResourceGroups", azure.RelatedEntityTypeDescendentResourceGroups},
		{"DescendentManagementGroups", azure.RelatedEntityTypeDescendentManagementGroups},
		{"DescendentVirtualMachines", azure.RelatedEntityTypeDescendentVirtualMachines},
		{"DescendentManagedClusters", azure.RelatedEntityTypeDescendentManagedClusters},
		{"DescendentWebApps", azure.RelatedEntityTypeDescendentWebApps},
		{"DescendentLogicApps", azure.RelatedEntityTypeDescendentLogicApps},
		{"DescendentAutomationAccounts", azure.RelatedEntityTypeDescendentAutomationAccounts},
		{"DescendentKeyVaults", azure.RelatedEntityTypeDescendentKeyVaults},
		{"DescendentApplications", azure.RelatedEntityTypeDescendentApplications},
		{"DescendentVMScaleSets", azure.RelatedEntityTypeDescendentVMScaleSets},
		{"DescendentContainerRegistries", azure.RelatedEntityTypeDescendentContainerRegistries},
		{"DescendentFunctionApps", azure.RelatedEntityTypeDescendentFunctionApps},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nodes, err := azure.ListEntityDescendents(context.Background(), g.DB, tc.entityType, TenantObjectID, 0, 0)
			require.NoError(t, err, "error for entity type %s", tc.entityType)
			require.NotNil(t, nodes)
		})
	}
}

// ============================================================================
// Context deadline tests
// ============================================================================

func TestListEntityDescendents_ContextCancellation(t *testing.T) {
	g := seedAzureGraph(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := azure.ListEntityDescendents(ctx, g.DB, azure.RelatedEntityTypeDescendentUsers, TenantObjectID, 0, 0)
	require.Error(t, err)
}

// ============================================================================
// Multiple entity paths tests
// ============================================================================

func TestListEntityGroupMembershipPaths_MultipleMembers(t *testing.T) {
	g := seedAzureGraph(t)
	groupID := "group-0002"
	createRel(t, g.DB, createUser(t, g.DB, TenantObjectID, "user-0004", "Additional User"), g.Groups[1], azschema.MemberOf)
	createRel(t, g.DB, createUser(t, g.DB, TenantObjectID, "user-0005", "Another User"), g.Groups[1], azschema.MemberOf)
	_, err := azure.ListEntityGroupMembershipPaths(context.Background(), g.DB, groupID)
	require.NoError(t, err)
}
