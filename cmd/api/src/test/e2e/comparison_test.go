//go:build comparison && e2e

package e2e_test

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype"
	"github.com/specterops/dawgs"
	_ "github.com/specterops/dawgs/drivers/neo4j"
	"github.com/specterops/dawgs/graph"
	schema "github.com/specterops/bloodhound/packages/go/graphschema"
	"github.com/specterops/dawgs/util/size"
	"github.com/stretchr/testify/require"
)

// isNeo4jTransientError checks whether an error is a transient Neo4j error
// that is worth retrying (database unavailable, deadlock, connection issues).
func isNeo4jTransientError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, substr := range []string{
		"TransientError",
		"DatabaseUnavailable",
		"DeadlockDetected",
		"connection refused",
		"connection reset",
	} {
		if strings.Contains(msg, substr) {
			return true
		}
	}
	return false
}

// retryNeo4j retries fn on Neo4j transient errors with exponential backoff.
// The maximum total wait time is approximately 30 seconds.
// Non-transient errors are returned immediately without retrying.
func retryNeo4j(t *testing.T, name string, fn func() error) error {
	t.Helper()
	backoffs := []time.Duration{
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		15 * time.Second,
	}
	var err error
	for attempt := 0; ; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !isNeo4jTransientError(err) {
			return err
		}
		if attempt >= len(backoffs) {
			t.Logf("retryNeo4j(%s): exhausted %d retries, last error: %v", name, len(backoffs), err)
			return err
		}
		t.Logf("retryNeo4j(%s): attempt %d failed with transient error, retrying in %s: %v",
			name, attempt+1, backoffs[attempt], err)
		time.Sleep(backoffs[attempt])
	}
}

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
	// Verify connectivity with retry (container may still be starting)
	err = retryNeo4j(t, "openNeo4j", func() error {
		return db.ReadTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw("RETURN 1 AS n", nil)
			defer result.Close()
			return result.Error()
		})
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
	err := retryNeo4j(t, "clearNeo4j", func() error {
		return db.WriteTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw("MATCH (n) DETACH DELETE n", nil)
			defer result.Close()
			return result.Error()
		})
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
	// NonDeterministic is set when the query has LIMIT but no ORDER BY: both backends
	// may return different but equally-valid subsets of a larger result set, so a
	// content mismatch is expected and should not be counted as a failure.
	NonDeterministic bool
}

// serializeValue converts a query result value to a stable string representation.
// Both kglite and Neo4j are handled: kglite returns *graph.Node / *graph.Relationship /
// *graph.Path, while the Neo4j driver returns dbtype.Node / dbtype.Relationship /
// dbtype.Path (value types). Both variants are serialized using stable domain
// identifiers rather than Go pointer addresses, which are non-deterministic and
// uncomparable across two different database backends.
func serializeValue(v any) string {
	switch typed := v.(type) {
	// --- kglite graph types ---
	case *graph.Node:
		if typed == nil {
			return "<nil node>"
		}
		// Prefer objectid (stable domain identifier), fall back to name, then graph ID.
		if oid, err := typed.Properties.Get("objectid").String(); err == nil && oid != "" {
			return fmt.Sprintf("node(objectid=%s)", oid)
		}
		if name, err := typed.Properties.Get("name").String(); err == nil && name != "" {
			return fmt.Sprintf("node(name=%s)", name)
		}
		return fmt.Sprintf("node(id=%d)", typed.ID)

	case *graph.Relationship:
		if typed == nil {
			return "<nil rel>"
		}
		return fmt.Sprintf("rel(%s:%d->%d)", typed.Kind.String(), typed.StartID, typed.EndID)

	case *graph.Path:
		if typed == nil {
			return "<nil path>"
		}
		// Serialize as: path(node_id-[EdgeKind]->node_id-[EdgeKind]->node_id)
		// Node IDs from kglite won't match Neo4j IDs, so use objectid/name from
		// properties when available, falling back to graph-internal ID.
		var sb strings.Builder
		sb.WriteString("path(")
		for i, node := range typed.Nodes {
			if i > 0 {
				if i-1 < len(typed.Edges) {
					sb.WriteString("-[")
					sb.WriteString(typed.Edges[i-1].Kind.String())
					sb.WriteString("]->")
				}
			}
			sb.WriteString(serializeValue(node))
		}
		sb.WriteString(")")
		return sb.String()

	// --- Neo4j driver dbtype graph types ---
	case dbtype.Node:
		// Prefer objectid, fall back to name, then element ID.
		if oid, ok := typed.Props["objectid"].(string); ok && oid != "" {
			return fmt.Sprintf("node(objectid=%s)", oid)
		}
		if name, ok := typed.Props["name"].(string); ok && name != "" {
			return fmt.Sprintf("node(name=%s)", name)
		}
		return fmt.Sprintf("node(elementid=%s)", typed.ElementId)

	case dbtype.Relationship:
		return fmt.Sprintf("rel(%s:%s->%s)", typed.Type, typed.StartElementId, typed.EndElementId)

	case dbtype.Path:
		var sb strings.Builder
		sb.WriteString("path(")
		for i, node := range typed.Nodes {
			if i > 0 {
				if i-1 < len(typed.Relationships) {
					sb.WriteString("-[")
					sb.WriteString(typed.Relationships[i-1].Type)
					sb.WriteString("]->")
				}
			}
			sb.WriteString(serializeValue(node))
		}
		sb.WriteString(")")
		return sb.String()

	default:
		return fmt.Sprintf("%v", v)
	}
}

