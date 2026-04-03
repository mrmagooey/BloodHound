//go:build standalone

// Copyright 2023 Specter Ops, Inc.
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

	"github.com/bloodhoundad/azurehound/v2/constants"
	graph_mocks "github.com/specterops/bloodhound/cmd/api/src/vendormocks/dawgs/graph"
	"github.com/specterops/bloodhound/packages/go/analysis/azure"
	azschema "github.com/specterops/bloodhound/packages/go/graphschema/azure"
	"github.com/specterops/dawgs/cardinality"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
	"github.com/specterops/dawgs/util/size"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var (
	user  = graph.NewNode(0, graph.NewProperties(), azschema.User)
	user2 = graph.NewNode(1, graph.NewProperties(), azschema.User)
	group = graph.NewNode(2, graph.NewProperties(), azschema.Group)
	app   = graph.NewNode(3, graph.NewProperties(), azschema.App)
)

// setupRoleAssignments is used to create a testable RoleAssignments struct. It is used in all RoleAssignments tests
// and may require adjusting tests if modified
func setupRoleAssignments() azure.RoleAssignments {
	roleMap := map[string]cardinality.Duplex[uint64]{
		constants.GlobalAdministratorRoleID:   cardinality.NewBitmap64(),
		constants.ReportsReaderRoleID:         cardinality.NewBitmap64(),
		constants.HelpdeskAdministratorRoleID: cardinality.NewBitmap64(),
		constants.PartnerTier1SupportRoleID:   cardinality.NewBitmap64(),
	}
	roleMap[constants.GlobalAdministratorRoleID].Add(uint64(user.ID))
	roleMap[constants.ReportsReaderRoleID].Add(uint64(group.ID))
	roleMap[constants.HelpdeskAdministratorRoleID].Add(uint64(group.ID))
	roleMap[constants.PartnerTier1SupportRoleID].Add(uint64(app.ID))

	return azure.RoleAssignments{
		// user2 has no roles! this is intentional
		Principals: graph.NewNodeSet(user, user2, group, app).KindSet(),
		RoleMap:    roleMap,
	}
}

func TestRoleAssignments_NodeHasRole(t *testing.T) {
	assignments := setupRoleAssignments()
	assert.True(t, assignments.NodeHasRole(user.ID, azschema.CompanyAdministratorRole))
	assert.False(t, assignments.NodeHasRole(user.ID, azschema.HelpdeskAdministratorRole))
	assert.True(t, assignments.NodeHasRole(group.ID, azschema.ReportsReaderRole))
	assert.True(t, assignments.NodeHasRole(group.ID, azschema.HelpdeskAdministratorRole))
	assert.False(t, assignments.NodeHasRole(group.ID, azschema.PartnerTier1SupportRole))
}

func TestRoleAssignments_UsersWithoutRoles(t *testing.T) {
	assignments := setupRoleAssignments()
	assert.False(t, assignments.UsersWithoutRoles().Contains(uint64(user.ID)))
	assert.True(t, assignments.UsersWithoutRoles().Contains(uint64(user2.ID)))
}

func TestRoleAssignments_NodesWithRole(t *testing.T) {
	assignments := setupRoleAssignments()
	assert.True(t, assignments.PrincipalsWithRole(constants.ReportsReaderRoleID, constants.GlobalAdministratorRoleID).Contains(uint64(user.ID)))
	assert.True(t, assignments.PrincipalsWithRole(constants.ReportsReaderRoleID, constants.GlobalAdministratorRoleID).Contains(uint64(group.ID)))
	assert.True(t, assignments.PrincipalsWithRole(constants.ReportsReaderRoleID, constants.HelpdeskAdministratorRoleID).Contains(uint64(group.ID)))
	assert.False(t, assignments.PrincipalsWithRole(constants.ReportsReaderRoleID).Contains(uint64(user.ID)))
}

func TestRoleAssignments_NodesWithRolesExclusive(t *testing.T) {
	assignments := setupRoleAssignments()
	assert.Equal(t, user, assignments.NodesWithRolesExclusive(azschema.ReportsReaderRole, azschema.CompanyAdministratorRole).Get(azschema.User).Get(user.ID))
	assert.Equal(t, graph.EmptyNodeSet().Get(0), assignments.NodesWithRolesExclusive(azschema.ReportsReaderRole, azschema.CompanyAdministratorRole).Get(azschema.Group).Get(group.ID))
	assert.Equal(t, group, assignments.NodesWithRolesExclusive(azschema.ReportsReaderRole, azschema.HelpdeskAdministratorRole).Get(azschema.Group).Get(group.ID))
	assert.Equal(t, graph.EmptyNodeSet().Get(0), assignments.NodesWithRolesExclusive(azschema.ReportsReaderRole).Get(azschema.User).Get(user.ID))
}

