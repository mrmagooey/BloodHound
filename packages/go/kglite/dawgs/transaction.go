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
	"fmt"

	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
	neo4jquery "github.com/specterops/dawgs/query/neo4j"
	"github.com/specterops/dawgs/util/size"
)

// Transaction implements graph.Transaction using kglite.
type Transaction struct {
	ctx      context.Context
	driver   *Driver
	readOnly bool
}

func (t *Transaction) WithGraph(_ graph.Graph) graph.Transaction {
	return t
}

func (t *Transaction) GraphQueryMemoryLimit() size.Size {
	return graphQueryMemoryLimit()
}

func (t *Transaction) Commit() error {
	return nil
}

// Raw executes a raw Cypher query and returns a graph.Result.
func (t *Transaction) Raw(cypher string, parameters map[string]any) graph.Result {
	result, err := t.driver.kg.Cypher(cypher, parameters)
	if err != nil {
		return newErrorResult(err)
	}
	// Convert JSON values in rows to DAWGS types
	for i, row := range result.Rows {
		for j, v := range row {
			result.Rows[i][j] = convertValue(v)
		}
	}
	return newResult(result)
}

// Query is an alias for Raw.
func (t *Transaction) Query(cypher string, parameters map[string]any) graph.Result {
	return t.Raw(cypher, parameters)
}

// Nodes returns a NodeQuery scoped to this transaction.
func (t *Transaction) Nodes() graph.NodeQuery {
	return &nodeQuery{
		ctx:          t.ctx,
		tx:           t,
		queryBuilder: neo4jquery.NewEmptyQueryBuilder(),
	}
}

// Relationships returns a RelationshipQuery scoped to this transaction.
func (t *Transaction) Relationships() graph.RelationshipQuery {
	return &relQuery{
		ctx:          t.ctx,
		tx:           t,
		queryBuilder: neo4jquery.NewEmptyQueryBuilder(),
	}
}

// CreateNode creates a new node.
func (t *Transaction) CreateNode(properties *graph.Properties, kinds ...graph.Kind) (*graph.Node, error) {
	if len(kinds) == 0 {
		return nil, fmt.Errorf("kglite: CreateNode requires at least one kind")
	}

	// Build: CREATE (n:`Kind` {key: $p_key, ...}) RETURN n
	var propsMap map[string]any
	if properties != nil {
		propsMap = properties.Map
	}
	pattern, params := propsPattern("p_", propsMap)

	result, err := t.driver.kg.Cypher(
		fmt.Sprintf("CREATE (n:%s %s) RETURN n", quoteIdent(kinds[0].String()), pattern),
		params,
	)
	if err != nil {
		return nil, fmt.Errorf("kglite: CreateNode: %w", err)
	}
	if len(result.Rows) == 0 || len(result.Rows[0]) == 0 {
		return nil, fmt.Errorf("kglite: CreateNode: no result returned")
	}

	v := convertValue(result.Rows[0][0])
	if node, ok := v.(*graph.Node); ok {
		// Add additional kinds
		for _, k := range kinds[1:] {
			if k != nil {
				node.Kinds = node.Kinds.Add(k)
			}
		}
		return node, nil
	}
	return nil, fmt.Errorf("kglite: CreateNode: unexpected result type %T", v)
}

// UpdateNode updates a node's properties.
func (t *Transaction) UpdateNode(node *graph.Node) error {
	if node.Properties == nil {
		return nil
	}
	modified := node.Properties.ModifiedProperties()
	if len(modified) == 0 {
		return nil
	}

	qb := neo4jquery.NewEmptyQueryBuilder()
	qb.Apply(query.Where(query.Equals(query.NodeID(), node.ID)))
	qb.Apply(query.Updatef(func() graph.Criteria {
		return query.SetProperties(query.Node(), modified)
	}))

	if err := qb.Prepare(); err != nil {
		return err
	}
	cypher, err := qb.Render()
	if err != nil {
		return err
	}
	_, err = t.driver.kg.Cypher(cypher, qb.Parameters)
	return err
}

// CreateRelationshipByIDs creates a relationship between two nodes by their IDs.
func (t *Transaction) CreateRelationshipByIDs(startNodeID, endNodeID graph.ID, kind graph.Kind, properties *graph.Properties) (*graph.Relationship, error) {
	kindStr := kind.String()
	if kindStr == "" {
		return nil, fmt.Errorf("kglite: CreateRelationshipByIDs: relationship kind cannot be empty")
	}

	var propsMap map[string]any
	if properties != nil {
		propsMap = properties.Map
	} else {
		propsMap = map[string]any{}
	}

	baseParams := map[string]any{
		"start_id": uint64(startNodeID),
		"end_id":   uint64(endNodeID),
	}
	relPattern, propParams := propsPattern("rp_", propsMap)

	cypher := fmt.Sprintf(
		`MATCH (s) WHERE id(s) = $start_id MATCH (e) WHERE id(e) = $end_id CREATE (s)-[r:%s %s]->(e) RETURN r`,
		quoteIdent(kindStr), relPattern,
	)
	result, err := t.driver.kg.Cypher(cypher, mergeParams(baseParams, propParams))
	if err != nil {
		return nil, fmt.Errorf("kglite: CreateRelationshipByIDs: %w", err)
	}
	if len(result.Rows) == 0 || len(result.Rows[0]) == 0 {
		return nil, fmt.Errorf("kglite: CreateRelationshipByIDs: no result")
	}

	v := convertValue(result.Rows[0][0])
	if rel, ok := v.(*graph.Relationship); ok {
		return rel, nil
	}
	return nil, fmt.Errorf("kglite: CreateRelationshipByIDs: unexpected type %T", v)
}

// UpdateRelationship updates a relationship's properties.
func (t *Transaction) UpdateRelationship(relationship *graph.Relationship) error {
	if relationship.Properties == nil {
		return nil
	}
	modified := relationship.Properties.ModifiedProperties()
	if len(modified) == 0 {
		return nil
	}

	qb := neo4jquery.NewEmptyQueryBuilder()
	qb.Apply(query.Where(query.Equals(query.RelationshipID(), relationship.ID)))
	qb.Apply(query.Updatef(func() graph.Criteria {
		return query.SetProperties(query.Relationship(), modified)
	}))

	if err := qb.Prepare(); err != nil {
		return err
	}
	cypher, err := qb.Render()
	if err != nil {
		return err
	}
	_, err = t.driver.kg.Cypher(cypher, qb.Parameters)
	return err
}