// normalizeResult normalizes numeric values in result strings for comparison.
// Converts "1519.0" to "1519", handles int/float representation differences.
var floatIntPattern = regexp.MustCompile(`(\d+)\.0\b`)

func normalizeResult(s string) string {
	return floatIntPattern.ReplaceAllString(s, "$1")
}

// runQueryValues executes a Cypher query and returns the first row's values as a string.
// Unlike runQuery, this doesn't depend on Keys() being populated (Neo4j driver returns empty keys).
func runQueryValues(ctx context.Context, t *testing.T, db graph.Database, cypher string) (string, time.Duration, error) {
	start := time.Now()
	var out strings.Builder
	var queryErr error
	retryErr := retryNeo4j(t, "runQueryValues", func() error {
		out.Reset()
		queryErr = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw(cypher, nil)
			defer result.Close()
			if result.Error() != nil {
				return result.Error()
			}
			for result.Next() {
				vals := result.Values()
				for i, v := range vals {
					if i > 0 {
						out.WriteString(", ")
					}
					out.WriteString(serializeValue(v))
				}
				out.WriteString("\n")
			}
			return result.Error()
		})
		return queryErr
	})
	return strings.TrimRight(out.String(), "\n"), time.Since(start), retryErr
}

// sortLines sorts the lines of a multi-line result string for order-independent comparison.
func sortLines(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// hasOrderBy reports whether a Cypher query contains an ORDER BY clause (case-insensitive).
func hasOrderBy(cypher string) bool {
	return strings.Contains(strings.ToUpper(cypher), "ORDER BY")
}

// hasLimit reports whether a Cypher query contains a LIMIT clause (case-insensitive).
func hasLimit(cypher string) bool {
	return strings.Contains(strings.ToUpper(cypher), "LIMIT")
}

// isNonDeterministicQuery reports whether a query has LIMIT but no ORDER BY.
// Such queries ask each backend to return an arbitrary subset of a potentially
// larger result set. Both backends can return different but equally-valid rows,
// so content mismatches are expected and should not be counted as failures.
func isNonDeterministicQuery(cypher string) bool {
	return hasLimit(cypher) && !hasOrderBy(cypher)
}

func compareQueries(ctx context.Context, t *testing.T,
	kgliteDB, neo4jDB graph.Database, queries []presetQuery) []comparisonResult {
	t.Helper()
	results := make([]comparisonResult, 0, len(queries))
	for _, q := range queries {
		kResult, kDur, kErr := runQueryValues(ctx, t, kgliteDB, q.Cypher)
		nResult, nDur, nErr := runQueryValues(ctx, t, neo4jDB, q.Cypher)

		kNorm := normalizeResult(kResult)
		nNorm := normalizeResult(nResult)

		// Queries without ORDER BY have undefined row ordering; sort both sides
		// before comparing so that equivalent result sets compare equal regardless
		// of the order in which each backend returns them.
		if !hasOrderBy(q.Cypher) {
			kNorm = sortLines(kNorm)
			nNorm = sortLines(nNorm)
		}

		nonDeterministic := isNonDeterministicQuery(q.Cypher)

		results = append(results, comparisonResult{
			QueryName:        q.Name,
			Cypher:           q.Cypher,
			KgliteResult:     kResult,
			KgliteDur:        kDur,
			KgliteErr:        kErr,
			Neo4jResult:      nResult,
			Neo4jDur:         nDur,
			Neo4jErr:         nErr,
			Match:            kErr == nil && nErr == nil && kNorm == nNorm,
			NonDeterministic: nonDeterministic,
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

func reportComparison(t *testing.T, results []comparisonResult) (mismatches, errors int) {
	t.Helper()
	var matches, nonDeterministic int
	var totalKglite, totalNeo4j time.Duration

	t.Logf("")
	t.Logf("%-40s | %-20s | %-20s | %10s %10s %8s | %s",
		"Query", "kglite", "Neo4j", "kglite_ms", "neo4j_ms", "speedup", "Status")
	t.Logf("%s", strings.Repeat("-", 140))

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
			if r.NonDeterministic {
				// LIMIT without ORDER BY: both backends returned valid but different
				// subsets; treat as non-deterministic rather than a real mismatch.
				status = "NONDETERMINISTIC"
				nonDeterministic++
			} else {
				status = "MISMATCH"
				mismatches++
			}
		} else {
			matches++
		}

		totalKglite += r.KgliteDur
		totalNeo4j += r.Neo4jDur

		speedup := "n/a"
		if r.KgliteDur > 0 && r.Neo4jDur > 0 {
			ratio := float64(r.Neo4jDur) / float64(r.KgliteDur)
			speedup = fmt.Sprintf("%.1fx", ratio)
		}

		t.Logf("%-40s | %-20s | %-20s | %10s %10s %8s | %s",
			truncateStr(r.QueryName, 40),
			truncateStr(kStr, 20),
			truncateStr(nStr, 20),
			r.KgliteDur.Round(time.Microsecond),
			r.Neo4jDur.Round(time.Microsecond),
			speedup,
			status)
		if status == "ERROR" {
			t.Logf("  FULL ERROR: kglite=%v neo4j=%v", r.KgliteErr, r.Neo4jErr)
		}
		if status == "MISMATCH" || status == "ERROR" {
			t.Logf("  DETAIL: kglite=%s", truncateStr(kStr, 200))
			t.Logf("  DETAIL: neo4j =%s", truncateStr(nStr, 200))
		}
		if status == "NONDETERMINISTIC" {
			t.Logf("  DETAIL (non-det): kglite=%s", truncateStr(kStr, 200))
			t.Logf("  DETAIL (non-det): neo4j =%s", truncateStr(nStr, 200))
		}
	}

	t.Logf("%s", strings.Repeat("-", 140))
	totalSpeedup := "n/a"
	if totalKglite > 0 && totalNeo4j > 0 {
		totalSpeedup = fmt.Sprintf("%.1fx", float64(totalNeo4j)/float64(totalKglite))
	}
	t.Logf("%-40s | %-20s | %-20s | %10s %10s %8s | %d match, %d mismatch, %d error, %d nondeterministic",
		fmt.Sprintf("TOTAL (%d queries)", len(results)), "", "",
		totalKglite.Round(time.Millisecond),
		totalNeo4j.Round(time.Millisecond),
		totalSpeedup,
		matches, mismatches, errors, nonDeterministic)
	t.Logf("")
	return mismatches, errors
}

// requireComparisonPass reports comparison results and fails the test if there
// are any real mismatches (ordering differences are already handled by sortLines
// in compareQueries; non-deterministic queries with LIMIT but no ORDER BY are
// excluded from failure).
func requireComparisonPass(t *testing.T, results []comparisonResult) {
	t.Helper()
	mismatches, errors := reportComparison(t, results)
	if mismatches > 0 || errors > 0 {
		t.Errorf("comparison failed: %d mismatches, %d errors (out of %d queries)", mismatches, errors, len(results))
	}
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
	require.NoError(t, retryNeo4j(t, "AssertSchema", func() error {
		return neo4jDB.AssertSchema(ctx, schema.DefaultGraphSchema())
	}))

	ingestSchema := loadIngestSchema(t)

	// Ingest into both
	t.Log("=== Ingesting AD data into kglite ===")
	kIngestDur := ingestZip(ctx, t, kgliteDB, adZip, ingestSchema)
	t.Logf("  kglite ingest: %s", kIngestDur.Round(time.Millisecond))

	t.Log("=== Ingesting AD data into Neo4j ===")
	nIngestDur := ingestZip(ctx, t, neo4jDB, adZip, ingestSchema)
	t.Logf("  Neo4j ingest: %s", nIngestDur.Round(time.Millisecond))

	// Analysis on both
	t.Log("=== Running analysis on kglite ===")
	kAnalysisDur := runAnalysis(ctx, t, kgliteDB)
	t.Log("=== Running analysis on Neo4j ===")
	nAnalysisDur := runAnalysis(ctx, t, neo4jDB)

	// Performance summary: ingest + analysis
	t.Log("")
	t.Log("=== Performance Summary: AD ===")
	t.Logf("%-25s %12s %12s %10s", "Phase", "kglite", "Neo4j", "Speedup")
	t.Logf("%s", strings.Repeat("-", 65))
	t.Logf("%-25s %12s %12s %10.1fx", "Ingest",
		kIngestDur.Round(time.Millisecond), nIngestDur.Round(time.Millisecond),
		float64(nIngestDur)/float64(kIngestDur))
	t.Logf("%-25s %12s %12s %10.1fx", "Analysis",
		kAnalysisDur.Round(time.Millisecond), nAnalysisDur.Round(time.Millisecond),
		float64(nAnalysisDur)/float64(kAnalysisDur))
	totalK := kIngestDur + kAnalysisDur
	totalN := nIngestDur + nAnalysisDur
	t.Logf("%-25s %12s %12s %10.1fx", "Total (ingest+analysis)",
		totalK.Round(time.Millisecond), totalN.Round(time.Millisecond),
		float64(totalN)/float64(totalK))
	t.Logf("%s", strings.Repeat("-", 65))
	t.Log("")

	// Compare preset queries
	t.Log("=== Comparing AD preset queries ===")
	results := compareQueries(ctx, t, kgliteDB, neo4jDB, adPresetQueries)
	requireComparisonPass(t, results)

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
	requireComparisonPass(t, attackResults)
}

// TestCompareAzure loads Azure sample data into both backends and compares results.
func TestCompareAzure(t *testing.T) {
	ctx := context.Background()
	azureZip := filepath.Join(testdataDir(), "entra_sampledata.zip")
	skipIfMissing(t, azureZip)

	kgliteDB := openGraph(t)
	neo4jDB := openNeo4j(t)

	clearNeo4j(ctx, t, neo4jDB)
	require.NoError(t, retryNeo4j(t, "AssertSchema", func() error {
		return neo4jDB.AssertSchema(ctx, schema.DefaultGraphSchema())
	}))

	ingestSchema := loadIngestSchema(t)

	t.Log("=== Ingesting Azure data into kglite ===")
	kIngestDur := ingestZip(ctx, t, kgliteDB, azureZip, ingestSchema)
	t.Logf("  kglite ingest: %s", kIngestDur.Round(time.Millisecond))

	t.Log("=== Ingesting Azure data into Neo4j ===")
	nIngestDur := ingestZip(ctx, t, neo4jDB, azureZip, ingestSchema)
	t.Logf("  Neo4j ingest: %s", nIngestDur.Round(time.Millisecond))

	t.Log("=== Running analysis on kglite ===")
	kAnalysisDur := runAnalysis(ctx, t, kgliteDB)
	t.Log("=== Running analysis on Neo4j ===")
	nAnalysisDur := runAnalysis(ctx, t, neo4jDB)

	// Performance summary
	t.Log("")
	t.Log("=== Performance Summary: Azure ===")
	t.Logf("%-25s %12s %12s %10s", "Phase", "kglite", "Neo4j", "Speedup")
	t.Logf("%s", strings.Repeat("-", 65))
	t.Logf("%-25s %12s %12s %10.1fx", "Ingest",
		kIngestDur.Round(time.Millisecond), nIngestDur.Round(time.Millisecond),
		float64(nIngestDur)/float64(kIngestDur))
	t.Logf("%-25s %12s %12s %10.1fx", "Analysis",
		kAnalysisDur.Round(time.Millisecond), nAnalysisDur.Round(time.Millisecond),
		float64(nAnalysisDur)/float64(kAnalysisDur))
	totalK := kIngestDur + kAnalysisDur
	totalN := nIngestDur + nAnalysisDur
	t.Logf("%-25s %12s %12s %10.1fx", "Total (ingest+analysis)",
		totalK.Round(time.Millisecond), totalN.Round(time.Millisecond),
		float64(totalN)/float64(totalK))
	t.Logf("%s", strings.Repeat("-", 65))
	t.Log("")

	t.Log("=== Comparing Azure preset queries ===")
	results := compareQueries(ctx, t, kgliteDB, neo4jDB, azurePresetQueries)
	requireComparisonPass(t, results)

	attackQueries := make([]presetQuery, 0, len(azureAttackPathEdges))
	for _, e := range azureAttackPathEdges {
		attackQueries = append(attackQueries, presetQuery{
			Name:   e.Name,
			Cypher: fmt.Sprintf("MATCH ()-[r:%s]->() RETURN count(r) AS c", e.EdgeType),
		})
	}
	t.Log("=== Comparing Azure attack path edges ===")
	attackResults := compareQueries(ctx, t, kgliteDB, neo4jDB, attackQueries)
	requireComparisonPass(t, attackResults)
}
