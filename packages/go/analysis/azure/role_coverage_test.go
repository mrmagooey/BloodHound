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
	"testing"

	"github.com/specterops/bloodhound/packages/go/analysis/azure"
	azschema "github.com/specterops/bloodhound/packages/go/graphschema/azure"
	"github.com/specterops/bloodhound/packages/go/graphschema/common"
	"github.com/specterops/dawgs/cardinality"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========================================================================
// post.go — static/pure functions returning role ID slices
// ========================================================================

func TestAddMemberAllGroupsTargetRoles(t *testing.T) {
	roles := azure.AddMemberAllGroupsTargetRoles()
	assert.NotEmpty(t, roles)
	assert.Contains(t, roles, azschema.CompanyAdministratorRole)
	assert.Contains(t, roles, azschema.PrivilegedRoleAdministratorRole)
	assert.Len(t, roles, 2)
}

func TestAddMemberGroupNotRoleAssignableTargetRoles(t *testing.T) {
	roles := azure.AddMemberGroupNotRoleAssignableTargetRoles()
	assert.NotEmpty(t, roles)
	assert.Contains(t, roles, azschema.GroupsAdministratorRole)
	assert.Contains(t, roles, azschema.DirectoryWritersRole)
	assert.Contains(t, roles, azschema.IdentityGovernanceAdministrator)
	assert.Contains(t, roles, azschema.UserAccountAdministratorRole)
	assert.Contains(t, roles, azschema.IntuneServiceAdministratorRole)
	assert.Contains(t, roles, azschema.KnowledgeAdministratorRole)
	assert.Contains(t, roles, azschema.KnowledgeManagerRole)
	assert.Len(t, roles, 7)
}

func TestResetPasswordRoleIDs(t *testing.T) {
	roles := azure.ResetPasswordRoleIDs()
	assert.NotEmpty(t, roles)
	assert.Contains(t, roles, azschema.CompanyAdministratorRole)
	assert.Contains(t, roles, azschema.PrivilegedAuthenticationAdministratorRole)
	assert.Contains(t, roles, azschema.PartnerTier2SupportRole)
	assert.Contains(t, roles, azschema.HelpdeskAdministratorRole)
	assert.Contains(t, roles, azschema.AuthenticationAdministratorRole)
	assert.Contains(t, roles, azschema.UserAccountAdministratorRole)
	assert.Contains(t, roles, azschema.PasswordAdministratorRole)
	assert.Contains(t, roles, azschema.PartnerTier1SupportRole)
	assert.Len(t, roles, 8)
}

func TestAddSecretRoleIDs(t *testing.T) {
	roles := azure.AddSecretRoleIDs()
	assert.NotEmpty(t, roles)
	assert.Contains(t, roles, azschema.ApplicationAdministratorRole)
	assert.Contains(t, roles, azschema.CloudApplicationAdministratorRole)
	assert.Len(t, roles, 2)
}

func TestHelpdeskAdministratorPasswordResetTargetRoles(t *testing.T) {
	roles := azure.HelpdeskAdministratorPasswordResetTargetRoles()
	assert.NotEmpty(t, roles)
	assert.Contains(t, roles, azschema.ReportsReaderRole)
	assert.Contains(t, roles, azschema.MessageCenterReaderRole)
	assert.Contains(t, roles, azschema.HelpdeskAdministratorRole)
	assert.Contains(t, roles, azschema.GuestInviterRole)
	assert.Contains(t, roles, azschema.DirectoryReadersRole)
	assert.Contains(t, roles, azschema.PasswordAdministratorRole)
	assert.Contains(t, roles, azschema.UsageSummaryReportsReaderRole)
	assert.Len(t, roles, 7)
}

func TestAuthenticationAdministratorPasswordResetTargetRoles(t *testing.T) {
	roles := azure.AuthenticationAdministratorPasswordResetTargetRoles()
	assert.NotEmpty(t, roles)
	assert.Contains(t, roles, azschema.AuthenticationAdministratorRole)
	assert.Contains(t, roles, azschema.ReportsReaderRole)
	assert.Contains(t, roles, azschema.MessageCenterReaderRole)
	assert.Contains(t, roles, azschema.GuestInviterRole)
	assert.Contains(t, roles, azschema.DirectoryReadersRole)
	assert.Contains(t, roles, azschema.PasswordAdministratorRole)
	assert.Contains(t, roles, azschema.UsageSummaryReportsReaderRole)
	assert.Len(t, roles, 7)
}