func TestTenantRoles(t *testing.T) {
	var (
		ctrl       = gomock.NewController(t)
		mockTx     = graph_mocks.NewMockTransaction(ctrl)
		stubTenant = &graph.Node{
			ID:         1,
			Kinds:      graph.Kinds{azschema.Entity, azschema.Tenant},
			Properties: &graph.Properties{},
		}
		stubIntuneAdminRole = &graph.Node{
			ID:    2,
			Kinds: graph.Kinds{azschema.Entity, azschema.Role},
			Properties: &graph.Properties{
				Map: map[string]any{
					"templateid": constants.IntuneAdministratorRoleID,
				},
			},
		}
	)
	defer ctrl.Finish()

	mockRelQuery1 := graph_mocks.NewMockRelationshipQuery(ctrl)
	mockTx.EXPECT().Relationships().Return(mockRelQuery1).Times(1)

	mockRelQuery2 := graph_mocks.NewMockRelationshipQuery(ctrl)
	mockRelQuery1.EXPECT().Filterf(gomock.AssignableToTypeOf(func() graph.Criteria { return nil })).Return(mockRelQuery2)
	mockRelQuery2.EXPECT().
		FetchDirection(gomock.Any(), gomock.AssignableToTypeOf(func(graph.Cursor[graph.DirectionalResult]) error { return nil })).
		DoAndReturn(func(_ any, delegate func(graph.Cursor[graph.DirectionalResult]) error) error {
			mockCursor := graph_mocks.NewMockCursor[graph.DirectionalResult](ctrl)
			c := make(chan graph.DirectionalResult, 1)
			go func() {
				defer close(c)
				c <- graph.DirectionalResult{Node: stubIntuneAdminRole}
			}()
			mockCursor.EXPECT().Chan().Return(c)
			mockCursor.EXPECT().Error().Return(nil)
			return delegate(mockCursor)
		})

	roles, err := azure.TenantRoles(mockTx, stubTenant, constants.IntuneAdministratorRoleID)
	require.Nil(t, err)
	assert.Equal(t, 1, roles.Len())
	assert.Contains(t, roles.Slice(), stubIntuneAdminRole)
}

