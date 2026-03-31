//go:build e2e

package e2e_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Expected values captured from known-good run against kglite with sample data.
// If sample data changes, re-run with -v and update these values.

var adExpectedCounts = map[string]int64{
	"Total nodes":                        1519,
	"Total relationships":                16367,
	"Computers":                          0,
	"Users":                              0,
	"Groups":                             0,
	"Kerberoastable users":               0,
	"AS-REP roastable users":             0,
	"AdminCount users":                   0,
	"AdminCount computers":               0,
	"Enabled domain admin users":         0,
	"DCSync relationships":               8,
	"HasSession relationships":           29,
	"AdminTo relationships":              0,
	"MemberOf relationships":             304,
	"ADCS cert templates":                0,
	"Enterprise CAs":                     0,
	"Computers with unconstrained delegation": 0,
	"OUs":  0,
	"GPOs": 0,
}

var azureExpectedCounts = map[string]int64{
	"Azure tenants":            0,
	"Azure users":              0,
	"Azure service principals": 0,
	"Azure apps":               0,
	"Azure VMs":                0,
	"Azure groups":             0,
	"AZGlobalAdmin relationships": 0,
	"AZOwns relationships":        2850,
}

var combinedExpectedCounts = map[string]int64{
	// AD queries against combined graph
	"Total nodes":                        15074,
	"Total relationships":                43755,
	"Computers":                          0,
	"Users":                              0,
	"Groups":                             0,
	"Kerberoastable users":               0,
	"AS-REP roastable users":             0,
	"AdminCount users":                   0,
	"AdminCount computers":               0,
	"Enabled domain admin users":         0,
	"DCSync relationships":               8,
	"HasSession relationships":           29,
	"AdminTo relationships":              0,
	"MemberOf relationships":             304,
	"ADCS cert templates":                0,
	"Enterprise CAs":                     0,
	"Computers with unconstrained delegation": 0,
	"OUs":  0,
	"GPOs": 0,
	// Azure queries against combined graph
	"Azure tenants":            0,
	"Azure users":              0,
	"Azure service principals": 0,
	"Azure apps":               0,
	"Azure VMs":                0,
	"Azure groups":             0,
	"AZGlobalAdmin relationships": 0,
	"AZOwns relationships":        2850,
}

// TestADIngestAssertions loads AD sample data, runs analysis, and asserts exact query results.
func TestADIngestAssertions(t *testing.T) {
	ctx := context.Background()
	adZip := filepath.Join(testdataDir(), "ad_sampledata.zip")
	skipIfMissing(t, adZip)

	db := openGraph(t)
	schema := loadIngestSchema(t)
	ingestZip(ctx, t, db, adZip, schema)
	runAnalysis(ctx, t, db)

	for _, q := range adPresetQueries {
		expected, ok := adExpectedCounts[q.Name]
		if !ok {
			continue // skip queries that return non-count results (e.g., Domains returns rows)
		}
		q := q
		t.Run(q.Name, func(t *testing.T) {
			got := runQueryInt64(ctx, t, db, q.Cypher)
			require.Equal(t, expected, got, "query: %s", q.Cypher)
		})
	}
}

// TestAzureIngestAssertions loads Azure sample data, runs analysis, and asserts exact query results.
func TestAzureIngestAssertions(t *testing.T) {
	ctx := context.Background()
	azureZip := filepath.Join(testdataDir(), "entra_sampledata.zip")
	skipIfMissing(t, azureZip)

	db := openGraph(t)
	schema := loadIngestSchema(t)
	ingestZip(ctx, t, db, azureZip, schema)
	runAnalysis(ctx, t, db)

	for _, q := range azurePresetQueries {
		expected, ok := azureExpectedCounts[q.Name]
		if !ok {
			continue
		}
		q := q
		t.Run(q.Name, func(t *testing.T) {
			got := runQueryInt64(ctx, t, db, q.Cypher)
			require.Equal(t, expected, got, "query: %s", q.Cypher)
		})
	}
}

// TestCombinedIngestAssertions loads both datasets, runs analysis, and asserts all query results.
func TestCombinedIngestAssertions(t *testing.T) {
	ctx := context.Background()
	adZip := filepath.Join(testdataDir(), "ad_sampledata.zip")
	azureZip := filepath.Join(testdataDir(), "entra_sampledata.zip")
	skipIfMissing(t, adZip)
	skipIfMissing(t, azureZip)

	db := openGraph(t)
	schema := loadIngestSchema(t)
	ingestZip(ctx, t, db, adZip, schema)
	ingestZip(ctx, t, db, azureZip, schema)
	runAnalysis(ctx, t, db)

	allQueries := append(adPresetQueries, azurePresetQueries...)
	for _, q := range allQueries {
		expected, ok := combinedExpectedCounts[q.Name]
		if !ok {
			continue
		}
		q := q
		t.Run(q.Name, func(t *testing.T) {
			got := runQueryInt64(ctx, t, db, q.Cypher)
			require.Equal(t, expected, got, "query: %s", q.Cypher)
		})
	}
}
