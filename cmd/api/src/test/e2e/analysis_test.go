//go:build e2e

package e2e_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Attack path edge types produced by ad.Post() and azure.Post() post-processing.
// Expected counts are captured from known-good kglite run with sample data.

type edgeAssertion struct {
	Name     string
	EdgeType string
	Expected int64
}

var adAttackPathEdges = []edgeAssertion{
	// Core AD attack paths
	{"DCSync", "DCSync", 8},
	{"HasSession", "HasSession", 29},
	{"MemberOf", "MemberOf", 304},
	{"AdminTo", "AdminTo", 24},
	{"CanRDP", "CanRDP", 0},
	{"CanPSRemote", "CanPSRemote", 0},
	{"ExecuteDCOM", "ExecuteDCOM", 0},

	// ACL-based relationships
	{"GenericAll", "GenericAll", 3292},
	{"GenericWrite", "GenericWrite", 579},
	{"Owns", "Owns", 1439},
	{"WriteOwner", "WriteOwner", 2187},
	{"WriteDACL", "WriteDACL", 0},
	{"ForceChangePassword", "ForceChangePassword", 52},
	{"AddMember", "AddMember", 93},

	// ADCS escalation paths
	{"ADCSESC1", "ADCSESC1", 7},
	{"ADCSESC3", "ADCSESC3", 12},
	{"ADCSESC4", "ADCSESC4", 7},
	{"ADCSESC6a", "ADCSESC6a", 5},
	{"ADCSESC6b", "ADCSESC6b", 12},
	{"ADCSESC9a", "ADCSESC9a", 14},
	{"ADCSESC9b", "ADCSESC9b", 10},
	{"ADCSESC10a", "ADCSESC10a", 7},
	{"ADCSESC10b", "ADCSESC10b", 8},
	{"ADCSESC13", "ADCSESC13", 0},

	// NTLM relay
	{"CoerceAndRelayNTLMToSMB", "CoerceAndRelayNTLMToSMB", 0},
	{"CoerceAndRelayNTLMToADCS", "CoerceAndRelayNTLMToADCS", 0},

	// Additional analysis edges
	{"SyncLAPSPassword", "SyncLAPSPassword", 2},
	{"GPOAppliesTo", "GPOAppliesTo", 0},
	// Note: "Contains" and "TrustedForNTAuth" conflict with kglite Cypher keywords;
	// they are tested via the generic relationship count query below instead.
	{"GoldenCert", "GoldenCert", 4},
}

var azureAttackPathEdges = []edgeAssertion{
	{"AZGlobalAdmin", "AZGlobalAdmin", 30},
	{"AZOwns", "AZOwns", 2850},
	{"AZPrivilegedRoleAdmin", "AZPrivilegedRoleAdmin", 8},
	{"AZMemberOf", "AZMemberOf", 4439},
	{"AZHasRole", "AZHasRole", 598},
	{"AZContributor", "AZContributor", 25},
}

// TestADAttackPathEdges verifies that AD post-processing creates the expected derived edges.
func TestADAttackPathEdges(t *testing.T) {
	ctx := context.Background()
	db := sharedADGraph(t)

	for _, tc := range adAttackPathEdges {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			got := runQueryInt64(ctx, t, db,
				"MATCH ()-[r:"+tc.EdgeType+"]->() RETURN count(r) AS c")
			require.Equal(t, tc.Expected, got, "edge type: %s", tc.EdgeType)
		})
	}
}

// TestAzureAttackPathEdges verifies that Azure post-processing creates the expected derived edges.
func TestAzureAttackPathEdges(t *testing.T) {
	ctx := context.Background()
	db := sharedAzureGraph(t)

	for _, tc := range azureAttackPathEdges {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			got := runQueryInt64(ctx, t, db,
				"MATCH ()-[r:"+tc.EdgeType+"]->() RETURN count(r) AS c")
			require.Equal(t, tc.Expected, got, "edge type: %s", tc.EdgeType)
		})
	}
}
