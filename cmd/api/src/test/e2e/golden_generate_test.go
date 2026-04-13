//go:build comparison && e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	schema "github.com/specterops/bloodhound/packages/go/graphschema"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

// writeGoldenFile writes a GoldenFile to disk as indented JSON.
func writeGoldenFile(t *testing.T, path string, gf *GoldenFile) {
	t.Helper()
	data, err := json.MarshalIndent(gf, "", "  ")
	require.NoError(t, err, "marshal golden file")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755), "create golden dir")
	require.NoError(t, os.WriteFile(path, data, 0o644), "write golden file %s", path)
	t.Logf("Wrote golden file: %s (%d queries, %d bytes)", path, gf.QueryCount, len(data))
}

// generateGolden runs each query against Neo4j, normalizes the results, and writes
// a golden file. The Neo4j database must already have data ingested and analysis run.
func generateGolden(ctx context.Context, t *testing.T, neo4jDB graph.Database, dataset string, filename string, queries []presetQuery) {
	t.Helper()

	results := make([]GoldenResult, 0, len(queries))
	for _, q := range queries {
		nResult, _, nErr := runQueryValues(ctx, t, neo4jDB, q.Cypher)
		require.NoError(t, nErr, "Neo4j query failed during golden generation: %s (%s)", q.Name, q.Cypher)

		nNorm := normalizeResult(nResult)
		if !hasOrderBy(q.Cypher) {
			nNorm = sortLines(nNorm)
		}

		gr := GoldenResult{
			Name:             q.Name,
			Cypher:           q.Cypher,
			Result:           nNorm,
			NonDeterministic: isNonDeterministicQuery(q.Cypher),
		}
		results = append(results, gr)
	}

	gf := &GoldenFile{
		Dataset:     dataset,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		QueryCount:  len(results),
		Results:     results,
	}

	writeGoldenFile(t, goldenFilePath(filename), gf)
}

// ingestAndAnalyzeNeo4j ingests a dataset zip into Neo4j and runs analysis.
// Returns after analysis is complete. Uses tolerant mode if tolerant is true.
func ingestAndAnalyzeNeo4j(ctx context.Context, t *testing.T, neo4jDB graph.Database, zipPath string, tolerant bool) {
	t.Helper()
	clearNeo4j(ctx, t, neo4jDB)
	require.NoError(t, retryNeo4j(t, "AssertSchema", func() error {
		return neo4jDB.AssertSchema(ctx, schema.DefaultGraphSchema())
	}))

	ingestSchema := loadIngestSchema(t)

	t.Logf("  Ingesting %s into Neo4j...", filepath.Base(zipPath))
	if tolerant {
		ingestZipTolerant(ctx, t, neo4jDB, zipPath, ingestSchema)
	} else {
		ingestZip(ctx, t, neo4jDB, zipPath, ingestSchema)
	}

	t.Log("  Running analysis on Neo4j...")
	runAnalysis(ctx, t, neo4jDB)
}

// buildAttackPathQueries converts edge assertions to preset queries.
func buildAttackPathQueries(edges []edgeAssertion) []presetQuery {
	queries := make([]presetQuery, 0, len(edges))
	for _, e := range edges {
		queries = append(queries, presetQuery{
			Name:   e.Name + " (edge count)",
			Cypher: fmt.Sprintf("MATCH ()-[r:%s]->() RETURN count(r) AS c", e.EdgeType),
		})
	}
	return queries
}

// TestGenerateGoldenAD generates the AD golden file from Neo4j.
func TestGenerateGoldenAD(t *testing.T) {
	ctx := context.Background()
	adZip := filepath.Join(testdataDir(), "ad_sampledata.zip")
	skipIfMissing(t, adZip)

	neo4jDB := openNeo4j(t)

	t.Log("=== Generating AD golden file ===")
	ingestAndAnalyzeNeo4j(ctx, t, neo4jDB, adZip, false)

	queries := make([]presetQuery, 0, len(adPresetQueries)+len(adAttackPathEdges))
	queries = append(queries, adPresetQueries...)
	queries = append(queries, buildAttackPathQueries(adAttackPathEdges)...)

	generateGolden(ctx, t, neo4jDB, "ad", "ad_golden.json", queries)
}