func TestUserAdministratorPasswordResetTargetRoles(t *testing.T) {
	roles := azure.UserAdministratorPasswordResetTargetRoles()
	assert.NotEmpty(t, roles)
	assert.Contains(t, roles, azschema.UserAccountAdministratorRole)
	assert.Contains(t, roles, azschema.ReportsReaderRole)
	assert.Contains(t, roles, azschema.MessageCenterReaderRole)
	assert.Contains(t, roles, azschema.HelpdeskAdministratorRole)
	assert.Contains(t, roles, azschema.GuestInviterRole)
	assert.Contains(t, roles, azschema.DirectoryReadersRole)
	assert.Contains(t, roles, azschema.PasswordAdministratorRole)
	assert.Contains(t, roles, azschema.UsageSummaryReportsReaderRole)
	assert.Contains(t, roles, azschema.GroupsAdministratorRole)
	assert.Len(t, roles, 9)
}

func TestPasswordAdministratorPasswordResetTargetRoles(t *testing.T) {
	roles := azure.PasswordAdministratorPasswordResetTargetRoles()
	assert.NotEmpty(t, roles)
	assert.Contains(t, roles, azschema.PasswordAdministratorRole)
	assert.Contains(t, roles, azschema.GuestInviterRole)
	assert.Contains(t, roles, azschema.DirectoryReadersRole)
	assert.Len(t, roles, 3)
}

// ========================================================================
// post.go — IsWindowsDevice
// ========================================================================

func TestIsWindowsDevice(t *testing.T) {
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
				props.Set(common.OperatingSystem.String(), tc.os)
			}
			node := graph.NewNode(100, props, azschema.Device)
			result, err := azure.IsWindowsDevice(node)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// ========================================================================
// role.go — RoleAssignmentMap
// ========================================================================

func TestRoleAssignmentMap_UserHasRoles(t *testing.T) {
	ram := azure.RoleAssignmentMap{
		graph.ID(10): {"role-a": {}, "role-b": {}},
	}

	nodeWithRoles := &graph.Node{ID: 10}
	nodeWithoutRoles := &graph.Node{ID: 99}

	assert.True(t, ram.UserHasRoles(nodeWithRoles))
	assert.False(t, ram.UserHasRoles(nodeWithoutRoles))
}

func TestRoleAssignmentMap_HasRole(t *testing.T) {
	ram := azure.RoleAssignmentMap{
		graph.ID(10): {"role-a": {}, "role-b": {}},
		graph.ID(20): {"role-c": {}},
	}

	assert.True(t, ram.HasRole(graph.ID(10), "role-a"))
	assert.True(t, ram.HasRole(graph.ID(10), "role-b"))
	assert.True(t, ram.HasRole(graph.ID(10), "role-x", "role-a"))
	assert.False(t, ram.HasRole(graph.ID(10), "role-c"))
	assert.False(t, ram.HasRole(graph.ID(99), "role-a"))
	assert.True(t, ram.HasRole(graph.ID(20), "role-c"))
}

// ========================================================================
// role.go — RoleAssignments accessor methods
// ========================================================================

// setupRoleAssignmentsExtended creates a more complex RoleAssignments for
// exercising methods not covered by the existing test setup.
func setupRoleAssignmentsExtended() azure.RoleAssignments {
	sp := graph.NewNode(10, graph.NewProperties(), azschema.ServicePrincipal)
	sp2 := graph.NewNode(11, graph.NewProperties(), azschema.ServicePrincipal)
	u1 := graph.NewNode(20, graph.NewProperties(), azschema.User)
	u2 := graph.NewNode(21, graph.NewProperties(), azschema.User)
	u3 := graph.NewNode(22, graph.NewProperties(), azschema.User) // no roles
	g1 := graph.NewNode(30, graph.NewProperties(), azschema.Group)

	roleMap := map[string]cardinality.Duplex[uint64]{
		azschema.CompanyAdministratorRole:  cardinality.NewBitmap64(),
		azschema.ReportsReaderRole:         cardinality.NewBitmap64(),
		azschema.HelpdeskAdministratorRole: cardinality.NewBitmap64(),
	}
	// u1 = GlobalAdmin only
	roleMap[azschema.CompanyAdministratorRole].Add(uint64(u1.ID))
	// u2 = ReportsReader + HelpdeskAdmin
	roleMap[azschema.ReportsReaderRole].Add(uint64(u2.ID))
	roleMap[azschema.HelpdeskAdministratorRole].Add(uint64(u2.ID))
	// sp = GlobalAdmin
	roleMap[azschema.CompanyAdministratorRole].Add(uint64(sp.ID))
	// sp2 = ReportsReader
	roleMap[azschema.ReportsReaderRole].Add(uint64(sp2.ID))
	// g1 = HelpdeskAdmin
	roleMap[azschema.HelpdeskAdministratorRole].Add(uint64(g1.ID))

	ragMembership := cardinality.NewBitmap64()
	ragMembership.Add(uint64(u1.ID))

	return azure.RoleAssignments{
		Principals:                    graph.NewNodeSet(sp, sp2, u1, u2, u3, g1).KindSet(),
		RoleMap:                       roleMap,
		RoleAssignableGroupMembership: ragMembership,
	}
}

