// Copyright 2025 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package dawgs

import (
	"context"

	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
	neo4jquery "github.com/specterops/dawgs/query/neo4j"
)

// ─── NodeQuery ────────────────────────────────────────────────────────────────

type nodeQuery struct {
	ctx          context.Context
	tx           *Transaction
	queryBuilder *neo4jquery.QueryBuilder
}

func (q *nodeQuery) run(cypher string, params map[string]any) graph.Result {
	return q.tx.Raw(cypher, params)
}

func (q *nodeQuery) Filter(criteria graph.Criteria) graph.NodeQuery {
	q.queryBuilder.Apply(query.Where(criteria))
	return q
}

func (q *nodeQuery) Filterf(fn graph.CriteriaProvider) graph.NodeQuery {
	return q.Filter(fn())
}

func (q *nodeQuery) OrderBy(criteria ...graph.Criteria) graph.NodeQuery {
	q.queryBuilder.Apply(query.OrderBy(criteria...))
	return q
}

func (q *nodeQuery) Offset(n int) graph.NodeQuery {
	q.queryBuilder.Apply(query.Offset(n))
	return q
}

func (q *nodeQuery) Limit(n int) graph.NodeQuery {
	q.queryBuilder.Apply(query.Limit(n))
	return q
}

func (q *nodeQuery) Query(delegate func(graph.Result) error, finalCriteria ...graph.Criteria) error {
	for _, c := range finalCriteria {
		q.queryBuilder.Apply(c)
	}
	if err := q.queryBuilder.Prepare(); err != nil {
		return err
	}
	cypher, err := q.queryBuilder.Render()
	if err != nil {
		return err
	}
	result := q.run(cypher, q.queryBuilder.Parameters)
	if result.Error() != nil {
		return result.Error()
	}
	defer result.Close()
	return delegate(result)
}

func (q *nodeQuery) Delete() error {
	q.queryBuilder.Apply(query.Delete(query.Node()))
	if err := q.queryBuilder.Prepare(); err != nil {
		return err
	}
	cypher, err := q.queryBuilder.Render()
	if err != nil {
		return err
	}
	return q.run(cypher, q.queryBuilder.Parameters).Error()
}

func (q *nodeQuery) Update(properties *graph.Properties) error {
	q.queryBuilder.Apply(query.Updatef(func() graph.Criteria {
		var stmts []graph.Criteria
		if m := properties.ModifiedProperties(); len(m) > 0 {
			stmts = append(stmts, query.SetProperties(query.Node(), m))
		}
		if d := properties.DeletedProperties(); len(d) > 0 {
			stmts = append(stmts, query.DeleteProperties(query.Node(), d...))
		}
		return stmts
	}))
	if err := q.queryBuilder.Prepare(); err != nil {
		return err
	}
	cypher, err := q.queryBuilder.Render()
	if err != nil {
		return err
	}
	return q.run(cypher, q.queryBuilder.Parameters).Error()
}

func (q *nodeQuery) Count() (int64, error) {
	var count int64
	err := q.Query(func(result graph.Result) error {
		if !result.Next() {
			return graph.ErrNoResultsFound
		}
		return result.Scan(&count)
	}, query.Returning(query.Count(query.Node())))
	return count, err
}

func (q *nodeQuery) First() (*graph.Node, error) {
	var node graph.Node
	err := q.Query(func(result graph.Result) error {
		if !result.Next() {
			return graph.ErrNoResultsFound
		}
		return result.Scan(&node)
	}, query.Returning(query.Node()), query.Limit(1))
	return &node, err
}

func (q *nodeQuery) Fetch(delegate func(graph.Cursor[*graph.Node]) error, finalCriteria ...graph.Criteria) error {
	return q.Query(func(result graph.Result) error {
		cursor := graph.NewResultIterator(q.ctx, result, func(result graph.Result) (*graph.Node, error) {
			var node graph.Node
			return &node, result.Scan(&node)
		})
		defer cursor.Close()
		return delegate(cursor)
	}, append([]graph.Criteria{query.Returning(query.Node())}, finalCriteria...)...)
}

