//go:build comparison && e2e

package e2e_test

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/specterops/dawgs"
	_ "github.com/specterops/dawgs/drivers/neo4j"
	"github.com/specterops/dawgs/graph"
	schema "github.com/specterops/bloodhound/packages/go/graphschema"
	"github.com/specterops/dawgs/util/size"
	"github.com/stretchr/testify/require"
)

// openNeo4j opens a connection to the test Neo4j instance.
// Skips the test if Neo4j is not available (docker-compose.testing.yml not running).
func openNeo4j(t *testing.T) graph.Database {
	t.Helper()
	ctx := context.Background()
	db, err := dawgs.Open(ctx, "neo4j", dawgs.Config{
		GraphQueryMemoryLimit: size.Gibibyte,
		ConnectionString:      "neo4j://neo4j:neo4j@localhost:37687",
	})
	if err != nil {
		t.Skipf("Neo4j not available (start with: docker compose -f docker-compose.testing.yml up -d): %v", err)
	}
	// Verify connectivity
	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("RETURN 1 AS n", nil)
		defer result.Close()
		return result.Error()
	})
	if err != nil {
		db.Close(ctx)
		t.Skipf("Neo4j not responding: %v", err)
	}
	t.Cleanup(func() { db.Close(context.Background()) })
	return db
}

// clearNeo4j removes all nodes and relationships from Neo4j.
func clearNeo4j(ctx context.Context, t *testing.T, db graph.Database) {
	t.Helper()
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n) DETACH DELETE n", nil)
		defer result.Close()
		return result.Error()
	})
	require.NoError(t, err, "failed to clear Neo4j")
}

type comparisonResult struct {
	QueryName    string
	Cypher       string
	KgliteResult string
	KgliteDur    time.Duration
	KgliteErr    error
	Neo4jResult  string
	Neo4jDur     time.Duration
	Neo4jErr     error
	Match        bool
}

// normalizeResult normalizes numeric values in result strings for comparison.
// Converts "1519.0" to "1519", handles int/float representation differences.
var floatIntPattern = regexp.MustCompile(`(\d+)\.0\b`)

func normalizeResult(s string) string {
	return floatIntPattern.ReplaceAllString(s, "$1")
}

func compareQueries(ctx context.Context, t *testing.T,
	kgliteDB, neo4jDB graph.Database, queries []presetQuery) []comparisonResult {
	t.Helper()
	results := make([]comparisonResult, 0, len(queries))
	for _, q := range queries {
		kResult, kDur, kErr := runQuery(ctx, kgliteDB, q.Cypher)
		nResult, nDur, nErr := runQuery(ctx, neo4jDB, q.Cypher)

		kNorm := normalizeResult(kResult)
		nNorm := normalizeResult(nResult)

		results = append(results, comparisonResult{
			QueryName:    q.Name,
			Cypher:       q.Cypher,
			KgliteResult: kResult,
			KgliteDur:    kDur,
			KgliteErr:    kErr,
			Neo4jResult:  nResult,
			Neo4jDur:     nDur,
			Neo4jErr:     nErr,
			Match:        kErr == nil && nErr == nil && kNorm == nNorm,
		})
	}
	return results
}

