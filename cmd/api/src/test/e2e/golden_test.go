//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/specterops/dawgs/graph"
)

// GoldenFile represents the contents of a golden results file.
type GoldenFile struct {
	Dataset     string         `json:"dataset"`
	GeneratedAt string         `json:"generated_at"`
	QueryCount  int            `json:"query_count"`
	Results     []GoldenResult `json:"results"`
}

// GoldenResult is a single query result in a golden file.
type GoldenResult struct {
	Name             string `json:"name"`
	Cypher           string `json:"cypher"`
	Result           string `json:"result"`
	NonDeterministic bool   `json:"non_deterministic,omitempty"`
}

// goldenFilePath returns the absolute path to a golden file in testdata/golden/.
func goldenFilePath(filename string) string {
	return filepath.Join(testdataDir(), "golden", filename)
}

// loadGoldenFile reads and parses a golden JSON file. Skips the test if the file
// does not exist (golden files must be generated first with make golden-generate).
func loadGoldenFile(t *testing.T, filename string) *GoldenFile {
	t.Helper()
	path := goldenFilePath(filename)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Skipf("golden file not found (generate with: make golden-generate): %s", path)
	}
	if err != nil {
		t.Fatalf("failed to read golden file %s: %v", path, err)
	}
	var gf GoldenFile
	if err := json.Unmarshal(data, &gf); err != nil {
		t.Fatalf("failed to parse golden file %s: %v", path, err)
	}
	return &gf
}

// compareKgliteToGolden runs each query from the golden file against the kglite
// database, normalizes the results, and compares them. Non-deterministic queries
// are logged but skipped. Fails the test if any deterministic query mismatches.
func compareKgliteToGolden(ctx context.Context, t *testing.T, db graph.Database, golden *GoldenFile) {
	t.Helper()

	var matches, mismatches, errors, skippedNonDet int
	var totalDur time.Duration

	t.Logf("")
	t.Logf("%-40s | %-20s | %-20s | %10s | %s",
		"Query", "Golden", "kglite", "kglite_ms", "Status")
	t.Logf("%s", strings.Repeat("-", 110))

	for _, gr := range golden.Results {
		if gr.NonDeterministic {
			skippedNonDet++
			t.Logf("%-40s | %-20s | %-20s | %10s | %s",
				truncateStr(gr.Name, 40), truncateStr(gr.Result, 20), "-", "-", "SKIP_NONDET")
			continue
		}

		start := time.Now()
		kResult, kErr := runQueryValuesKglite(ctx, t, db, gr.Cypher)
		dur := time.Since(start)
		totalDur += dur

		if kErr != nil {
			errors++
			t.Logf("%-40s | %-20s | %-20s | %10s | %s",
				truncateStr(gr.Name, 40),
				truncateStr(gr.Result, 20),
				"ERR",
				dur.Round(time.Microsecond),
				"ERROR")
			t.Logf("  ERROR: %v", kErr)
			continue
		}

		kNorm := normalizeResult(kResult)
		if !hasOrderBy(gr.Cypher) {
			kNorm = sortLines(kNorm)
		}

		if kNorm == gr.Result {
			matches++
			t.Logf("%-40s | %-20s | %-20s | %10s | %s",
				truncateStr(gr.Name, 40),
				truncateStr(gr.Result, 20),
				truncateStr(kResult, 20),
				dur.Round(time.Microsecond),
				"MATCH")
		} else {
			mismatches++
			t.Logf("%-40s | %-20s | %-20s | %10s | %s",
				truncateStr(gr.Name, 40),
				truncateStr(gr.Result, 20),
				truncateStr(kResult, 20),
				dur.Round(time.Microsecond),
				"MISMATCH")
			t.Logf("  DETAIL: golden=%s", truncateStr(gr.Result, 200))
			t.Logf("  DETAIL: kglite=%s", truncateStr(kNorm, 200))
		}
	}

	t.Logf("%s", strings.Repeat("-", 110))
	t.Logf("%-40s | %-20s | %-20s | %10s | %d match, %d mismatch, %d error, %d skipped(nondet)",
		fmt.Sprintf("TOTAL (%d queries)", len(golden.Results)), "", "",
		totalDur.Round(time.Millisecond),
		matches, mismatches, errors, skippedNonDet)
	t.Logf("")

	if mismatches > 0 || errors > 0 {
		t.Errorf("golden comparison failed: %d mismatches, %d errors (out of %d queries)",
			mismatches, errors, len(golden.Results))
	}
}

// TestGoldenAD compares the shared AD kglite graph against golden Neo4j results.
func TestGoldenAD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := sharedADGraph(t)
	golden := loadGoldenFile(t, "ad_golden.json")
	t.Logf("=== Golden comparison: AD (%d queries, generated %s) ===", golden.QueryCount, golden.GeneratedAt)
	compareKgliteToGolden(ctx, t, db, golden)
}

// TestGoldenAzure compares the shared Azure kglite graph against golden Neo4j results.
func TestGoldenAzure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := sharedAzureGraph(t)
	golden := loadGoldenFile(t, "azure_golden.json")
	t.Logf("=== Golden comparison: Azure (%d queries, generated %s) ===", golden.QueryCount, golden.GeneratedAt)
	compareKgliteToGolden(ctx, t, db, golden)
}

// TestGoldenKNexus compares the shared k-nexus kglite graph against golden Neo4j results.
func TestGoldenKNexus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := sharedKNexusGraph(t)
	golden := loadGoldenFile(t, "knexus_golden.json")
	t.Logf("=== Golden comparison: KNexus (%d queries, generated %s) ===", golden.QueryCount, golden.GeneratedAt)
	compareKgliteToGolden(ctx, t, db, golden)
}

// TestGoldenKNexusOpenGraph compares bundled OpenGraph query results against golden Neo4j results.
func TestGoldenKNexusOpenGraph(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := sharedKNexusGraph(t)
	golden := loadGoldenFile(t, "knexus_opengraph_golden.json")
	t.Logf("=== Golden comparison: KNexus OpenGraph (%d queries, generated %s) ===", golden.QueryCount, golden.GeneratedAt)
	compareKgliteToGolden(ctx, t, db, golden)
}