func (q *nodeQuery) FetchIDs(delegate func(graph.Cursor[graph.ID]) error) error {
	return q.Query(func(result graph.Result) error {
		cursor := graph.NewResultIterator(q.ctx, result, func(result graph.Result) (graph.ID, error) {
			var id graph.ID
			return id, result.Scan(&id)
		})
		defer cursor.Close()
		return delegate(cursor)
	}, query.Returning(query.NodeID()))
}

func (q *nodeQuery) FetchKinds(delegate func(graph.Cursor[graph.KindsResult]) error) error {
	return q.Query(func(result graph.Result) error {
		cursor := graph.NewResultIterator(q.ctx, result, func(result graph.Result) (graph.KindsResult, error) {
			var (
				id    graph.ID
				kinds graph.Kinds
				err   = result.Scan(&id, &kinds)
			)
			return graph.KindsResult{ID: id, Kinds: kinds}, err
		})
		defer cursor.Close()
		return delegate(cursor)
	}, query.Returning(query.NodeID(), query.KindsOf(query.Node())))
}

// ─── RelationshipQuery ────────────────────────────────────────────────────────

type relQuery struct {
	ctx          context.Context
	tx           *Transaction
	queryBuilder *neo4jquery.QueryBuilder
}

func (q *relQuery) run(cypher string, params map[string]any) graph.Result {
	return q.tx.Raw(cypher, params)
}

func (q *relQuery) Filter(criteria graph.Criteria) graph.RelationshipQuery {
	q.queryBuilder.Apply(query.Where(criteria))
	return q
}

func (q *relQuery) Filterf(fn graph.CriteriaProvider) graph.RelationshipQuery {
	return q.Filter(fn())
}

func (q *relQuery) OrderBy(criteria ...graph.Criteria) graph.RelationshipQuery {
	q.queryBuilder.Apply(query.OrderBy(criteria...))
	return q
}

func (q *relQuery) Offset(n int) graph.RelationshipQuery {
	q.queryBuilder.Apply(query.Offset(n))
	return q
}

func (q *relQuery) Limit(n int) graph.RelationshipQuery {
	q.queryBuilder.Apply(query.Limit(n))
	return q
}

func (q *relQuery) Query(delegate func(graph.Result) error, finalCriteria ...graph.Criteria) error {
	for _, c := range finalCriteria {
		q.queryBuilder.Apply(c)
	}
	if err := q.queryBuilder.Prepare(); err != nil {
		return err
	}
	cypher, err := q.queryBuilder.Render()
	if err != nil {
		return err
	}
	result := q.run(cypher, q.queryBuilder.Parameters)
	if result.Error() != nil {
		return result.Error()
	}
	defer result.Close()
	return delegate(result)
}

func (q *relQuery) Delete() error {
	q.queryBuilder.Apply(query.Delete(query.Relationship()))
	if err := q.queryBuilder.Prepare(); err != nil {
		return err
	}
	cypher, err := q.queryBuilder.Render()
	if err != nil {
		return err
	}
	return q.run(cypher, q.queryBuilder.Parameters).Error()
}

func (q *relQuery) Update(properties *graph.Properties) error {
	q.queryBuilder.Apply(query.Updatef(func() graph.Criteria {
		var stmts []graph.Criteria
		if m := properties.ModifiedProperties(); len(m) > 0 {
			stmts = append(stmts, query.SetProperties(query.Relationship(), m))
		}
		if d := properties.DeletedProperties(); len(d) > 0 {
			stmts = append(stmts, query.DeleteProperties(query.Relationship(), d...))
		}
		return stmts
	}))
	if err := q.queryBuilder.Prepare(); err != nil {
		return err
	}
	cypher, err := q.queryBuilder.Render()
	if err != nil {
		return err
	}
	return q.run(cypher, q.queryBuilder.Parameters).Error()
}

func (q *relQuery) Count() (int64, error) {
	var count int64
	err := q.Query(func(result graph.Result) error {
		if !result.Next() {
			return graph.ErrNoResultsFound
		}
		return result.Scan(&count)
	}, query.Returning(query.Count(query.Relationship())))
	return count, err
}

