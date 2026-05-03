//go:build e2e

package e2e_test

import (
	"context"
	"testing"

	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
	"github.com/stretchr/testify/require"
)

var qcNodeKind = graph.StringKind("QCNode")
var qcEdgeKind = graph.StringKind("QCEdge")

// seedQCNodes creates n nodes with sequential names and objectids, returns IDs.
func seedQCNodes(ctx context.Context, t *testing.T, db graph.Database, n int) []graph.ID {
	t.Helper()
	ids := make([]graph.ID, n)
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		for i := 0; i < n; i++ {
			node, err := tx.CreateNode(
				graph.AsProperties(map[string]any{
					"name":     "qcnode_" + string(rune('A'+i)),
					"objectid": "QC-" + string(rune('A'+i)),
					"idx":      int64(i),
				}),
				qcNodeKind,
			)
			if err != nil {
				return err
			}
			ids[i] = node.ID
		}
		return nil
	})
	require.NoError(t, err)
	return ids
}

// seedQCChain creates n nodes connected linearly by qcEdgeKind, returns node IDs.
func seedQCChain(ctx context.Context, t *testing.T, db graph.Database, n int) []graph.ID {
	t.Helper()
	ids := seedQCNodes(ctx, t, db, n)
	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		for i := 0; i < len(ids)-1; i++ {
			_, err := tx.CreateRelationshipByIDs(ids[i], ids[i+1], qcEdgeKind,
				graph.AsProperties(map[string]any{"weight": int64(i + 1)}))
			if err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)
	return ids
}

// ─── Node Query Tests ────────────────────────────────────────────────────────

func TestQueryNodeFilterFetch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCNodes(ctx, t, db, 3)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		var fetched []*graph.Node
		return tx.Nodes().Filter(query.KindIn(query.Node(), qcNodeKind)).Fetch(func(cursor graph.Cursor[*graph.Node]) error {
			for node := range cursor.Chan() {
				fetched = append(fetched, node)
			}
			require.Len(t, fetched, 3)
			return cursor.Error()
		})
	})
	require.NoError(t, err)
}

func TestQueryNodeFilterfFetch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCNodes(ctx, t, db, 2)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		var fetched []*graph.Node
		return tx.Nodes().Filterf(func() graph.Criteria {
			return query.KindIn(query.Node(), qcNodeKind)
		}).Fetch(func(cursor graph.Cursor[*graph.Node]) error {
			for node := range cursor.Chan() {
				fetched = append(fetched, node)
			}
			require.Len(t, fetched, 2)
			return cursor.Error()
		})
	})
	require.NoError(t, err)
}

func TestQueryNodeFirst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCNodes(ctx, t, db, 3)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		node, err := tx.Nodes().Filter(query.KindIn(query.Node(), qcNodeKind)).First()
		require.NoError(t, err)
		require.NotNil(t, node)
		return nil
	})
	require.NoError(t, err)
}

func TestQueryNodeFirstNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		_, err := tx.Nodes().Filter(query.KindIn(query.Node(), qcNodeKind)).First()
		require.ErrorIs(t, err, graph.ErrNoResultsFound)
		return nil
	})
	require.NoError(t, err)
}

func TestQueryNodeFetchIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCNodes(ctx, t, db, 4)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		var ids []graph.ID
		return tx.Nodes().Filter(query.KindIn(query.Node(), qcNodeKind)).FetchIDs(func(cursor graph.Cursor[graph.ID]) error {
			for id := range cursor.Chan() {
				ids = append(ids, id)
			}
			require.Len(t, ids, 4)
			return cursor.Error()
		})
	})
	require.NoError(t, err)
}

func TestQueryNodeFetchKinds(t *testing.T) {
	t.Parallel()
	t.Skip("kglite: FetchKinds not yet implemented via query builder")
	ctx := context.Background()
	db := openGraph(t)
	seedQCNodes(ctx, t, db, 2)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		var results []graph.KindsResult
		return tx.Nodes().Filter(query.KindIn(query.Node(), qcNodeKind)).FetchKinds(func(cursor graph.Cursor[graph.KindsResult]) error {
			for kr := range cursor.Chan() {
				results = append(results, kr)
			}
			require.Len(t, results, 2)
			for _, kr := range results {
				require.True(t, kr.Kinds.ContainsOneOf(qcNodeKind), "expected node to have QCNode kind")
			}
			return cursor.Error()
		})
	})
	require.NoError(t, err)
}

func TestQueryNodeOrderByOffsetLimit(t *testing.T) {
	t.Parallel()
	t.Skip("kglite: OrderBy+Offset+Limit not yet supported via query builder")
	ctx := context.Background()
	db := openGraph(t)
	seedQCNodes(ctx, t, db, 5)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		var fetched []*graph.Node
		return tx.Nodes().
			Filter(query.KindIn(query.Node(), qcNodeKind)).
			OrderBy(query.Order(query.NodeProperty("idx"), query.Ascending())).
			Offset(1).
			Limit(2).
			Fetch(func(cursor graph.Cursor[*graph.Node]) error {
				for node := range cursor.Chan() {
					fetched = append(fetched, node)
				}
				require.Len(t, fetched, 2, "expected 2 nodes after offset+limit")
				return cursor.Error()
			})
	})
	require.NoError(t, err)
}

func TestQueryNodeDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCNodes(ctx, t, db, 3)

	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		return tx.Nodes().Filter(query.KindIn(query.Node(), qcNodeKind)).Delete()
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db, "MATCH (n:QCNode) RETURN count(n) AS c")
	require.Equal(t, int64(0), count)
}

func TestQueryNodeUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCNodes(ctx, t, db, 2)

	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		props := graph.NewProperties()
		props.Set("updated", true)
		return tx.Nodes().Filter(query.KindIn(query.Node(), qcNodeKind)).Update(props)
	})
	require.NoError(t, err)

	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:QCNode) WHERE n.updated = true RETURN count(n) AS c", nil)
		defer result.Close()
		require.NoError(t, result.Error())
		require.True(t, result.Next())
		var c int64
		require.NoError(t, result.Scan(&c))
		require.Equal(t, int64(2), c)
		return nil
	})
	require.NoError(t, err)
}

// ─── Relationship Query Tests ────────────────────────────────────────────────

func TestQueryRelFilterFetch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCChain(ctx, t, db, 4) // 3 edges

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		var fetched []*graph.Relationship
		return tx.Relationships().Filter(query.KindIn(query.Relationship(), qcEdgeKind)).Fetch(func(cursor graph.Cursor[*graph.Relationship]) error {
			for rel := range cursor.Chan() {
				fetched = append(fetched, rel)
			}
			require.Len(t, fetched, 3)
			return cursor.Error()
		})
	})
	require.NoError(t, err)
}

func TestQueryRelFetchTriples(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCChain(ctx, t, db, 3) // 2 edges

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		var triples []graph.RelationshipTripleResult
		return tx.Relationships().Filter(query.KindIn(query.Relationship(), qcEdgeKind)).FetchTriples(func(cursor graph.Cursor[graph.RelationshipTripleResult]) error {
			for triple := range cursor.Chan() {
				triples = append(triples, triple)
			}
			require.Len(t, triples, 2)
			return cursor.Error()
		})
	})
	require.NoError(t, err)
}

func TestQueryRelFetchKinds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCChain(ctx, t, db, 3) // 2 edges

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		var results []graph.RelationshipKindsResult
		return tx.Relationships().Filter(query.KindIn(query.Relationship(), qcEdgeKind)).FetchKinds(func(cursor graph.Cursor[graph.RelationshipKindsResult]) error {
			for kr := range cursor.Chan() {
				results = append(results, kr)
			}
			require.Len(t, results, 2)
			for _, kr := range results {
				require.Equal(t, qcEdgeKind, kr.Kind)
			}
			return cursor.Error()
		})
	})
	require.NoError(t, err)
}

func TestQueryRelDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCChain(ctx, t, db, 3) // 2 edges

	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		return tx.Relationships().Filter(query.KindIn(query.Relationship(), qcEdgeKind)).Delete()
	})
	require.NoError(t, err)

	count := runQueryInt64(ctx, t, db, "MATCH ()-[r:QCEdge]->() RETURN count(r) AS c")
	require.Equal(t, int64(0), count)
}

func TestQueryRelUpdate(t *testing.T) {
	t.Parallel()
	t.Skip("kglite: relationship Update via query builder not yet supported")
	ctx := context.Background()
	db := openGraph(t)
	seedQCChain(ctx, t, db, 3) // 2 edges

	err := db.WriteTransaction(ctx, func(tx graph.Transaction) error {
		props := graph.NewProperties()
		props.Set("marked", true)
		return tx.Relationships().Filter(query.KindIn(query.Relationship(), qcEdgeKind)).Update(props)
	})
	require.NoError(t, err)

	err = db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH ()-[r:QCEdge]->() WHERE r.marked = true RETURN count(r) AS c", nil)
		defer result.Close()
		require.NoError(t, result.Error())
		require.True(t, result.Next())
		var c int64
		require.NoError(t, result.Scan(&c))
		require.Equal(t, int64(2), c)
		return nil
	})
	require.NoError(t, err)
}

func TestQueryRelFilterfFetch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCChain(ctx, t, db, 3) // 2 edges

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		var fetched []*graph.Relationship
		return tx.Relationships().Filterf(func() graph.Criteria {
			return query.KindIn(query.Relationship(), qcEdgeKind)
		}).Fetch(func(cursor graph.Cursor[*graph.Relationship]) error {
			for rel := range cursor.Chan() {
				fetched = append(fetched, rel)
			}
			require.Len(t, fetched, 2)
			return cursor.Error()
		})
	})
	require.NoError(t, err)
}

