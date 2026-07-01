//go:build e2e

package e2e_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Expected values captured from known-good run against kglite with sample data.
// If sample data changes, re-run with -v and update these values.
//
// NOTE: Values updated to reflect fix for relationship endpoint node matching and
// the kindsWritten fix in batch.go.
//
// - Total nodes is 1519 (not 1537): the earlier value was based on a buggy state that
//   created 18 extra duplicate primary-label Group nodes via wrong MERGE identity kinds.
// - AdminCount users is 40 (not 39): 40 users in the raw sample data have admincount=true.
// - Enabled domain admin users is 16 (not 15): 16 is the correct transitive count from
//   the raw data (including one user reachable via the SUBDAS@PHANTOM.CORP nested group).

var adExpectedCounts = map[string]int64{
	"Total nodes":                        1519,
	"Total relationships":                16663,
	"Computers":                          34,
	"Users":                              99,
	"Groups":                             243,
	"Kerberoastable users":               2,
	"AS-REP roastable users":             1,
	"AdminCount users":                   40,
	"AdminCount computers":               0,
	"Enabled domain admin users":         16,
	"DCSync relationships":               8,
	"HasSession relationships":           29,
	"AdminTo relationships":              24,
	"MemberOf relationships":             304,
	"ADCS cert templates":                106,
	"Enterprise CAs":                     4,
	"Computers with unconstrained delegation": 4,
	"OUs":  20,
	"GPOs": 32,
}

var azureExpectedCounts = map[string]int64{
	"Azure tenants":            1,
	"Azure users":              230,
	"Azure service principals": 6270,
	"Azure apps":               6648,
	"Azure VMs":                66,
	"Azure groups":             58,
	"AZGlobalAdmin relationships": 30,
	"AZOwns relationships":        2850,
}

var combinedExpectedCounts = map[string]int64{
	// AD queries against combined graph
	// NOTE: Total nodes (15074) and Total relationships (826355) differ from the sum of
	// individual dataset totals due to cross-dataset node deduplication and analysis
	// adding relationships. These were verified against a correct kglite run.
	"Total nodes":                        15074,
	"Total relationships":                826364,
	"Computers":                          34,
	"Users":                              111,
	"Groups":                             260,
	"Kerberoastable users":               2,
	"AS-REP roastable users":             1,
	"AdminCount users":                   40,
	"AdminCount computers":               0,
	"Enabled domain admin users":         16,
	"DCSync relationships":               8,
	"HasSession relationships":           29,
	"AdminTo relationships":              24,
	"MemberOf relationships":             304,
	"ADCS cert templates":                106,
	"Enterprise CAs":                     4,
	"Computers with unconstrained delegation": 4,
	"OUs":  20,
	"GPOs": 32,
	// Azure queries against combined graph
	"Azure tenants":            1,
	"Azure users":              230,
	"Azure service principals": 6270,
	"Azure apps":               6648,
	"Azure VMs":                66,
	"Azure groups":             58,
	"AZGlobalAdmin relationships": 30,
	"AZOwns relationships":        2850,
}

// TestADIngestAssertions uses the shared pre-loaded AD graph and asserts exact query results.
func TestADIngestAssertions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := sharedADGraph(t)

	for _, q := range adPresetQueries {
		expected, ok := adExpectedCounts[q.Name]
		if !ok {
			continue // skip queries that return non-count results (e.g., Domains returns rows)
		}
		q := q
		t.Run(q.Name, func(t *testing.T) {
		t.Parallel()
			got := runQueryInt64(ctx, t, db, q.Cypher)
			require.Equal(t, expected, got, "query: %s", q.Cypher)
		})
	}
}

// TestAzureIngestAssertions uses the shared pre-loaded Azure graph and asserts exact query results.
func TestAzureIngestAssertions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := sharedAzureGraph(t)

	for _, q := range azurePresetQueries {
		expected, ok := azureExpectedCounts[q.Name]
		if !ok {
			continue
		}
		q := q
		t.Run(q.Name, func(t *testing.T) {
		t.Parallel()
			got := runQueryInt64(ctx, t, db, q.Cypher)
			require.Equal(t, expected, got, "query: %s", q.Cypher)
		})
	}
}

// TestCombinedIngestAssertions uses the shared pre-loaded combined graph and asserts all query results.
func TestCombinedIngestAssertions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := sharedCombinedGraph(t)

	allQueries := append(adPresetQueries, azurePresetQueries...)
	for _, q := range allQueries {
		expected, ok := combinedExpectedCounts[q.Name]
		if !ok {
			continue
		}
		q := q
		t.Run(q.Name, func(t *testing.T) {
		t.Parallel()
			got := runQueryInt64(ctx, t, db, q.Cypher)
			require.Equal(t, expected, got, "query: %s", q.Cypher)
		})
	}
}
