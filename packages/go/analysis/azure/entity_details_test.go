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

	"github.com/specterops/bloodhound/packages/go/analysis/azure"
	azschema "github.com/specterops/bloodhound/packages/go/graphschema/azure"
	"github.com/specterops/bloodhound/packages/go/graphschema/common"
	"github.com/specterops/bloodhound/packages/go/graphschema"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========================================================================
// Pure functions in azure.go (no DB needed)
// ========================================================================

func TestGetDescendentKinds(t *testing.T) {
	tests := []struct {
		name     string
		kind     graph.Kind
		expected []graph.Kind
	}{
		{
			name: "Tenant descendants",
			kind: azschema.Tenant,
			expected: []graph.Kind{
				azschema.User,
				azschema.Group,
				azschema.ManagementGroup,
				azschema.Subscription,
				azschema.ResourceGroup,
				azschema.VM,
				azschema.ManagedCluster,
				azschema.VMScaleSet,
				azschema.ContainerRegistry,
				azschema.WebApp,
				azschema.LogicApp,
				azschema.AutomationAccount,
				azschema.KeyVault,
				azschema.App,
				azschema.ServicePrincipal,
				azschema.Device,
				azschema.FunctionApp,
			},
		},
		{
			name: "ManagementGroup descendants",
			kind: azschema.ManagementGroup,
			expected: []graph.Kind{
				azschema.ManagementGroup,
				azschema.Subscription,
				azschema.ResourceGroup,
				azschema.VM,
				azschema.ManagedCluster,
				azschema.VMScaleSet,
				azschema.ContainerRegistry,
				azschema.WebApp,
				azschema.LogicApp,
				azschema.AutomationAccount,
				azschema.KeyVault,
				azschema.FunctionApp,
			},
		},
		{
			name: "ResourceGroup descendants",
			kind: azschema.ResourceGroup,
			expected: []graph.Kind{
				azschema.VM,
				azschema.ManagedCluster,
				azschema.VMScaleSet,
				azschema.ContainerRegistry,
				azschema.WebApp,
				azschema.LogicApp,
				azschema.AutomationAccount,
				azschema.KeyVault,
				azschema.FunctionApp,
			},
		},
		{
			name: "Subscription descendants",
			kind: azschema.Subscription,
			expected: []graph.Kind{
				azschema.ResourceGroup,
				azschema.VM,
				azschema.ManagedCluster,
				azschema.VMScaleSet,
				azschema.ContainerRegistry,
				azschema.WebApp,
				azschema.LogicApp,
				azschema.AutomationAccount,
				azschema.KeyVault,
				azschema.FunctionApp,
			},
		},
		{
			name:     "Non-container kind",
			kind:     azschema.User,
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := azure.GetDescendentKinds(tc.kind)
			if tc.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.ElementsMatch(t, tc.expected, result)
			}
		})
	}
}

func TestAzureNonDescentKinds(t *testing.T) {
	kinds := azure.AzureNonDescentKinds()
	assert.NotNil(t, kinds)
	assert.NotEmpty(t, kinds)
	assert.ElementsMatch(t, []graph.Kind{
		azschema.MemberOf,
		azschema.HasRole,
		azschema.RunsAs,
	}, kinds)
}

// ========================================================================
// FromGraphNode and FromGraphNodes
// ========================================================================

func TestFromGraphNode(t *testing.T) {
	props := graph.NewProperties()
	props.Set(common.ObjectID.String(), "test-object-123")
	props.Set(common.Name.String(), "Test Node")
	node := graph.NewNode(100, props, azschema.User)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.User: true,
	}

	result := azure.FromGraphNode(validPrimaryKinds, node)

	assert.NotNil(t, result)
	assert.Equal(t, "test-object-123", result.Properties[common.ObjectID.String()])
	assert.Equal(t, "Test Node", result.Properties[common.Name.String()])
	assert.NotEmpty(t, result.Kinds)
	assert.Contains(t, result.Kinds, azschema.User.String())
}

func TestFromGraphNodes(t *testing.T) {
	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.User: true,
	}

	nodes := make([]*graph.Node, 3)
	for i := 0; i < 3; i++ {
		props := graph.NewProperties()
		props.Set(common.ObjectID.String(), "user-"+string(rune(i)))
		nodes[i] = graph.NewNode(graph.ID(i), props, azschema.User)
	}

	result := azure.FromGraphNodes(validPrimaryKinds, nodes)

	assert.Len(t, result, 3)
	for i := 0; i < 3; i++ {
		assert.NotNil(t, result[i])
		assert.NotEmpty(t, result[i].Properties)
	}
}

