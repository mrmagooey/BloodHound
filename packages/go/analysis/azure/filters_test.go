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
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/ops"
	"github.com/specterops/dawgs/query"
	"github.com/stretchr/testify/require"
)

// ========================================================================
// FilterEntityActiveAssignments
// ========================================================================

func TestFilterEntityActiveAssignments(t *testing.T) {
	db := openTestGraph(t)
	user := createUser(t, db, TenantObjectID, "user-001", "TestUser")
	role := createRole(t, db, "role-001", "Test Role", "test-template")
	group := createGroup(t, db, "group-001", "Test Group", false)

	// Create HasRole relationship - should match
	createRel(t, db, user, role, azschema.HasRole)

	// Create MemberOf relationship - should match
	createRel(t, db, user, group, azschema.MemberOf)

	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// HasRole should match filter
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterEntityActiveAssignments(),
				query.Kind(query.Relationship(), azschema.HasRole),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "HasRole should match FilterEntityActiveAssignments")

		// MemberOf should match filter
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterEntityActiveAssignments(),
				query.Kind(query.Relationship(), azschema.MemberOf),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "MemberOf should match FilterEntityActiveAssignments")

		return nil
	}))
}

// ========================================================================
// FilterEntityPIMAssignments
// ========================================================================

func TestFilterEntityPIMAssignments(t *testing.T) {
	db := openTestGraph(t)
	user := createUser(t, db, TenantObjectID, "user-001", "TestUser")
	role := createRole(t, db, "role-001", "Test Role", "test-template")
	group := createGroup(t, db, "group-001", "Test Group", false)

	// Create Grant relationship - should match
	createRel(t, db, user, role, azschema.Grant)

	// Create GrantSelf relationship - should match
	createRel(t, db, user, role, azschema.GrantSelf)

	// Create MemberOf relationship - should match
	createRel(t, db, user, group, azschema.MemberOf)

	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// Grant should match
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterEntityPIMAssignments(),
				query.Kind(query.Relationship(), azschema.Grant),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "Grant should match FilterEntityPIMAssignments")

		// GrantSelf should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterEntityPIMAssignments(),
				query.Kind(query.Relationship(), azschema.GrantSelf),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "GrantSelf should match FilterEntityPIMAssignments")

		// MemberOf should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterEntityPIMAssignments(),
				query.Kind(query.Relationship(), azschema.MemberOf),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "MemberOf should match FilterEntityPIMAssignments")

		return nil
	}))
}

// ========================================================================
// FilterRoleApprovers
// ========================================================================

func TestFilterRoleApprovers(t *testing.T) {
	db := openTestGraph(t)
	user := createUser(t, db, TenantObjectID, "user-001", "TestUser")
	role := createRole(t, db, "role-001", "Test Role", "test-template")
	group := createGroup(t, db, "group-001", "Test Group", false)

	// Create AZRoleApprover relationship - should match
	createRel(t, db, user, role, azschema.AZRoleApprover)

	// Create MemberOf relationship - should match
	createRel(t, db, user, group, azschema.MemberOf)

	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// AZRoleApprover should match
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterRoleApprovers(),
				query.Kind(query.Relationship(), azschema.AZRoleApprover),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "AZRoleApprover should match FilterRoleApprovers")

		// MemberOf should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterRoleApprovers(),
				query.Kind(query.Relationship(), azschema.MemberOf),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "MemberOf should match FilterRoleApprovers")

		return nil
	}))
}

// ========================================================================
// FilterExecutionPrivileges
// ========================================================================

func TestFilterExecutionPrivileges(t *testing.T) {
	db := openTestGraph(t)
	user := createUser(t, db, TenantObjectID, "user-001", "TestUser")
	vm := createVM(t, db, "vm-001", "TestVM")

	// Create VMAdminLogin relationship (part of ExecutionPrivileges) - should match
	createRel(t, db, user, vm, azschema.VMAdminLogin)

	// Create VMContributor relationship (part of ExecutionPrivileges) - should match
	createRel(t, db, user, vm, azschema.VMContributor)

	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// VMAdminLogin should match
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterExecutionPrivileges(),
				query.Kind(query.Relationship(), azschema.VMAdminLogin),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "VMAdminLogin should match FilterExecutionPrivileges")

		// VMContributor should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterExecutionPrivileges(),
				query.Kind(query.Relationship(), azschema.VMContributor),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "VMContributor should match FilterExecutionPrivileges")

		return nil
	}))
}

