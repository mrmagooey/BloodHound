//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype"
	"github.com/specterops/dawgs/graph"
)

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

// truncateStr truncates a string to max characters, adding "..." if truncated.
func truncateStr(s string, max int) string {
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

// runQueryValuesKglite executes a Cypher query against kglite and returns the result
// rows as a serialized string (one line per row, values comma-separated).
// Unlike runQueryValues, this has no retry logic (kglite has no transient errors).
func runQueryValuesKglite(ctx context.Context, t *testing.T, db graph.Database, cypher string) (string, error) {
	t.Helper()
	var out strings.Builder
	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
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
	return strings.TrimRight(out.String(), "\n"), err
}
