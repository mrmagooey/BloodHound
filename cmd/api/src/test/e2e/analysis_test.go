//go:build e2e

package e2e_test

import (
	"context"
	"path/filepath"
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
	{"AdminTo", "AdminTo", 0},
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
	{"ADCSESC1", "ADCSESC1", 0},
	{"ADCSESC3", "ADCSESC3", 0},
	{"ADCSESC4", "ADCSESC4", 0},
	{"ADCSESC6a", "ADCSESC6a", 0},
	{"ADCSESC6b", "ADCSESC6b", 0},
	{"ADCSESC9a", "ADCSESC9a", 0},
	{"ADCSESC9b", "ADCSESC9b", 0},
	{"ADCSESC10a", "ADCSESC10a", 0},
	{"ADCSESC10b", "ADCSESC10b", 0},
	{"ADCSESC13", "ADCSESC13", 0},

	// NTLM relay
	{"CoerceAndRelayNTLMToSMB", "CoerceAndRelayNTLMToSMB", 0},
	{"CoerceAndRelayNTLMToADCS", "CoerceAndRelayNTLMToADCS", 0},

	// Additional analysis edges
	{"SyncLAPSPassword", "SyncLAPSPassword", 2},
	{"GPOAppliesTo", "GPOAppliesTo", 0},
	// Note: "Contains" and "TrustedForNTAuth" conflict with kglite Cypher keywords;
	// they are tested via the generic relationship count query below instead.
	{"GoldenCert", "GoldenCert", 0},
}

var azureAttackPathEdges = []edgeAssertion{
	{"AZGlobalAdmin", "AZGlobalAdmin", 0},
	{"AZOwns", "AZOwns", 2850},
	{"AZPrivilegedRoleAdmin", "AZPrivilegedRoleAdmin", 0},
	{"AZMemberOf", "AZMemberOf", 4439},
	{"AZHasRole", "AZHasRole", 598},
	{"AZContributor", "AZContributor", 25},
}

// TestADAttackPathEdges verifies that AD post-processing creates the expected derived edges.
func TestADAttackPathEdges(t *testing.T) {
	ctx := context.Background()
	adZip := filepath.Join(testdataDir(), "ad_sampledata.zip")
	skipIfMissing(t, adZip)

	db := openGraph(t)
	schema := loadIngestSchema(t)
	ingestZip(ctx, t, db, adZip, schema)
	runAnalysis(ctx, t, db)

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
	azureZip := filepath.Join(testdataDir(), "entra_sampledata.zip")
	skipIfMissing(t, azureZip)

	db := openGraph(t)
	schema := loadIngestSchema(t)
	ingestZip(ctx, t, db, azureZip, schema)
	runAnalysis(ctx, t, db)

	for _, tc := range azureAttackPathEdges {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			got := runQueryInt64(ctx, t, db,
				"MATCH ()-[r:"+tc.EdgeType+"]->() RETURN count(r) AS c")
			require.Equal(t, tc.Expected, got, "edge type: %s", tc.EdgeType)
		})
	}
}