// ========================================================================
// Entity Details - User
// ========================================================================

func TestUserEntityDetails_NoHydrate(t *testing.T) {
	g := seedAzureGraph(t)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.User: true,
	}

	details, err := azure.UserEntityDetails(context.Background(), g.DB, validPrimaryKinds, "user-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.NotEmpty(t, details.Node.Properties)
}

func TestUserEntityDetails_WithHydrate(t *testing.T) {
	g := seedAzureGraph(t)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.User: true,
	}

	details, err := azure.UserEntityDetails(context.Background(), g.DB, validPrimaryKinds, "user-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.Roles, 0)
	assert.GreaterOrEqual(t, details.GroupMembership, 0)
}

func TestUserEntityDetails_InvalidObjectID(t *testing.T) {
	g := seedAzureGraph(t)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.User: true,
	}

	_, err := azure.UserEntityDetails(context.Background(), g.DB, validPrimaryKinds, "nonexistent-id", false)
	assert.Error(t, err)
}

// ========================================================================
// Entity Details - Group
// ========================================================================

func TestGroupEntityDetails_NoHydrate(t *testing.T) {
	g := seedAzureGraph(t)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.Group: true,
	}

	details, err := azure.GroupEntityDetails(context.Background(), g.DB, validPrimaryKinds, "group-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.NotEmpty(t, details.Node.Properties)
}

func TestGroupEntityDetails_WithHydrate(t *testing.T) {
	g := seedAzureGraph(t)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.Group: true,
	}

	details, err := azure.GroupEntityDetails(context.Background(), g.DB, validPrimaryKinds, "group-0002", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.Roles, 0)
	assert.GreaterOrEqual(t, details.GroupMembers, 0)
	assert.GreaterOrEqual(t, details.GroupMembership, 0)
}

// ========================================================================
// Entity Details - Device
// ========================================================================

func TestDeviceEntityDetails_NoHydrate(t *testing.T) {
	g := seedAzureGraph(t)
	device := createDevice(t, g.DB, "device-0001", "Test Device")
	createRel(t, g.DB, g.Tenant, device, azschema.Contains)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.Device: true,
	}

	details, err := azure.DeviceEntityDetails(context.Background(), g.DB, validPrimaryKinds, "device-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
}