// TestGenerateGoldenAzure generates the Azure golden file from Neo4j.
func TestGenerateGoldenAzure(t *testing.T) {
	ctx := context.Background()
	azureZip := filepath.Join(testdataDir(), "entra_sampledata.zip")
	skipIfMissing(t, azureZip)

	neo4jDB := openNeo4j(t)

	t.Log("=== Generating Azure golden file ===")
	ingestAndAnalyzeNeo4j(ctx, t, neo4jDB, azureZip, false)

	queries := make([]presetQuery, 0, len(azurePresetQueries)+len(azureAttackPathEdges))
	queries = append(queries, azurePresetQueries...)
	queries = append(queries, buildAttackPathQueries(azureAttackPathEdges)...)

	generateGolden(ctx, t, neo4jDB, "azure", "azure_golden.json", queries)
}

// TestGenerateGoldenKNexus generates the k-nexus golden file from Neo4j.
func TestGenerateGoldenKNexus(t *testing.T) {
	ctx := context.Background()
	knexusZip := filepath.Join(testdataDir(), "k-nexusglobal_sampledata.zip")
	skipIfMissing(t, knexusZip)

	neo4jDB := openNeo4j(t)

	t.Log("=== Generating KNexus golden file ===")
	ingestAndAnalyzeNeo4j(ctx, t, neo4jDB, knexusZip, true)

	queries := make([]presetQuery, 0, len(knexusPresetQueries)+len(knexusAttackPathEdges))
	queries = append(queries, knexusPresetQueries...)
	for _, e := range knexusAttackPathEdges {
		queries = append(queries, presetQuery{
			Name:   e.Name + " (edge count)",
			Cypher: fmt.Sprintf("MATCH ()-[r:%s]->() RETURN count(r) AS c", e.EdgeType),
		})
	}

	generateGolden(ctx, t, neo4jDB, "knexus", "knexus_golden.json", queries)
}

// TestGenerateGoldenKNexusOpenGraph generates the k-nexus OpenGraph golden file from Neo4j.
// Uses the bundled queries from the zip, filtered for kglite compatibility.
func TestGenerateGoldenKNexusOpenGraph(t *testing.T) {
	ctx := context.Background()
	knexusZip := filepath.Join(testdataDir(), "k-nexusglobal_sampledata.zip")
	skipIfMissing(t, knexusZip)

	neo4jDB := openNeo4j(t)

	t.Log("=== Generating KNexus OpenGraph golden file ===")
	ingestAndAnalyzeNeo4j(ctx, t, neo4jDB, knexusZip, true)

	queryGroups := loadQueriesFromZip(t, knexusZip)
	t.Logf("Loaded %d query categories from zip", len(queryGroups))

	var allQueries []presetQuery
	var totalSkipped int
	for _, category := range []string{"hybrid", "githound", "oktahound", "jamfhound", "oktahound-privilege-zones"} {
		queries, ok := queryGroups[category]
		if !ok || len(queries) == 0 {
			continue
		}
		compatible, skipped := filterKgliteCompatible(queries)
		totalSkipped += len(skipped)
		// Prefix query names with category for uniqueness
		for i := range compatible {
			compatible[i].Name = category + "/" + compatible[i].Name
		}
		allQueries = append(allQueries, compatible...)
	}
	if totalSkipped > 0 {
		t.Logf("Skipped %d queries with unsupported Cypher patterns", totalSkipped)
	}

	generateGolden(ctx, t, neo4jDB, "knexus_opengraph", "knexus_opengraph_golden.json", allQueries)
}

