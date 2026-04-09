//go:build comparison && e2e

package e2e_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	schema "github.com/specterops/bloodhound/packages/go/graphschema"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

func TestKNexusDebug2(t *testing.T) {
	ctx := context.Background()
	knexusZip := filepath.Join(testdataDir(), "k-nexusglobal_sampledata.zip")
	skipIfMissing(t, knexusZip)

	kgliteDB := openGraph(t)
	neo4jDB := openNeo4j(t)
	clearNeo4j(ctx, t, neo4jDB)
	require.NoError(t, retryNeo4j(t, "AssertSchema", func() error {
		return neo4jDB.AssertSchema(ctx, schema.DefaultGraphSchema())
	}))
	ingestSchema := loadIngestSchema(t)

	t.Log("=== Ingesting into kglite ===")
	ingestZipTolerant(ctx, t, kgliteDB, knexusZip, ingestSchema)
	t.Log("=== Ingesting into Neo4j ===")
	ingestZipTolerant(ctx, t, neo4jDB, knexusZip, ingestSchema)

	t.Log("=== Running analysis on kglite ===")
	runAnalysis(ctx, t, kgliteDB)
	t.Log("=== Running analysis on Neo4j ===")
	runAnalysis(ctx, t, neo4jDB)

	queries := []struct {
		name   string
		cypher string
	}{
		// Count nodes per label set
		{"label combos", "MATCH (n) RETURN labels(n) AS lbls, count(n) AS c ORDER BY c DESC LIMIT 40"},
		// Nodes where Base is primary but also have GitHub/Okta/SCIM
		{"Base+GitHub", "MATCH (n:Base) WHERE labels(n) CONTAINS '\"GitHub\"' RETURN count(n) AS c"},
		{"Base+Okta", "MATCH (n:Base) WHERE labels(n) CONTAINS '\"Okta\"' RETURN count(n) AS c"},
		{"Base+SCIM", "MATCH (n:Base) WHERE labels(n) CONTAINS '\"SCIM\"' RETURN count(n) AS c"},
		{"Base+jamf", "MATCH (n:Base) WHERE labels(n) CONTAINS '\"jamf\"' RETURN count(n) AS c"},
		// GitHub-only nodes (no Base)
		{"GitHub no Base", "MATCH (n:GitHub) WHERE NOT labels(n) CONTAINS '\"Base\"' RETURN count(n) AS c"},
		// Count GitHub nodes
		{"GitHub total", "MATCH (n:GitHub) RETURN count(n) AS c"},
		{"Okta total", "MATCH (n:Okta) RETURN count(n) AS c"},
		{"SCIM total", "MATCH (n:SCIM) RETURN count(n) AS c"},
		{"jamf total", "MATCH (n:jamf) RETURN count(n) AS c"},
		// Check for nodes with same objectid across different primary labels
		{"dup objectids", `MATCH (a:Base), (b) WHERE a.objectid = b.objectid AND NOT labels(b) CONTAINS '"Base"' RETURN labels(a) AS a_lbls, labels(b) AS b_lbls, a.objectid AS oid LIMIT 10`},
	}

	for _, q := range queries {
		t.Logf("\n=== %s ===", q.name)
		for _, db := range []struct {
			name string
			db   graph.Database
		}{{"kglite", kgliteDB}, {"neo4j", neo4jDB}} {
			err := db.db.ReadTransaction(ctx, func(tx graph.Transaction) error {
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
					t.Logf("  %-7s row%02d: %s", db.name, row, strings.Join(parts, " | "))
					row++
					if row >= 40 {
						break
					}
				}
				if row == 0 {
					t.Logf("  %-7s: (no rows)", db.name)
				}
				return result.Error()
			})
			if err != nil {
				t.Logf("  %-7s: ERROR: %v", db.name, err)
			}
		}
	}
}

func init() {
	_ = time.Now
}
