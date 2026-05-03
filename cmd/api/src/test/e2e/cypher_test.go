//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

var cypherNodeKind = graph.StringKind("CypherTest")
var cypherEdgeKind = graph.StringKind("CypherEdge")

// setupCypherTestGraph creates a small graph for Cypher feature testing:
// A -[:CypherEdge {weight: 1}]-> B -[:CypherEdge {weight: 2}]-> C
// with properties: name, objectid, enabled (bool), score (int)
func setupCypherTestGraph(ctx context.Context, t *testing.T, db graph.Database) (a, b, c graph.ID) {
	t.Helper()
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		nodeA, err := tx.CreateNode(graph.AsProperties(map[string]any{
			"name": "Alice", "objectid": "A-1", "enabled": true, "score": 100,
		}), cypherNodeKind)
		if err != nil {
			return err
		}
		nodeB, err := tx.CreateNode(graph.AsProperties(map[string]any{
			"name": "Bob", "objectid": "B-2", "enabled": false, "score": 200,
		}), cypherNodeKind)
		if err != nil {
			return err
		}
		nodeC, err := tx.CreateNode(graph.AsProperties(map[string]any{
			"name": "Charlie", "objectid": "C-3", "enabled": true, "score": 150,
		}), cypherNodeKind)
		if err != nil {
			return err
		}
		if _, err := tx.CreateRelationshipByIDs(nodeA.ID, nodeB.ID, cypherEdgeKind,
			graph.AsProperties(map[string]any{"weight": 1})); err != nil {
			return err
		}
		if _, err := tx.CreateRelationshipByIDs(nodeB.ID, nodeC.ID, cypherEdgeKind,
			graph.AsProperties(map[string]any{"weight": 2})); err != nil {
			return err
		}
		a, b, c = nodeA.ID, nodeB.ID, nodeC.ID
		return nil
	})
	require.NoError(t, err)
	return
}

func TestCypherMATCH(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	setupCypherTestGraph(ctx, t, db)

	t.Run("all nodes", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db, "MATCH (n:CypherTest) RETURN count(n) AS c")
		require.Equal(t, int64(3), count)
	})

	t.Run("all relationships", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db, "MATCH ()-[r:CypherEdge]->() RETURN count(r) AS c")
		require.Equal(t, int64(2), count)
	})

	t.Run("pattern match", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db, "MATCH (a:CypherTest)-[:CypherEdge]->(b:CypherTest) RETURN count(*) AS c")
		require.Equal(t, int64(2), count)
	})
}

func TestCypherWHERE(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	setupCypherTestGraph(ctx, t, db)

	t.Run("equals", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db, "MATCH (n:CypherTest) WHERE n.name = 'Alice' RETURN count(n) AS c")
		require.Equal(t, int64(1), count)
	})

	t.Run("not equals", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db, "MATCH (n:CypherTest) WHERE n.name <> 'Alice' RETURN count(n) AS c")
		require.Equal(t, int64(2), count)
	})

	t.Run("greater than", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db, "MATCH (n:CypherTest) WHERE n.score > 100 RETURN count(n) AS c")
		require.Equal(t, int64(2), count)
	})

	t.Run("boolean", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db, "MATCH (n:CypherTest) WHERE n.enabled = true RETURN count(n) AS c")
		require.Equal(t, int64(2), count)
	})

	t.Run("AND", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db, "MATCH (n:CypherTest) WHERE n.enabled = true AND n.score > 100 RETURN count(n) AS c")
		require.Equal(t, int64(1), count)
	})

	t.Run("OR", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db, "MATCH (n:CypherTest) WHERE n.name = 'Alice' OR n.name = 'Bob' RETURN count(n) AS c")
		require.Equal(t, int64(2), count)
	})

	t.Run("STARTS WITH", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db, "MATCH (n:CypherTest) WHERE n.name STARTS WITH 'Al' RETURN count(n) AS c")
		require.Equal(t, int64(1), count)
	})

	t.Run("ENDS WITH", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db, "MATCH (n:CypherTest) WHERE n.objectid ENDS WITH '-3' RETURN count(n) AS c")
		require.Equal(t, int64(1), count)
	})

	t.Run("CONTAINS", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db, "MATCH (n:CypherTest) WHERE n.name CONTAINS 'ob' RETURN count(n) AS c")
		require.Equal(t, int64(1), count)
	})

	t.Run("IN list", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db, "MATCH (n:CypherTest) WHERE n.name IN ['Alice', 'Charlie'] RETURN count(n) AS c")
		require.Equal(t, int64(2), count)
	})
}

func TestCypherWITH(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	setupCypherTestGraph(ctx, t, db)

	count := runQueryInt64(ctx, t, db,
		"MATCH (n:CypherTest) WITH n WHERE n.enabled = true RETURN count(n) AS c")
	require.Equal(t, int64(2), count)
}

func TestCypherORDER_BY(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	setupCypherTestGraph(ctx, t, db)

	var names []string
	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:CypherTest) RETURN n.name AS name ORDER BY n.name", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		for result.Next() {
			vals := result.Values()
			if s, ok := vals[0].(string); ok {
				names = append(names, s)
			}
		}
		return result.Error()
	})
	require.NoError(t, err)
	require.Equal(t, []string{"Alice", "Bob", "Charlie"}, names)
}

func TestCypherDISTINCT(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	setupCypherTestGraph(ctx, t, db)

	// All 3 nodes have CypherTest kind — DISTINCT should return 1 kind
	count := runQueryInt64(ctx, t, db,
		"MATCH (n:CypherTest) RETURN count(DISTINCT n.enabled) AS c")
	require.Equal(t, int64(2), count) // true, false
}