func TestRoleMembers(t *testing.T) {
	var (
		ctrl       = gomock.NewController(t)
		mockTx     = graph_mocks.NewMockTransaction(ctrl)
		stubTenant = &graph.Node{
			ID:         1,
			Kinds:      graph.Kinds{azschema.Entity, azschema.Tenant},
			Properties: &graph.Properties{},
		}
		stubIntuneAdminRole = &graph.Node{
			ID:    2,
			Kinds: graph.Kinds{azschema.Entity, azschema.Role},
			Properties: &graph.Properties{
				Map: map[string]any{
					"templateid": constants.IntuneAdministratorRoleID,
				},
			},
		}
		stubIntuneAdmin1 = &graph.Node{
			ID:    3,
			Kinds: graph.Kinds{azschema.Entity, azschema.User},
		}
		stubIntuneAdmin2 = &graph.Node{
			ID:    4,
			Kinds: graph.Kinds{azschema.Entity, azschema.User},
		}
		stubIntuneAdmin3 = &graph.Node{
			ID:    5,
			Kinds: graph.Kinds{azschema.Entity, azschema.User},
		}
	)
	defer ctrl.Finish()

	mockTx.EXPECT().GraphQueryMemoryLimit().Return(1 * size.Gibibyte).AnyTimes()

	mockRelQuery1 := graph_mocks.NewMockRelationshipQuery(ctrl)
	mockRelQuery2 := graph_mocks.NewMockRelationshipQuery(ctrl)
	gomock.InOrder(
		mockTx.EXPECT().Relationships().Return(mockRelQuery1),
		mockTx.EXPECT().Relationships().Return(mockRelQuery2).AnyTimes(),
	)

	mockFilterf1 := graph_mocks.NewMockRelationshipQuery(ctrl)
	mockRelQuery1.EXPECT().Filterf(gomock.AssignableToTypeOf(func() graph.Criteria { return nil })).Return(mockFilterf1)
	mockFilterf1.EXPECT().
		FetchDirection(gomock.Any(), gomock.AssignableToTypeOf(func(graph.Cursor[graph.DirectionalResult]) error { return nil })).
		DoAndReturn(func(_ any, delegate func(graph.Cursor[graph.DirectionalResult]) error) error {
			mockCursor := graph_mocks.NewMockCursor[graph.DirectionalResult](ctrl)
			c := make(chan graph.DirectionalResult, 1)
			go func() {
				defer close(c)
				c <- graph.DirectionalResult{Node: stubIntuneAdminRole}
			}()
			mockCursor.EXPECT().Chan().Return(c)
			mockCursor.EXPECT().Error().Return(nil)
			return delegate(mockCursor)
		})

	mockFilterf2 := graph_mocks.NewMockRelationshipQuery(ctrl)
	mockRelQuery2.EXPECT().Filterf(gomock.AssignableToTypeOf(func() graph.Criteria { return nil })).Return(mockFilterf2).AnyTimes()
	mockFilterf2.EXPECT().
		FetchDirection(gomock.Any(), gomock.AssignableToTypeOf(func(graph.Cursor[graph.DirectionalResult]) error { return nil })).
		DoAndReturn(func(_ any, delegate func(graph.Cursor[graph.DirectionalResult]) error) error {
			mockCursor := graph_mocks.NewMockCursor[graph.DirectionalResult](ctrl)
			c := make(chan graph.DirectionalResult, 3)
			go func() {
				defer close(c)
				c <- graph.DirectionalResult{
					Node: stubIntuneAdmin1,
					Relationship: &graph.Relationship{
						ID:      101,
						Kind:    azschema.HasRole,
						StartID: stubIntuneAdmin1.ID,
						EndID:   stubIntuneAdminRole.ID,
					},
				}
				c <- graph.DirectionalResult{
					Node: stubIntuneAdmin2,
					Relationship: &graph.Relationship{
						ID:      102,
						Kind:    azschema.HasRole,
						StartID: stubIntuneAdmin2.ID,
						EndID:   stubIntuneAdminRole.ID,
					},
				}
				c <- graph.DirectionalResult{
					Node: stubIntuneAdmin3,
					Relationship: &graph.Relationship{
						ID:      103,
						Kind:    azschema.HasRole,
						StartID: stubIntuneAdmin3.ID,
						EndID:   stubIntuneAdminRole.ID,
					},
				}
			}()
			mockCursor.EXPECT().Chan().Return(c)
			mockCursor.EXPECT().Error().Return(nil)
			return delegate(mockCursor)
		}).AnyTimes()

	members, err := azure.RoleMembers(mockTx, stubTenant, constants.IntuneAdministratorRoleID)
	require.Nil(t, err)
	assert.Equal(t, 3, members.Len())
	assert.NotContains(t, members.Slice(), stubIntuneAdminRole)
	assert.Contains(t, members.Slice(), stubIntuneAdmin1)
	assert.Contains(t, members.Slice(), stubIntuneAdmin2)
	assert.Contains(t, members.Slice(), stubIntuneAdmin3)
}