// ========================================================================
// FilterKeyReaders
// ========================================================================

func TestFilterKeyReaders(t *testing.T) {
	db := openTestGraph(t)
	user := createUser(t, db, TenantObjectID, "user-001", "TestUser")
	keyVault := createKeyVault(t, db, "kv-001", "TestKeyVault")

	// Create GetKeys relationship - should match
	createRel(t, db, user, keyVault, azschema.GetKeys)

	// Create Owner relationship - should match
	createRel(t, db, user, keyVault, azschema.Owner)

	// Create Contributor relationship - should match
	createRel(t, db, user, keyVault, azschema.Contributor)

	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// GetKeys should match
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterKeyReaders(),
				query.Kind(query.Relationship(), azschema.GetKeys),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "GetKeys should match FilterKeyReaders")

		// Owner should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterKeyReaders(),
				query.Kind(query.Relationship(), azschema.Owner),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "Owner should match FilterKeyReaders")

		// Contributor should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterKeyReaders(),
				query.Kind(query.Relationship(), azschema.Contributor),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "Contributor should match FilterKeyReaders")

		return nil
	}))
}

// ========================================================================
// FilterCertificateReaders
// ========================================================================

func TestFilterCertificateReaders(t *testing.T) {
	db := openTestGraph(t)
	user := createUser(t, db, TenantObjectID, "user-001", "TestUser")
	keyVault := createKeyVault(t, db, "kv-001", "TestKeyVault")

	// Create GetCertificates relationship - should match
	createRel(t, db, user, keyVault, azschema.GetCertificates)

	// Create Owner relationship - should match
	createRel(t, db, user, keyVault, azschema.Owner)

	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// GetCertificates should match
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterCertificateReaders(),
				query.Kind(query.Relationship(), azschema.GetCertificates),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "GetCertificates should match FilterCertificateReaders")

		// Owner should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterCertificateReaders(),
				query.Kind(query.Relationship(), azschema.Owner),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "Owner should match FilterCertificateReaders")

		return nil
	}))
}

// ========================================================================
// FilterSecretReaders
// ========================================================================

func TestFilterSecretReaders(t *testing.T) {
	db := openTestGraph(t)
	user := createUser(t, db, TenantObjectID, "user-001", "TestUser")
	keyVault := createKeyVault(t, db, "kv-001", "TestKeyVault")

	// Create GetSecrets relationship - should match
	createRel(t, db, user, keyVault, azschema.GetSecrets)

	// Create Owner relationship - should match
	createRel(t, db, user, keyVault, azschema.Owner)

	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// GetSecrets should match
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterSecretReaders(),
				query.Kind(query.Relationship(), azschema.GetSecrets),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "GetSecrets should match FilterSecretReaders")

		// Owner should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterSecretReaders(),
				query.Kind(query.Relationship(), azschema.Owner),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "Owner should match FilterSecretReaders")

		return nil
	}))
}

// ========================================================================
// FilterControlsRelationships
// ========================================================================

func TestFilterControlsRelationships(t *testing.T) {
	db := openTestGraph(t)
	user := createUser(t, db, TenantObjectID, "user-001", "TestUser")
	vm := createVM(t, db, "vm-001", "TestVM")
	tenant := createTenant(t, db, "tenant-new", "New Tenant")
	group := createGroup(t, db, "group-001", "Test Group", false)

	// Create Owner relationship (part of ControlRelationships) - should match
	createRel(t, db, user, vm, azschema.Owner)

	// Create Contributor relationship (part of ControlRelationships) - should match
	createRel(t, db, user, vm, azschema.Contributor)

	// Create Contains relationship - should match
	createRel(t, db, tenant, group, azschema.Contains)

	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// Owner should match
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterControlsRelationships(),
				query.Kind(query.Relationship(), azschema.Owner),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "Owner should match FilterControlsRelationships")

		// Contributor should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterControlsRelationships(),
				query.Kind(query.Relationship(), azschema.Contributor),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "Contributor should match FilterControlsRelationships")

		// Contains should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterControlsRelationships(),
				query.Kind(query.Relationship(), azschema.Contains),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "Contains should match FilterControlsRelationships")

		return nil
	}))
}