func TestRoleAssignments_Users(t *testing.T) {
	ra := setupRoleAssignmentsExtended()
	users := ra.Users()
	// u1=20, u2=21, u3=22
	assert.True(t, users.Contains(20))
	assert.True(t, users.Contains(21))
	assert.True(t, users.Contains(22))
	assert.False(t, users.Contains(10)) // SP
	assert.False(t, users.Contains(30)) // Group
}

func TestRoleAssignments_ServicePrincipals(t *testing.T) {
	ra := setupRoleAssignmentsExtended()
	sps := ra.ServicePrincipals()
	assert.True(t, sps.Contains(10))
	assert.True(t, sps.Contains(11))
	assert.False(t, sps.Contains(20)) // User
	assert.False(t, sps.Contains(30)) // Group
}

func TestRoleAssignments_UsersWithAnyRole(t *testing.T) {
	ra := setupRoleAssignmentsExtended()
	usersWithRoles := ra.UsersWithAnyRole()
	assert.True(t, usersWithRoles.Contains(20))  // u1 has GlobalAdmin
	assert.True(t, usersWithRoles.Contains(21))  // u2 has ReportsReader + Helpdesk
	assert.False(t, usersWithRoles.Contains(22)) // u3 has no roles
	assert.False(t, usersWithRoles.Contains(10)) // SP, not user
}

func TestRoleAssignments_UsersWithoutRoles_Extended(t *testing.T) {
	ra := setupRoleAssignmentsExtended()
	noRoles := ra.UsersWithoutRoles()
	assert.True(t, noRoles.Contains(22))  // u3
	assert.False(t, noRoles.Contains(20)) // u1
	assert.False(t, noRoles.Contains(21)) // u2
}

func TestRoleAssignments_UsersWithRole(t *testing.T) {
	ra := setupRoleAssignmentsExtended()

	globalAdminUsers := ra.UsersWithRole(azschema.CompanyAdministratorRole)
	assert.True(t, globalAdminUsers.Contains(20))  // u1
	assert.False(t, globalAdminUsers.Contains(21)) // u2 doesn't have GlobalAdmin
	assert.False(t, globalAdminUsers.Contains(10)) // SP excluded

	helpdeskUsers := ra.UsersWithRole(azschema.HelpdeskAdministratorRole)
	assert.True(t, helpdeskUsers.Contains(21))  // u2
	assert.False(t, helpdeskUsers.Contains(20)) // u1 doesn't have Helpdesk
}

func TestRoleAssignments_ServicePrincipalsWithRole(t *testing.T) {
	ra := setupRoleAssignmentsExtended()

	globalAdminSPs := ra.ServicePrincipalsWithRole(azschema.CompanyAdministratorRole)
	assert.True(t, globalAdminSPs.Contains(10))  // sp
	assert.False(t, globalAdminSPs.Contains(11)) // sp2 doesn't have GlobalAdmin
	assert.False(t, globalAdminSPs.Contains(20)) // User excluded

	reportsReaderSPs := ra.ServicePrincipalsWithRole(azschema.ReportsReaderRole)
	assert.True(t, reportsReaderSPs.Contains(11))  // sp2
	assert.False(t, reportsReaderSPs.Contains(10)) // sp doesn't have ReportsReader
}

func TestRoleAssignments_UsersWithRolesExclusive(t *testing.T) {
	ra := setupRoleAssignmentsExtended()

	// u1 only has GlobalAdmin - should match when querying for GlobalAdmin exclusively
	exclusiveGlobalAdmin := ra.UsersWithRolesExclusive(azschema.CompanyAdministratorRole)
	assert.True(t, exclusiveGlobalAdmin.Contains(20))  // u1 only has GlobalAdmin
	assert.False(t, exclusiveGlobalAdmin.Contains(21)) // u2 has other roles too

	// u2 has ReportsReader + HelpdeskAdmin - should match when querying both exclusively
	exclusiveBoth := ra.UsersWithRolesExclusive(azschema.ReportsReaderRole, azschema.HelpdeskAdministratorRole)
	assert.True(t, exclusiveBoth.Contains(21))  // u2 only has these two
	assert.False(t, exclusiveBoth.Contains(20)) // u1 has GlobalAdmin (excluded by other roles)
}