func TestQueryRelOrderByOffsetLimit(t *testing.T) {
	t.Parallel()
	t.Skip("kglite: OrderBy+Offset+Limit not yet supported for relationships via query builder")
	ctx := context.Background()
	db := openGraph(t)
	seedQCChain(ctx, t, db, 5) // 4 edges

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		var fetched []*graph.Relationship
		return tx.Relationships().
			Filter(query.KindIn(query.Relationship(), qcEdgeKind)).
			OrderBy(query.Order(query.RelationshipProperty("weight"), query.Ascending())).
			Offset(1).
			Limit(2).
			Fetch(func(cursor graph.Cursor[*graph.Relationship]) error {
				for rel := range cursor.Chan() {
					fetched = append(fetched, rel)
				}
				require.Len(t, fetched, 2, "expected 2 rels after offset+limit")
				return cursor.Error()
			})
	})
	require.NoError(t, err)
}

func TestQueryRelFetchIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCChain(ctx, t, db, 3) // 2 edges

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		var ids []graph.ID
		return tx.Relationships().Filter(query.KindIn(query.Relationship(), qcEdgeKind)).FetchIDs(func(cursor graph.Cursor[graph.ID]) error {
			for id := range cursor.Chan() {
				ids = append(ids, id)
			}
			require.Len(t, ids, 2)
			return cursor.Error()
		})
	})
	require.NoError(t, err)
}

// ─── Result Tests ────────────────────────────────────────────────────────────

func TestResultScanKeysValues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCNodes(ctx, t, db, 1)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:QCNode) RETURN n.name AS name, n.idx AS idx", nil)
		defer result.Close()
		require.NoError(t, result.Error())

		// Keys
		keys := result.Keys()
		require.Contains(t, keys, "name")
		require.Contains(t, keys, "idx")

		// Next + Values
		require.True(t, result.Next())
		vals := result.Values()
		require.Len(t, vals, 2)

		// Scan
		var name string
		var idx int64
		require.NoError(t, result.Scan(&name, &idx))
		require.NotEmpty(t, name)

		// No more rows
		require.False(t, result.Next())
		return result.Error()
	})
	require.NoError(t, err)
}

func TestResultScanNode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCNodes(ctx, t, db, 1)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:QCNode) RETURN n", nil)
		defer result.Close()
		require.NoError(t, result.Error())
		require.True(t, result.Next())

		var node graph.Node
		require.NoError(t, result.Scan(&node))
		require.True(t, node.Kinds.ContainsOneOf(qcNodeKind))
		return nil
	})
	require.NoError(t, err)
}

func TestResultScanRelationship(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCChain(ctx, t, db, 2) // 1 edge

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH ()-[r:QCEdge]->() RETURN r", nil)
		defer result.Close()
		require.NoError(t, result.Error())
		require.True(t, result.Next())

		var rel graph.Relationship
		require.NoError(t, result.Scan(&rel))
		require.Equal(t, qcEdgeKind, rel.Kind)
		return nil
	})
	require.NoError(t, err)
}

func TestResultErrorFromBadQuery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("THIS IS NOT VALID CYPHER", nil)
		defer result.Close()
		require.Error(t, result.Error(), "bad query should produce an error result")

		// Next should return false on error results
		require.False(t, result.Next())
		// Keys and Values should be nil on error results
		require.Nil(t, result.Keys())
		require.Nil(t, result.Values())
		return nil
	})
	require.NoError(t, err)
}

func TestResultMapper(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCNodes(ctx, t, db, 1)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:QCNode) RETURN n", nil)
		defer result.Close()
		require.NoError(t, result.Error())
		require.True(t, result.Next())

		mapper := result.Mapper()
		require.NotNil(t, mapper)
		return nil
	})
	require.NoError(t, err)
}

func TestResultValuesCache(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCNodes(ctx, t, db, 1)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:QCNode) RETURN n.name AS name", nil)
		defer result.Close()
		require.NoError(t, result.Error())
		require.True(t, result.Next())

		vals1 := result.Values()
		vals2 := result.Values()
		require.Equal(t, vals1, vals2)
		return nil
	})
	require.NoError(t, err)
}

func TestResultMultipleRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCNodes(ctx, t, db, 3)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n:QCNode) RETURN n.name AS name", nil)
		defer result.Close()
		require.NoError(t, result.Error())

		rowCount := 0
		for result.Next() {
			vals := result.Values()
			require.Len(t, vals, 1)
			keys := result.Keys()
			require.Equal(t, []string{"name"}, keys)
			rowCount++
		}
		require.Equal(t, 3, rowCount)
		return nil
	})
	require.NoError(t, err)
}

func TestQueryNodePropertyFilter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openGraph(t)
	seedQCNodes(ctx, t, db, 3)

	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		node, err := tx.Nodes().Filter(
			query.And(
				query.KindIn(query.Node(), qcNodeKind),
				query.Equals(query.NodeProperty("idx"), 0),
			),
		).First()
		require.NoError(t, err)
		require.NotNil(t, node)
		return nil
	})
	require.NoError(t, err)
}