// ========================================================================
// FilterAppRoleAssignmentTransitRelationships
// ========================================================================

func TestFilterAppRoleAssignmentTransitRelationships(t *testing.T) {
	db := openTestGraph(t)
	sp := createServicePrincipal(t, db, "sp-001", "TestSP")
	app := createApplication(t, db, "app-001", "TestApp")

	// Create AZMGAddMember relationship (part of AppRoleTransitRelationshipKinds) - should match
	createRel(t, db, sp, app, azschema.AZMGAddMember)

	// Create AZMGAddOwner relationship (part of AppRoleTransitRelationshipKinds) - should match
	createRel(t, db, sp, app, azschema.AZMGAddOwner)

	// Create AZMGGrantAppRoles relationship (part of AppRoleTransitRelationshipKinds) - should match
	createRel(t, db, sp, app, azschema.AZMGGrantAppRoles)

	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// AZMGAddMember should match
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterAppRoleAssignmentTransitRelationships(),
				query.Kind(query.Relationship(), azschema.AZMGAddMember),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "AZMGAddMember should match FilterAppRoleAssignmentTransitRelationships")

		// AZMGAddOwner should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterAppRoleAssignmentTransitRelationships(),
				query.Kind(query.Relationship(), azschema.AZMGAddOwner),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "AZMGAddOwner should match FilterAppRoleAssignmentTransitRelationships")

		// AZMGGrantAppRoles should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterAppRoleAssignmentTransitRelationships(),
				query.Kind(query.Relationship(), azschema.AZMGGrantAppRoles),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "AZMGGrantAppRoles should match FilterAppRoleAssignmentTransitRelationships")

		return nil
	}))
}

// ========================================================================
// FilterAbusableAppRoleAssignmentRelationships
// ========================================================================

func TestFilterAbusableAppRoleAssignmentRelationships(t *testing.T) {
	db := openTestGraph(t)
	sp := createServicePrincipal(t, db, "sp-001", "TestSP")
	app := createApplication(t, db, "app-001", "TestApp")

	// Create ApplicationReadWriteAll relationship (part of AbusableAppRoleRelationshipKinds) - should match
	createRel(t, db, sp, app, azschema.ApplicationReadWriteAll)

	// Create DirectoryReadWriteAll relationship (part of AbusableAppRoleRelationshipKinds) - should match
	createRel(t, db, sp, app, azschema.DirectoryReadWriteAll)

	// Create GroupReadWriteAll relationship (part of AbusableAppRoleRelationshipKinds) - should match
	createRel(t, db, sp, app, azschema.GroupReadWriteAll)

	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// ApplicationReadWriteAll should match
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterAbusableAppRoleAssignmentRelationships(),
				query.Kind(query.Relationship(), azschema.ApplicationReadWriteAll),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "ApplicationReadWriteAll should match FilterAbusableAppRoleAssignmentRelationships")

		// DirectoryReadWriteAll should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterAbusableAppRoleAssignmentRelationships(),
				query.Kind(query.Relationship(), azschema.DirectoryReadWriteAll),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "DirectoryReadWriteAll should match FilterAbusableAppRoleAssignmentRelationships")

		// GroupReadWriteAll should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterAbusableAppRoleAssignmentRelationships(),
				query.Kind(query.Relationship(), azschema.GroupReadWriteAll),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "GroupReadWriteAll should match FilterAbusableAppRoleAssignmentRelationships")

		return nil
	}))
}

// ========================================================================
// FilterGroupMembership
// ========================================================================