func truncateStr(s string, max int) string {
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

func reportComparison(t *testing.T, results []comparisonResult) {
	t.Helper()
	var matches, mismatches, errors int

	t.Logf("")
	t.Logf("%-40s | %-25s | %-25s | %s", "Query", "kglite", "Neo4j", "Status")
	t.Logf("%s", strings.Repeat("-", 110))

	for _, r := range results {
		kStr := r.KgliteResult
		nStr := r.Neo4jResult
		if r.KgliteErr != nil {
			kStr = "ERR: " + r.KgliteErr.Error()
		}
		if r.Neo4jErr != nil {
			nStr = "ERR: " + r.Neo4jErr.Error()
		}

		status := "MATCH"
		if r.KgliteErr != nil || r.Neo4jErr != nil {
			status = "ERROR"
			errors++
		} else if !r.Match {
			status = "MISMATCH"
			mismatches++
		} else {
			matches++
		}

		t.Logf("%-40s | %-25s | %-25s | %s",
			truncateStr(r.QueryName, 40),
			truncateStr(kStr, 25),
			truncateStr(nStr, 25),
			status)
	}

	t.Logf("%s", strings.Repeat("-", 110))
	t.Logf("Total: %d queries | %d MATCH | %d MISMATCH | %d ERROR",
		len(results), matches, mismatches, errors)
	t.Logf("")
}

// TestCompareAD loads AD sample data into both kglite and Neo4j, runs analysis,
// and compares query results side-by-side.
func TestCompareAD(t *testing.T) {
	ctx := context.Background()
	adZip := filepath.Join(testdataDir(), "ad_sampledata.zip")
	skipIfMissing(t, adZip)

	kgliteDB := openGraph(t)
	neo4jDB := openNeo4j(t)

	// Prepare Neo4j
	clearNeo4j(ctx, t, neo4jDB)
	require.NoError(t, neo4jDB.AssertSchema(ctx, schema.DefaultGraphSchema()))

	ingestSchema := loadIngestSchema(t)

	// Ingest into both
	t.Log("=== Ingesting AD data into kglite ===")
	kDur := ingestZip(ctx, t, kgliteDB, adZip, ingestSchema)
	t.Logf("  kglite ingest: %s", kDur.Round(time.Millisecond))

	t.Log("=== Ingesting AD data into Neo4j ===")
	nDur := ingestZip(ctx, t, neo4jDB, adZip, ingestSchema)
	t.Logf("  Neo4j ingest: %s", nDur.Round(time.Millisecond))

	// Analysis on both
	t.Log("=== Running analysis on kglite ===")
	runAnalysis(ctx, t, kgliteDB)
	t.Log("=== Running analysis on Neo4j ===")
	runAnalysis(ctx, t, neo4jDB)

	// Compare preset queries
	t.Log("=== Comparing AD preset queries ===")
	results := compareQueries(ctx, t, kgliteDB, neo4jDB, adPresetQueries)
	reportComparison(t, results)

	// Compare attack path edges
	attackQueries := make([]presetQuery, 0, len(adAttackPathEdges))
	for _, e := range adAttackPathEdges {
		attackQueries = append(attackQueries, presetQuery{
			Name:   e.Name,
			Cypher: fmt.Sprintf("MATCH ()-[r:%s]->() RETURN count(r) AS c", e.EdgeType),
		})
	}
	t.Log("=== Comparing AD attack path edges ===")
	attackResults := compareQueries(ctx, t, kgliteDB, neo4jDB, attackQueries)
	reportComparison(t, attackResults)
}

// TestCompareAzure loads Azure sample data into both backends and compares results.
func TestCompareAzure(t *testing.T) {
	ctx := context.Background()
	azureZip := filepath.Join(testdataDir(), "entra_sampledata.zip")
	skipIfMissing(t, azureZip)

	kgliteDB := openGraph(t)
	neo4jDB := openNeo4j(t)

	clearNeo4j(ctx, t, neo4jDB)
	require.NoError(t, neo4jDB.AssertSchema(ctx, schema.DefaultGraphSchema()))

	ingestSchema := loadIngestSchema(t)

	t.Log("=== Ingesting Azure data into kglite ===")
	kDur := ingestZip(ctx, t, kgliteDB, azureZip, ingestSchema)
	t.Logf("  kglite ingest: %s", kDur.Round(time.Millisecond))

	t.Log("=== Ingesting Azure data into Neo4j ===")
	nDur := ingestZip(ctx, t, neo4jDB, azureZip, ingestSchema)
	t.Logf("  Neo4j ingest: %s", nDur.Round(time.Millisecond))

	t.Log("=== Running analysis on kglite ===")
	runAnalysis(ctx, t, kgliteDB)
	t.Log("=== Running analysis on Neo4j ===")
	runAnalysis(ctx, t, neo4jDB)

	t.Log("=== Comparing Azure preset queries ===")
	results := compareQueries(ctx, t, kgliteDB, neo4jDB, azurePresetQueries)
	reportComparison(t, results)

	attackQueries := make([]presetQuery, 0, len(azureAttackPathEdges))
	for _, e := range azureAttackPathEdges {
		attackQueries = append(attackQueries, presetQuery{
			Name:   e.Name,
			Cypher: fmt.Sprintf("MATCH ()-[r:%s]->() RETURN count(r) AS c", e.EdgeType),
		})
	}
	t.Log("=== Comparing Azure attack path edges ===")
	attackResults := compareQueries(ctx, t, kgliteDB, neo4jDB, attackQueries)
	reportComparison(t, attackResults)
}