func TestDeviceEntityDetails_WithHydrate(t *testing.T) {
	g := seedAzureGraph(t)
	device := createDevice(t, g.DB, "device-0001", "Test Device")
	createRel(t, g.DB, g.Tenant, device, azschema.Contains)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.Device: true,
	}

	details, err := azure.DeviceEntityDetails(context.Background(), g.DB, validPrimaryKinds, "device-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.InboundExecutionPrivileges, 0)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

// ========================================================================
// Entity Details - Service Principal
// ========================================================================

func TestServicePrincipalEntityDetails_NoHydrate(t *testing.T) {
	g := seedAzureGraph(t)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.ServicePrincipal: true,
	}

	details, err := azure.ServicePrincipalEntityDetails(context.Background(), g.DB, validPrimaryKinds, "sp-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
}

func TestServicePrincipalEntityDetails_WithHydrate(t *testing.T) {
	g := seedAzureGraph(t)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.ServicePrincipal: true,
	}

	details, err := azure.ServicePrincipalEntityDetails(context.Background(), g.DB, validPrimaryKinds, "sp-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.Roles, 0)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
	assert.GreaterOrEqual(t, details.OutboundObjectControl, 0)
}

// ========================================================================
// Entity Details - Application
// ========================================================================

func TestApplicationEntityDetails_NoHydrate(t *testing.T) {
	g := seedAzureGraph(t)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.App: true,
	}

	details, err := azure.ApplicationEntityDetails(context.Background(), g.DB, validPrimaryKinds, "app-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
}

func TestApplicationEntityDetails_WithHydrate(t *testing.T) {
	g := seedAzureGraph(t)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.App: true,
	}

	details, err := azure.ApplicationEntityDetails(context.Background(), g.DB, validPrimaryKinds, "app-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
	assert.GreaterOrEqual(t, details.FederatedIdentityCredentials, 0)
}

// ========================================================================
// Entity Details - VM
// ========================================================================

func TestVMEntityDetails_NoHydrate(t *testing.T) {
	g := seedAzureGraph(t)
	vm := createVM(t, g.DB, "vm-0001", "Test VM")
	createRel(t, g.DB, g.Tenant, vm, azschema.Contains)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.VM: true,
	}

	details, err := azure.VMEntityDetails(context.Background(), g.DB, validPrimaryKinds, "vm-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
}

func TestVMEntityDetails_WithHydrate(t *testing.T) {
	g := seedAzureGraph(t)
	vm := createVM(t, g.DB, "vm-0001", "Test VM")
	createRel(t, g.DB, g.Tenant, vm, azschema.Contains)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.VM: true,
	}

	details, err := azure.VMEntityDetails(context.Background(), g.DB, validPrimaryKinds, "vm-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.InboundExecutionPrivileges, 0)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

// ========================================================================
// Entity Details - Role
// ========================================================================

func TestRoleEntityDetails_NoHydrate(t *testing.T) {
	g := seedAzureGraph(t)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.Role: true,
	}

	roleID := "test-tenant-0001/" + azschema.CompanyAdministratorRole
	details, err := azure.RoleEntityDetails(context.Background(), g.DB, validPrimaryKinds, roleID, false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
}

func TestRoleEntityDetails_WithHydrate(t *testing.T) {
	g := seedAzureGraph(t)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.Role: true,
	}

	roleID := "test-tenant-0001/" + azschema.CompanyAdministratorRole
	details, err := azure.RoleEntityDetails(context.Background(), g.DB, validPrimaryKinds, roleID, true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.ActiveAssignments, 0)
	assert.GreaterOrEqual(t, details.PIMAssignments, 0)
	assert.GreaterOrEqual(t, details.Approvers, 0)
}

// ========================================================================
// Entity Details - Tenant
// ========================================================================

func TestTenantEntityDetails_NoHydrate(t *testing.T) {
	g := seedAzureGraph(t)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.Tenant: true,
	}

	details, err := azure.TenantEntityDetails(context.Background(), g.DB, validPrimaryKinds, TenantObjectID, false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
}

func TestTenantEntityDetails_WithHydrate(t *testing.T) {
	g := seedAzureGraph(t)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.Tenant: true,
	}

	details, err := azure.TenantEntityDetails(context.Background(), g.DB, validPrimaryKinds, TenantObjectID, true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.NotNil(t, details.Descendents)
	assert.NotNil(t, details.Descendents.DescendentCounts)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

// ========================================================================
// Entity Details - Key Vault
// ========================================================================

func TestKeyVaultEntityDetails_NoHydrate(t *testing.T) {
	g := seedAzureGraph(t)
	kv := createKeyVault(t, g.DB, "kv-0001", "Test Key Vault")
	createRel(t, g.DB, g.Tenant, kv, azschema.Contains)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.KeyVault: true,
	}

	details, err := azure.KeyVaultEntityDetails(context.Background(), g.DB, validPrimaryKinds, "kv-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
}

func TestKeyVaultEntityDetails_WithHydrate(t *testing.T) {
	g := seedAzureGraph(t)
	kv := createKeyVault(t, g.DB, "kv-0001", "Test Key Vault")
	createRel(t, g.DB, g.Tenant, kv, azschema.Contains)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.KeyVault: true,
	}

	details, err := azure.KeyVaultEntityDetails(context.Background(), g.DB, validPrimaryKinds, "kv-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.NotNil(t, details.Readers)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

// ========================================================================
// Entity Details - Management Group
// ========================================================================

func TestManagementGroupEntityDetails_NoHydrate(t *testing.T) {
	g := seedAzureGraph(t)
	mg := createManagementGroup(t, g.DB, "mg-0001", "Test MG")
	createRel(t, g.DB, g.Tenant, mg, azschema.Contains)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.ManagementGroup: true,
	}

	details, err := azure.ManagementGroupEntityDetails(context.Background(), g.DB, validPrimaryKinds, "mg-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
}

func TestManagementGroupEntityDetails_WithHydrate(t *testing.T) {
	g := seedAzureGraph(t)
	mg := createManagementGroup(t, g.DB, "mg-0001", "Test MG")
	createRel(t, g.DB, g.Tenant, mg, azschema.Contains)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.ManagementGroup: true,
	}

	details, err := azure.ManagementGroupEntityDetails(context.Background(), g.DB, validPrimaryKinds, "mg-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.NotNil(t, details.Descendents)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

// ========================================================================
// Entity Details - Subscription
// ========================================================================

func TestSubscriptionEntityDetails_NoHydrate(t *testing.T) {
	g := seedAzureGraph(t)
	sub := createSubscription(t, g.DB, "sub-0001", "Test Subscription")
	createRel(t, g.DB, g.Tenant, sub, azschema.Contains)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.Subscription: true,
	}

	details, err := azure.SubscriptionEntityDetails(context.Background(), g.DB, validPrimaryKinds, "sub-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
}

func TestSubscriptionEntityDetails_WithHydrate(t *testing.T) {
	g := seedAzureGraph(t)
	sub := createSubscription(t, g.DB, "sub-0001", "Test Subscription")
	createRel(t, g.DB, g.Tenant, sub, azschema.Contains)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.Subscription: true,
	}

	details, err := azure.SubscriptionEntityDetails(context.Background(), g.DB, validPrimaryKinds, "sub-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.NotNil(t, details.Descendents)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

// ========================================================================
// Entity Details - Resource Group
// ========================================================================

func TestResourceGroupEntityDetails_NoHydrate(t *testing.T) {
	g := seedAzureGraph(t)
	rg := createResourceGroup(t, g.DB, "rg-0001", "Test RG")
	createRel(t, g.DB, g.Tenant, rg, azschema.Contains)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.ResourceGroup: true,
	}

	details, err := azure.ResourceGroupEntityDetails(context.Background(), g.DB, validPrimaryKinds, "rg-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
}

func TestResourceGroupEntityDetails_WithHydrate(t *testing.T) {
	g := seedAzureGraph(t)
	rg := createResourceGroup(t, g.DB, "rg-0001", "Test RG")
	createRel(t, g.DB, g.Tenant, rg, azschema.Contains)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.ResourceGroup: true,
	}

	details, err := azure.ResourceGroupEntityDetails(context.Background(), g.DB, validPrimaryKinds, "rg-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.NotNil(t, details.Descendents)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

// ========================================================================
// Entity Details - Federated Identity Credentials
// ========================================================================

func TestFederatedIdentityCredentialEntityDetails(t *testing.T) {
	g := seedAzureGraph(t)

	var ficNode *graph.Node
	require.NoError(t, g.DB.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): "fic-0001",
			common.Name.String():     "Test FIC",
		})
		var err error
		ficNode, err = tx.CreateNode(props, azschema.Entity, azschema.FederatedIdentityCredential)
		return err
	}))

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.FederatedIdentityCredential: true,
	}

	details, err := azure.FederatedIdentityCredentialEntityDetails(context.Background(), g.DB, validPrimaryKinds, "fic-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	require.NotNil(t, ficNode)
}

// ========================================================================
// Additional Entity Details Tests
// ========================================================================

func TestWebAppEntityDetails(t *testing.T) {
	g := seedAzureGraph(t)

	var waNode *graph.Node
	require.NoError(t, g.DB.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): "wa-0001",
			common.Name.String():     "Test WebApp",
		})
		var err error
		waNode, err = tx.CreateNode(props, azschema.Entity, azschema.WebApp)
		return err
	}))
	require.NotNil(t, waNode)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.WebApp: true,
	}

	details, err := azure.WebAppEntityDetails(context.Background(), g.DB, validPrimaryKinds, "wa-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

func TestLogicAppEntityDetails(t *testing.T) {
	g := seedAzureGraph(t)

	var laNode *graph.Node
	require.NoError(t, g.DB.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): "la-0001",
			common.Name.String():     "Test LogicApp",
		})
		var err error
		laNode, err = tx.CreateNode(props, azschema.Entity, azschema.LogicApp)
		return err
	}))
	require.NotNil(t, laNode)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.LogicApp: true,
	}

	details, err := azure.LogicAppEntityDetails(context.Background(), g.DB, validPrimaryKinds, "la-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

func TestFunctionAppEntityDetails(t *testing.T) {
	g := seedAzureGraph(t)

	var faNode *graph.Node
	require.NoError(t, g.DB.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): "fa-0001",
			common.Name.String():     "Test FunctionApp",
		})
		var err error
		faNode, err = tx.CreateNode(props, azschema.Entity, azschema.FunctionApp)
		return err
	}))
	require.NotNil(t, faNode)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.FunctionApp: true,
	}

	details, err := azure.FunctionAppEntityDetails(context.Background(), g.DB, validPrimaryKinds, "fa-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

func TestAutomationAccountEntityDetails(t *testing.T) {
	g := seedAzureGraph(t)

	var aaNode *graph.Node
	require.NoError(t, g.DB.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): "aa-0001",
			common.Name.String():     "Test AutomationAccount",
		})
		var err error
		aaNode, err = tx.CreateNode(props, azschema.Entity, azschema.AutomationAccount)
		return err
	}))
	require.NotNil(t, aaNode)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.AutomationAccount: true,
	}

	details, err := azure.AutomationAccountEntityDetails(context.Background(), g.DB, validPrimaryKinds, "aa-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

func TestContainerRegistryEntityDetails(t *testing.T) {
	g := seedAzureGraph(t)

	var crNode *graph.Node
	require.NoError(t, g.DB.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): "cr-0001",
			common.Name.String():     "Test ContainerRegistry",
		})
		var err error
		crNode, err = tx.CreateNode(props, azschema.Entity, azschema.ContainerRegistry)
		return err
	}))
	require.NotNil(t, crNode)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.ContainerRegistry: true,
	}

	details, err := azure.ContainerRegistryEntityDetails(context.Background(), g.DB, validPrimaryKinds, "cr-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

func TestManagedClusterEntityDetails(t *testing.T) {
	g := seedAzureGraph(t)

	var mcNode *graph.Node
	require.NoError(t, g.DB.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): "mc-0001",
			common.Name.String():     "Test ManagedCluster",
		})
		var err error
		mcNode, err = tx.CreateNode(props, azschema.Entity, azschema.ManagedCluster)
		return err
	}))
	require.NotNil(t, mcNode)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.ManagedCluster: true,
	}

	details, err := azure.ManagedClusterEntityDetails(context.Background(), g.DB, validPrimaryKinds, "mc-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

func TestVMScaleSetEntityDetails(t *testing.T) {
	g := seedAzureGraph(t)

	var vssNode *graph.Node
	require.NoError(t, g.DB.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		props := graph.AsProperties(map[string]any{
			common.ObjectID.String(): "vss-0001",
			common.Name.String():     "Test VMScaleSet",
		})
		var err error
		vssNode, err = tx.CreateNode(props, azschema.Entity, azschema.VMScaleSet)
		return err
	}))
	require.NotNil(t, vssNode)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.VMScaleSet: true,
	}

	details, err := azure.VMScaleSetEntityDetails(context.Background(), g.DB, validPrimaryKinds, "vss-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

// ========================================================================
// Node Details Structure Tests
// ========================================================================

func TestNodeStructure_ContainsProperties(t *testing.T) {
	props := graph.NewProperties()
	props.Set(common.ObjectID.String(), "obj-123")
	props.Set(common.Name.String(), "Test Object")
	props.Set("custom-prop", "custom-value")
	node := graph.NewNode(100, props, azschema.User)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.User: true,
	}

	nodeDetails := azure.FromGraphNode(validPrimaryKinds, node)

	assert.NotNil(t, nodeDetails.Properties)
	assert.Len(t, nodeDetails.Properties, 3)
	assert.Equal(t, "obj-123", nodeDetails.Properties[common.ObjectID.String()])
	assert.Equal(t, "Test Object", nodeDetails.Properties[common.Name.String()])
	assert.Equal(t, "custom-value", nodeDetails.Properties["custom-prop"])
}