func TestRoleAssignments_UsersWithRoleAssignableGroupMembership(t *testing.T) {
	ra := setupRoleAssignmentsExtended()
	ragMembers := ra.UsersWithRoleAssignableGroupMembership()
	assert.True(t, ragMembers.Contains(20))  // u1 was added to RAG membership
	assert.False(t, ragMembers.Contains(21)) // u2 not in RAG
}

func TestRoleAssignments_PrincipalsWithRole(t *testing.T) {
	ra := setupRoleAssignmentsExtended()

	// Query for multiple roles
	result := ra.PrincipalsWithRole(azschema.CompanyAdministratorRole, azschema.ReportsReaderRole)
	assert.True(t, result.Contains(20))  // u1 (GlobalAdmin)
	assert.True(t, result.Contains(21))  // u2 (ReportsReader)
	assert.True(t, result.Contains(10))  // sp (GlobalAdmin)
	assert.True(t, result.Contains(11))  // sp2 (ReportsReader)
	assert.False(t, result.Contains(22)) // u3 (no roles)

	// Query for non-existent role
	empty := ra.PrincipalsWithRole("non-existent-role")
	assert.Equal(t, uint64(0), empty.Cardinality())
}

func TestRoleAssignments_PrincipalsWithRolesExclusive(t *testing.T) {
	ra := setupRoleAssignmentsExtended()

	// u1 (20) and sp (10) only have GlobalAdmin
	exclusive := ra.PrincipalsWithRolesExclusive(azschema.CompanyAdministratorRole)
	assert.True(t, exclusive.Contains(20))  // u1 only has GlobalAdmin
	assert.True(t, exclusive.Contains(10))  // sp only has GlobalAdmin
	assert.False(t, exclusive.Contains(21)) // u2 has other roles
	assert.False(t, exclusive.Contains(11)) // sp2 has ReportsReader (not in query)
}

func TestRoleAssignments_GetNodeKindSet(t *testing.T) {
	ra := setupRoleAssignmentsExtended()

	bm := cardinality.NewBitmap64()
	bm.Add(20) // u1
	bm.Add(10) // sp

	nks := ra.GetNodeKindSet(bm)
	userNodes := nks.Get(azschema.User)
	spNodes := nks.Get(azschema.ServicePrincipal)

	assert.Equal(t, 1, userNodes.Len())
	assert.Equal(t, 1, spNodes.Len())
}

func TestRoleAssignments_GetNodeSet(t *testing.T) {
	ra := setupRoleAssignmentsExtended()

	bm := cardinality.NewBitmap64()
	bm.Add(20) // u1
	bm.Add(10) // sp

	ns := ra.GetNodeSet(bm)
	assert.Equal(t, 2, ns.Len())
}

func TestRoleAssignments_NodesWithRolesExclusive_Extended(t *testing.T) {
	ra := setupRoleAssignmentsExtended()

	nks := ra.NodesWithRolesExclusive(azschema.CompanyAdministratorRole)
	userNodes := nks.Get(azschema.User)
	spNodes := nks.Get(azschema.ServicePrincipal)

	// u1 (20) only has GlobalAdmin
	assert.NotNil(t, userNodes.Get(graph.ID(20)))
	// sp (10) only has GlobalAdmin
	assert.NotNil(t, spNodes.Get(graph.ID(10)))
}

func TestRoleAssignments_NodeHasRole_Extended(t *testing.T) {
	ra := setupRoleAssignmentsExtended()

	assert.True(t, ra.NodeHasRole(graph.ID(20), azschema.CompanyAdministratorRole))
	assert.False(t, ra.NodeHasRole(graph.ID(20), azschema.ReportsReaderRole))
	assert.True(t, ra.NodeHasRole(graph.ID(21), azschema.ReportsReaderRole, azschema.CompanyAdministratorRole))
	assert.False(t, ra.NodeHasRole(graph.ID(22), azschema.CompanyAdministratorRole))
	assert.False(t, ra.NodeHasRole(graph.ID(99), azschema.CompanyAdministratorRole)) // non-existent node
}