func (q *relQuery) First() (*graph.Relationship, error) {
	var rel graph.Relationship
	err := q.Query(func(result graph.Result) error {
		if !result.Next() {
			return graph.ErrNoResultsFound
		}
		return result.Scan(&rel)
	}, query.Returning(query.Relationship()), query.Limit(1))
	return &rel, err
}

func (q *relQuery) Fetch(delegate func(graph.Cursor[*graph.Relationship]) error) error {
	return q.Query(func(result graph.Result) error {
		cursor := graph.NewResultIterator(q.ctx, result, func(result graph.Result) (*graph.Relationship, error) {
			var rel graph.Relationship
			return &rel, result.Scan(&rel)
		})
		defer cursor.Close()
		return delegate(cursor)
	}, query.Returning(query.Relationship()))
}

func (q *relQuery) FetchIDs(delegate func(graph.Cursor[graph.ID]) error) error {
	return q.Query(func(result graph.Result) error {
		cursor := graph.NewResultIterator(q.ctx, result, func(result graph.Result) (graph.ID, error) {
			var id graph.ID
			return id, result.Scan(&id)
		})
		defer cursor.Close()
		return delegate(cursor)
	}, query.Returning(query.RelationshipID()))
}

func (q *relQuery) FetchKinds(delegate func(graph.Cursor[graph.RelationshipKindsResult]) error) error {
	return q.Query(func(result graph.Result) error {
		cursor := graph.NewResultIterator(q.ctx, result, func(result graph.Result) (graph.RelationshipKindsResult, error) {
			var rel graph.Relationship
			err := result.Scan(&rel)
			return graph.RelationshipKindsResult{
				RelationshipTripleResult: graph.RelationshipTripleResult{
					ID:      rel.ID,
					StartID: rel.StartID,
					EndID:   rel.EndID,
				},
				Kind: rel.Kind,
			}, err
		})
		defer cursor.Close()
		return delegate(cursor)
	}, query.Returning(query.Relationship()))
}

func (q *relQuery) FetchDirection(direction graph.Direction, delegate func(graph.Cursor[graph.DirectionalResult]) error) error {
	var returnCriteria graph.Criteria
	switch direction {
	case graph.DirectionInbound:
		returnCriteria = query.Returning(query.Relationship(), query.End())
	case graph.DirectionOutbound:
		returnCriteria = query.Returning(query.Relationship(), query.Start())
	default:
		return graph.ErrInvalidDirection
	}

	return q.Query(func(result graph.Result) error {
		cursor := graph.NewResultIterator(q.ctx, result, func(result graph.Result) (graph.DirectionalResult, error) {
			var (
				rel  graph.Relationship
				node graph.Node
			)
			err := result.Scan(&rel, &node)
			return graph.DirectionalResult{Relationship: &rel, Node: &node}, err
		})
		defer cursor.Close()
		return delegate(cursor)
	}, returnCriteria)
}

func (q *relQuery) FetchTriples(delegate func(graph.Cursor[graph.RelationshipTripleResult]) error) error {
	return q.Query(func(result graph.Result) error {
		cursor := graph.NewResultIterator(q.ctx, result, func(result graph.Result) (graph.RelationshipTripleResult, error) {
			var rel graph.Relationship
			err := result.Scan(&rel)
			return graph.RelationshipTripleResult{
				ID:      rel.ID,
				StartID: rel.StartID,
				EndID:   rel.EndID,
			}, err
		})
		defer cursor.Close()
		return delegate(cursor)
	}, query.Returning(query.Relationship()))
}

func (q *relQuery) FetchAllShortestPaths(delegate func(graph.Cursor[graph.Path]) error) error {
	// kglite supports shortestPath but not allShortestPaths; use a single shortest path as fallback
	if err := q.queryBuilder.PrepareAllShortestPaths(); err != nil {
		return err
	}
	cypher, err := q.queryBuilder.Render()
	if err != nil {
		return err
	}
	result := q.run(cypher, q.queryBuilder.Parameters)
	if result.Error() != nil {
		return result.Error()
	}
	defer result.Close()

	cursor := graph.NewResultIterator(q.ctx, result, func(result graph.Result) (graph.Path, error) {
		var path graph.Path
		return path, result.Scan(&path)
	})
	defer cursor.Close()
	return delegate(cursor)
}