func TestCypherFunctions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	a, _, _ := setupCypherTestGraph(ctx, t, db)

	t.Run("count", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db, "MATCH (n:CypherTest) RETURN count(n) AS c")
		require.Equal(t, int64(3), count)
	})

	t.Run("id", func(t *testing.T) {
		t.Parallel()
		got := runQueryInt64(ctx, t, db,
			fmt.Sprintf("MATCH (n) WHERE id(n) = %d RETURN id(n) AS i", a))
		require.Equal(t, int64(a), got)
	})

	t.Run("type", func(t *testing.T) {
		t.Parallel()
		var relType string
		err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw("MATCH ()-[r:CypherEdge]->() RETURN type(r) AS t LIMIT 1", nil)
			defer result.Close()
			require.NoError(t, result.Error())
			require.True(t, result.Next())
			relType = result.Values()[0].(string)
			return result.Error()
		})
		require.NoError(t, err)
		require.Equal(t, "CypherEdge", relType)
	})

	t.Run("coalesce", func(t *testing.T) {
		t.Parallel()
		var name string
		err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw(
				fmt.Sprintf("MATCH (n) WHERE id(n) = %d RETURN coalesce(n.name, 'unknown') AS name", a), nil)
			defer result.Close()
			require.NoError(t, result.Error())
			require.True(t, result.Next())
			name = result.Values()[0].(string)
			return result.Error()
		})
		require.NoError(t, err)
		require.Equal(t, "Alice", name)
	})

	t.Run("toLower", func(t *testing.T) {
		t.Parallel()
		var lower string
		err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw(
				fmt.Sprintf("MATCH (n) WHERE id(n) = %d RETURN toLower(n.name) AS l", a), nil)
			defer result.Close()
			require.NoError(t, result.Error())
			require.True(t, result.Next())
			lower = result.Values()[0].(string)
			return result.Error()
		})
		require.NoError(t, err)
		require.Equal(t, "alice", lower)
	})

	t.Run("toUpper", func(t *testing.T) {
		t.Parallel()
		var upper string
		err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw(
				fmt.Sprintf("MATCH (n) WHERE id(n) = %d RETURN toUpper(n.name) AS u", a), nil)
			defer result.Close()
			require.NoError(t, result.Error())
			require.True(t, result.Next())
			upper = result.Values()[0].(string)
			return result.Error()
		})
		require.NoError(t, err)
		require.Equal(t, "ALICE", upper)
	})

	t.Run("labels", func(t *testing.T) {
		t.Parallel()
		var label string
		err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
			result := tx.Raw(
				fmt.Sprintf("MATCH (n) WHERE id(n) = %d RETURN labels(n) AS l", a), nil)
			defer result.Close()
			require.NoError(t, result.Error())
			require.True(t, result.Next())
			label = fmt.Sprintf("%v", result.Values()[0])
			return result.Error()
		})
		require.NoError(t, err)
		// kglite returns labels as a JSON array string or a plain string
		require.Contains(t, label, "CypherTest")
	})
}

func TestCypherVariableLengthPaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	setupCypherTestGraph(ctx, t, db) // A->B->C

	t.Run("unbounded", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db,
			"MATCH (a:CypherTest)-[:CypherEdge*1..]->(x) WHERE a.name = 'Alice' RETURN count(x) AS c")
		require.Equal(t, int64(2), count) // B and C
	})

	t.Run("exact length", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db,
			"MATCH (a:CypherTest)-[:CypherEdge*2]->(x) WHERE a.name = 'Alice' RETURN count(x) AS c")
		require.Equal(t, int64(1), count) // only C at distance 2
	})

	t.Run("bounded", func(t *testing.T) {
		t.Parallel()
		count := runQueryInt64(ctx, t, db,
			"MATCH (a:CypherTest)-[:CypherEdge*1..1]->(x) WHERE a.name = 'Alice' RETURN count(x) AS c")
		require.Equal(t, int64(1), count) // only B at distance 1
	})
}

func TestCypherPipeSeparatedTypes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)

	ids := createNodes(ctx, t, db, cypherNodeKind, 3)
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		if _, err := tx.CreateRelationshipByIDs(ids[0], ids[1], graph.StringKind("TypeA"), graph.NewProperties()); err != nil {
			return err
		}
		if _, err := tx.CreateRelationshipByIDs(ids[0], ids[2], graph.StringKind("TypeB"), graph.NewProperties()); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db,
		fmt.Sprintf("MATCH (a)-[:TypeA|TypeB]->(x) WHERE id(a) = %d RETURN count(x) AS c", ids[0]))
	require.Equal(t, int64(2), count)
}

func TestCypherLabelCheckInWhere(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	setupCypherTestGraph(ctx, t, db)

	count := runQueryInt64(ctx, t, db,
		"MATCH (n) WHERE n:CypherTest RETURN count(n) AS c")
	require.Equal(t, int64(3), count)
}

func TestCypherINList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	setupCypherTestGraph(ctx, t, db)

	// Test IN with inline list (not parameterized — kglite handles inline lists)
	count := runQueryInt64(ctx, t, db,
		"MATCH (n:CypherTest) WHERE n.name IN ['Alice', 'Charlie'] RETURN count(n) AS c")
	require.Equal(t, int64(2), count)
}

func TestCypherCASE(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	setupCypherTestGraph(ctx, t, db)

	var label string
	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw(
			"MATCH (n:CypherTest) WHERE n.name = 'Alice' RETURN CASE WHEN n.enabled = true THEN 'active' ELSE 'inactive' END AS status", nil)
		defer result.Close()
		require.NoError(t, result.Error())
		require.True(t, result.Next())
		label = result.Values()[0].(string)
		return result.Error()
	})
	require.NoError(t, err)
	require.Equal(t, "active", label)
}