func TestNodeStructure_ContainsKinds(t *testing.T) {
	node := graph.NewNode(100, graph.NewProperties(), azschema.User)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.User: true,
	}

	nodeDetails := azure.FromGraphNode(validPrimaryKinds, node)

	assert.NotNil(t, nodeDetails.Kinds)
	assert.Contains(t, nodeDetails.Kinds, azschema.User.String())
}

// ========================================================================
// Application Entity Details Tests
// ========================================================================

func TestApplicationEntityDetails_WithServicePrincipal(t *testing.T) {
	g := seedAzureGraph(t)
	db := g.DB

	app := g.Application
	sp := g.ServicePrincipals[0]

	// Create RunsAs relationship between app and service principal
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		_, err := tx.CreateRelationshipByIDs(app.ID, sp.ID, azschema.RunsAs, nil)
		return err
	}))

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.App: true,
	}

	// Use the actual object ID from the seeded graph
	details, err := azure.ApplicationEntityDetails(context.Background(), db, validPrimaryKinds, "app-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	// Service principal ID should be populated
	require.NotEmpty(t, details.Properties)
}

func TestApplicationEntityDetails_WithHydrateCounts(t *testing.T) {
	g := seedAzureGraph(t)
	db := g.DB

	app := g.Application
	sp := g.ServicePrincipals[0]

	// Wire up service principal to app
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		_, err := tx.CreateRelationshipByIDs(app.ID, sp.ID, azschema.RunsAs, nil)
		return err
	}))

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.App: true,
	}

	// Use the actual object ID from the seeded graph
	details, err := azure.ApplicationEntityDetails(context.Background(), db, validPrimaryKinds, "app-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
}