func TestFetchTenants(t *testing.T) {
	var (
		ctrl       = gomock.NewController(t)
		mockDB     = graph_mocks.NewMockDatabase(ctrl)
		stubTenant = &graph.Node{
			ID:         1,
			Kinds:      graph.Kinds{azschema.Entity, azschema.Tenant},
			Properties: &graph.Properties{},
		}
	)

	mockDB.EXPECT().ReadTransaction(gomock.Any(), gomock.AssignableToTypeOf(func(graph.Transaction) error { return nil }), gomock.Any()).DoAndReturn(func(_ any, delegate graph.TransactionDelegate, _ ...any) error {
		var (
			mockTx        = graph_mocks.NewMockTransaction(ctrl)
			mockNodeQuery = graph_mocks.NewMockNodeQuery(ctrl)
			mockFilterf   = graph_mocks.NewMockNodeQuery(ctrl)
		)
		mockTx.EXPECT().Nodes().Return(mockNodeQuery)
		mockNodeQuery.EXPECT().Filterf(gomock.Any()).Return(mockFilterf)
		mockFilterf.EXPECT().Fetch(gomock.AssignableToTypeOf(func(graph.Cursor[*graph.Node]) error { return nil }), gomock.Any()).DoAndReturn(func(delegate func(graph.Cursor[*graph.Node]) error, _ ...any) error {
			mockCursor := graph_mocks.NewMockCursor[*graph.Node](ctrl)
			c := make(chan *graph.Node, 1)
			go func() {
				defer close(c)
				c <- stubTenant
			}()

			mockCursor.EXPECT().Chan().Return(c)
			mockCursor.EXPECT().Error().Return(nil)
			return delegate(mockCursor)
		})

		return delegate(mockTx)
	})

	tenants, err := azure.FetchTenants(context.Background(), mockDB)
	require.Nil(t, err)
	assert.Equal(t, 1, tenants.Len())
	assert.Contains(t, tenants.Slice(), stubTenant)
}

func TestEndNodes(t *testing.T) {
	var (
		ctrl         = gomock.NewController(t)
		mockTx       = graph_mocks.NewMockTransaction(ctrl)
		mockRelQuery = graph_mocks.NewMockRelationshipQuery(ctrl)
		mockFilterf  = graph_mocks.NewMockRelationshipQuery(ctrl)
		stubTenant   = &graph.Node{
			ID:         1,
			Kinds:      graph.Kinds{azschema.Entity, azschema.Tenant},
			Properties: &graph.Properties{},
		}
		stubDevice1 = &graph.Node{
			ID:    1,
			Kinds: graph.Kinds{azschema.Entity, azschema.Device},
		}
		stubDevice2 = &graph.Node{
			ID:    2,
			Kinds: graph.Kinds{azschema.Entity, azschema.Device},
		}
		stubDevice3 = &graph.Node{
			ID:    3,
			Kinds: graph.Kinds{azschema.Entity, azschema.Device},
		}
	)
	mockTx.EXPECT().Relationships().Return(mockRelQuery)
	mockRelQuery.EXPECT().Filterf(gomock.Any()).Return(mockFilterf)
	mockFilterf.EXPECT().
		FetchDirection(gomock.Any(), gomock.AssignableToTypeOf(func(graph.Cursor[graph.DirectionalResult]) error { return nil })).
		DoAndReturn(func(_ any, delegate func(graph.Cursor[graph.DirectionalResult]) error) error {
			mockCursor := graph_mocks.NewMockCursor[graph.DirectionalResult](ctrl)
			c := make(chan graph.DirectionalResult, 3)
			go func() {
				defer close(c)
				c <- graph.DirectionalResult{Node: stubDevice1}
				c <- graph.DirectionalResult{Node: stubDevice2}
				c <- graph.DirectionalResult{Node: stubDevice3}
			}()
			mockCursor.EXPECT().Chan().Return(c)
			mockCursor.EXPECT().Error().Return(nil)
			return delegate(mockCursor)
		})

	nodes, err := azure.EndNodes(mockTx, stubTenant, azschema.Contains, azschema.Device)
	require.Nil(t, err)
	assert.Equal(t, 3, nodes.Len())
	assert.NotContains(t, nodes.Slice(), stubTenant)
	assert.Contains(t, nodes.Slice(), stubDevice1)
	assert.Contains(t, nodes.Slice(), stubDevice2)
	assert.Contains(t, nodes.Slice(), stubDevice3)
}

// ============================= Integration Tests using existing helpers =============================

// TestIsWindowsDeviceIntegration tests IsWindowsDevice with real graph nodes
func TestIsWindowsDeviceIntegration(t *testing.T) {
	tests := []struct {
		name     string
		os       string
		expected bool
		setProp  bool
	}{
		{"Windows 10", "Windows 10 Enterprise", true, true},
		{"windows lowercase", "windows server 2019", true, true},
		{"WINDOWS uppercase", "WINDOWS", true, true},
		{"Linux", "Ubuntu 20.04", false, true},
		{"macOS", "macOS Ventura", false, true},
		{"empty string", "", false, true},
		{"no OS property", "", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			props := graph.NewProperties()
			if tc.setProp {
				props.Set("operatingsystem", tc.os)
			}
			node := graph.NewNode(100, props, azschema.Device)
			result, err := azure.IsWindowsDevice(node)
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
		})
	}
}

