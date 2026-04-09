//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

// TestKNexusDiag runs diagnostic queries on kglite to understand node distribution
// and identify duplicate/stub nodes causing mismatches with Neo4j.
func TestKNexusDiag(t *testing.T) {
	ctx := context.Background()
	knexusZip := filepath.Join(testdataDir(), "k-nexusglobal_sampledata.zip")
	skipIfMissing(t, knexusZip)

	db := openGraph(t)
	schema := loadIngestSchema(t)

	t.Log("=== Ingesting into kglite ===")
	ingestZipTolerant(ctx, t, db, knexusZip, schema)
	t.Log("=== Running analysis ===")
	runAnalysis(ctx, t, db)

	diagQueries := []struct {
		name   string
		cypher string
	}{
		// Total counts
		{"total nodes", "MATCH (n) RETURN count(n) AS c"},
		{"total rels", "MATCH ()-[r]->() RETURN count(r) AS c"},

		// Nodes by primary label (top 30)
		{"nodes by primary label", "MATCH (n) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 30"},

		// Nodes with only 1 label (potential stubs without __kinds)
		{"single-label nodes", "MATCH (n) WHERE size(labels(n)) = 1 RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 20"},

		// Nodes with exactly "Base" label and nothing else
		{"Base-only nodes", "MATCH (n:Base) WHERE size(labels(n)) = 1 RETURN count(n) AS c"},
		{"AZBase-only nodes", "MATCH (n:AZBase) WHERE size(labels(n)) = 1 RETURN count(n) AS c"},

		// Check for duplicate objectids across different primary labels
		{"duplicate objectids (Base vs non-Base)",
			`MATCH (a:Base), (b) WHERE a <> b AND a.objectid = b.objectid AND NOT labels(b) CONTAINS '"Base"' RETURN labels(a) AS a_lbls, labels(b) AS b_lbls, a.objectid AS oid LIMIT 20`},

		// Nodes without __kinds property
		{"nodes without __kinds", "MATCH (n) WHERE n.__kinds IS NULL RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 20"},

		// Nodes WITH __kinds
		{"nodes with __kinds", "MATCH (n) WHERE n.__kinds IS NOT NULL RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 20"},

		// Count how many unique objectids exist
		{"unique objectids", "MATCH (n) WHERE n.objectid IS NOT NULL RETURN count(DISTINCT n.objectid) AS unique_oids"},
		{"total nodes with objectid", "MATCH (n) WHERE n.objectid IS NOT NULL RETURN count(n) AS c"},

		// Nodes whose objectid appears more than once
		{"nodes with duplicate objectids",
			`MATCH (n) WHERE n.objectid IS NOT NULL WITH n.objectid AS oid, count(n) AS cnt WHERE cnt > 1 RETURN oid, cnt ORDER BY cnt DESC LIMIT 20`},

		// Label breakdown for known types
		{"Users", "MATCH (n:User) RETURN count(n) AS c"},
		{"Groups", "MATCH (n:Group) RETURN count(n) AS c"},
		{"Computers", "MATCH (n:Computer) RETURN count(n) AS c"},
		{"AZTenants", "MATCH (n:AZTenant) RETURN count(n) AS c"},
		{"AZUsers", "MATCH (n:AZUser) RETURN count(n) AS c"},

		// OpenGraph types
		{"Okta nodes", "MATCH (n) WHERE n.__kinds CONTAINS 'Okta' RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10"},
		{"GitHub nodes", "MATCH (n) WHERE n.__kinds CONTAINS 'GH' OR n.__kinds CONTAINS 'GitHub' RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10"},
		{"Jamf nodes", "MATCH (n) WHERE n.__kinds CONTAINS 'Jamf' OR n.__kinds CONTAINS 'jamf' RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10"},
		{"SCIM nodes", "MATCH (n) WHERE n.__kinds CONTAINS 'SCIM' OR n.__kinds CONTAINS 'scim' RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 10"},
	}

	for _, q := range diagQueries {
		t.Logf("\n=== %s ===", q.name)
		t.Logf("  Cypher: %s", q.cypher)
		err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw(q.cypher, nil)
			defer result.Close()
			if result.Error() != nil {
				return result.Error()
			}
			row := 0
			for result.Next() {
				vals := result.Values()
				parts := make([]string, len(vals))
				for i, v := range vals {
					parts[i] = fmt.Sprintf("%v", v)
				}
				t.Logf("  row%02d: %s", row, strings.Join(parts, " | "))
				row++
				if row >= 30 {
					break
				}
			}
			if row == 0 {
				t.Logf("  (no rows)")
			}
			return result.Error()
		})
		require.NoError(t, err, q.name)
	}
}