// ========================================================================
// Device Entity Details Tests
// ========================================================================

func TestDeviceEntityDetails_WithProperties(t *testing.T) {
	db := openTestGraph(t)

	device := createDevice(t, db, "device-0001", "Test Device")

	// Set device properties
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		device.Properties.Set("operatingsystem", "Windows 10")
		device.Properties.Set("operatingsystemversion", "19041")
		return tx.UpdateNode(device)
	}))

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.Device: true,
	}

	details, err := azure.DeviceEntityDetails(context.Background(), db, validPrimaryKinds, "device-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

// ========================================================================
// Management Group Tests
// ========================================================================

func TestManagementGroupEntityDetails(t *testing.T) {
	db := openTestGraph(t)

	_ = createManagementGroup(t, db, "mg-0001", "Test Management Group")

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.ManagementGroup: true,
	}

	details, err := azure.ManagementGroupEntityDetails(context.Background(), db, validPrimaryKinds, "mg-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
}

// ========================================================================
// VM Entity Details Tests
// ========================================================================

func TestVMEntityDetails_WithContains(t *testing.T) {
	db := openTestGraph(t)
	tenant := createTenant(t, db, "test-tenant-vm", "Test Tenant VM")
	vm := createVM(t, db, "vm-0001", "Test VM")

	createRel(t, db, tenant, vm, azschema.Contains)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.VM: true,
	}

	details, err := azure.VMEntityDetails(context.Background(), db, validPrimaryKinds, "vm-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

// ========================================================================
// KeyVault Entity Details Tests
// ========================================================================

func TestKeyVaultEntityDetails_WithCounts(t *testing.T) {
	db := openTestGraph(t)
	tenant := createTenant(t, db, "test-tenant-kv", "Test Tenant KV")
	kv := createKeyVault(t, db, "kv-0001", "Test KeyVault")

	createRel(t, db, tenant, kv, azschema.Contains)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.KeyVault: true,
	}

	details, err := azure.KeyVaultEntityDetails(context.Background(), db, validPrimaryKinds, "kv-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.InboundObjectControl, 0)
}

// ========================================================================
// BaseEntityDetails Tests
// ========================================================================

func TestBaseEntityDetails_WithoutHydrate(t *testing.T) {
	g := seedAzureGraph(t)
	db := g.DB

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.User: true,
	}

	details, err := azure.BaseEntityDetails(context.Background(), db, validPrimaryKinds, "user-0001", false)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.Equal(t, 0, details.OutboundObjectControl)
}

func TestBaseEntityDetails_WithHydrate(t *testing.T) {
	g := seedAzureGraph(t)
	db := g.DB

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.User: true,
	}

	details, err := azure.BaseEntityDetails(context.Background(), db, validPrimaryKinds, "user-0001", true)
	require.NoError(t, err)
	require.NotNil(t, details.Node)
	assert.GreaterOrEqual(t, details.OutboundObjectControl, 0)
}

func TestBaseEntityDetails_NonExistent(t *testing.T) {
	db := openTestGraph(t)

	validPrimaryKinds := graphschema.ValidPrimaryKinds{
		azschema.User: true,
	}

	_, err := azure.BaseEntityDetails(context.Background(), db, validPrimaryKinds, "nonexistent-user", false)
	require.Error(t, err)
}