// TestExecuteCommandIntegration verifies ExecuteCommand creates edges for Intune admins to Windows devices
func TestExecuteCommandIntegration(t *testing.T) {
	g := seedAzureGraph(t)
	db := g.DB

	windowsDevice := createDevice(t, db, "device-001", "Windows Device")
	linuxDevice := createDevice(t, db, "device-002", "Linux Device")

	// Set OS properties
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		windowsDevice.Properties.Set("operatingsystem", "Windows 10 Enterprise")
		return tx.UpdateNode(windowsDevice)
	}))

	// Create relationships
	createRel(t, db, g.Tenant, windowsDevice, azschema.Contains)
	createRel(t, db, g.Tenant, linuxDevice, azschema.Contains)

	// Create Intune admin role and assign to user
	intuneRole := createRole(t, db, fmt.Sprintf("%s/%s", TenantObjectID, azschema.IntuneServiceAdministratorRole), "Intune Service Administrator", azschema.IntuneServiceAdministratorRole)
	intuneAdmin := createUser(t, db, TenantObjectID, "user-intune-001", "Intune Admin")
	createRel(t, db, g.Tenant, intuneRole, azschema.Contains)
	createRel(t, db, g.Tenant, intuneAdmin, azschema.Contains)
	createRel(t, db, intuneAdmin, intuneRole, azschema.HasRole)

	// Run ExecuteCommand
	stats, err := azure.ExecuteCommand(context.Background(), db)
	require.NoError(t, err)
	require.NotNil(t, stats)

	// Verify edge exists to Windows device but not Linux device
	requireRelExists(t, db, intuneAdmin.ID, windowsDevice.ID, azschema.ExecuteCommand)
	requireRelNotExists(t, db, intuneAdmin.ID, linuxDevice.ID, azschema.ExecuteCommand)
}

// TestUserRoleAssignmentsIntegration verifies UserRoleAssignments creates edges correctly
func TestUserRoleAssignmentsIntegration(t *testing.T) {
	g := seedAzureGraph(t)
	db := g.DB

	// Run UserRoleAssignments
	stats, err := azure.UserRoleAssignments(context.Background(), db)
	require.NoError(t, err)
	require.NotNil(t, stats)

	// Verify GlobalAdmin edge was created (users[0] has CompanyAdministratorRole)
	requireRelExists(t, db, g.Users[0].ID, g.Tenant.ID, azschema.GlobalAdmin)
}

// TestFixManagementGroupNamesIntegration tests management group name formatting
func TestFixManagementGroupNamesIntegration(t *testing.T) {
	g := seedAzureGraph(t)
	db := g.DB

	mgGroup := createManagementGroup(t, db, "mg-001", "Management Group")

	// Set tenantID and displayName
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		mgGroup.Properties.Set(azschema.TenantID.String(), TenantObjectID)
		mgGroup.Properties.Set("displayname", "MyMG")
		return tx.UpdateNode(mgGroup)
	}))

	// Run FixManagementGroupNames
	err := azure.FixManagementGroupNames(context.Background(), db)
	require.NoError(t, err)

	// Verify the name was updated
	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		node, err := tx.Nodes().Filter(query.Equals(query.NodeID(), mgGroup.ID)).First()
		require.NoError(t, err)
		name, err := node.Properties.Get("name").String()
		require.NoError(t, err)
		// Name should be formatted as "DISPLAYNAME@TENANTNAME"
		require.True(t, len(name) > 0 && name[0] == 'M', "expected management group name to start with M")
		return nil
	}))
}

// TestCreateAZRoleApproverEdgeIntegration tests approver edge creation
func TestCreateAZRoleApproverEdgeIntegration(t *testing.T) {
	g := seedAzureGraph(t)
	db := g.DB

	stats, err := azure.CreateAZRoleApproverEdge(context.Background(), db)
	require.NoError(t, err)
	require.NotNil(t, stats)
}

// TestAppRoleAssignmentsIntegration verifies AppRoleAssignments completes
func TestAppRoleAssignmentsIntegration(t *testing.T) {
	g := seedAzureGraph(t)
	db := g.DB

	stats, err := azure.AppRoleAssignments(context.Background(), db)
	require.NoError(t, err)
	require.NotNil(t, stats)
}