func TestFilterGroupMembership(t *testing.T) {
	db := openTestGraph(t)
	user := createUser(t, db, TenantObjectID, "user-001", "TestUser")
	group := createGroup(t, db, "group-001", "Test Group", false)

	// Create MemberOf relationship - should match
	createRel(t, db, user, group, azschema.MemberOf)

	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// MemberOf should match
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterGroupMembership(),
				query.Kind(query.Relationship(), azschema.MemberOf),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "MemberOf should match FilterGroupMembership")

		return nil
	}))
}

// ========================================================================
// FilterGroupMembers
// ========================================================================

func TestFilterGroupMembers(t *testing.T) {
	db := openTestGraph(t)
	user := createUser(t, db, TenantObjectID, "user-001", "TestUser")
	group1 := createGroup(t, db, "group-001", "Test Group 1", false)
	group2 := createGroup(t, db, "group-002", "Test Group 2", false)

	// Create MemberOf relationship from user (Entity type) - should match
	createRel(t, db, user, group1, azschema.MemberOf)

	// Create MemberOf relationship from group (Entity type) to another group - should match
	createRel(t, db, group2, group1, azschema.MemberOf)

	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// User as start node should match
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterGroupMembers(),
				query.Kind(query.Start(), azschema.User),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "User as start node should match FilterGroupMembers")

		// Group as start node should match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterGroupMembers(),
				query.Kind(query.Start(), azschema.Group),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "Group as start node should match FilterGroupMembers")

		return nil
	}))
}

// ========================================================================
// FilterRoleAssignableGroupMembersUsers
// ========================================================================

func TestFilterRoleAssignableGroupMembersUsers(t *testing.T) {
	db := openTestGraph(t)
	user := createUser(t, db, TenantObjectID, "user-001", "TestUser")

	// Create a group that will be set with IsAssignableToRole = "true" (as string)
	var ragNode *graph.Node
	require.NoError(t, db.WriteTransaction(context.Background(), func(tx graph.Transaction) error {
		var err error
		props := graph.AsProperties(map[string]any{
			"objectid":             "rag-001",
			"name":                 "Role Assignable Group",
			"isassignabletorole": "true", // Set as string to match filter
		})
		ragNode, err = tx.CreateNode(props, azschema.Entity, azschema.Group)
		return err
	}))

	nonRoleAssignableGroup := createGroup(t, db, "group-001", "Non-Role Assignable Group", false)

	// Create MemberOf from user to role-assignable group - should match
	createRel(t, db, user, ragNode, azschema.MemberOf)

	// Create MemberOf from user to non-role-assignable group - should NOT match
	createRel(t, db, user, nonRoleAssignableGroup, azschema.MemberOf)

	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// User to role-assignable group should match
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterRoleAssignableGroupMembersUsers(),
				query.Equals(query.EndID(), ragNode.ID),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "User to role-assignable group should match FilterRoleAssignableGroupMembersUsers")

		// User to non-role-assignable group should NOT match
		rels, err = ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterRoleAssignableGroupMembersUsers(),
				query.Equals(query.EndID(), nonRoleAssignableGroup.ID),
			)
		}))
		require.NoError(t, err)
		require.Equal(t, 0, len(rels), "User to non-role-assignable group should NOT match FilterRoleAssignableGroupMembersUsers")

		return nil
	}))
}

// ========================================================================
// FilterContains
// ========================================================================

func TestFilterContains(t *testing.T) {
	db := openTestGraph(t)
	tenant := createTenant(t, db, "tenant-001", "Test Tenant")
	user := createUser(t, db, TenantObjectID, "user-001", "TestUser")

	// Create Contains relationship - should match
	createRel(t, db, tenant, user, azschema.Contains)

	require.NoError(t, db.ReadTransaction(context.Background(), func(tx graph.Transaction) error {
		// Contains should match
		rels, err := ops.FetchRelationships(tx.Relationships().Filterf(func() graph.Criteria {
			return query.And(
				azure.FilterContains(),
				query.Kind(query.Relationship(), azschema.Contains),
			)
		}))
		require.NoError(t, err)
		require.Greater(t, len(rels), 0, "Contains should match FilterContains")

		return nil
	}))
}
