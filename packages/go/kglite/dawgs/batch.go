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
	"strings"

	"github.com/specterops/dawgs/graph"
	neo4jquery "github.com/specterops/dawgs/query/neo4j"
)

// Batch implements graph.Batch using kglite.
type Batch struct {
	ctx    context.Context
	driver *Driver
}

func (b *Batch) WithGraph(_ graph.Graph) graph.Batch {
	return b
}

func (b *Batch) Commit() error {
	return nil
}

// Nodes returns a NodeQuery for bulk operations.
func (b *Batch) Nodes() graph.NodeQuery {
	return &nodeQuery{
		ctx: b.ctx,
		tx: &Transaction{
			ctx:      b.ctx,
			driver:   b.driver,
			readOnly: false,
		},
		queryBuilder: neo4jquery.NewEmptyQueryBuilder(),
	}
}

// Relationships returns a RelationshipQuery for bulk operations.
func (b *Batch) Relationships() graph.RelationshipQuery {
	return &relQuery{
		ctx: b.ctx,
		tx: &Transaction{
			ctx:      b.ctx,
			driver:   b.driver,
			readOnly: false,
		},
		queryBuilder: neo4jquery.NewEmptyQueryBuilder(),
	}
}

// CreateNode creates a new node in the graph.
func (b *Batch) CreateNode(node *graph.Node) error {
	if len(node.Kinds) == 0 {
		return fmt.Errorf("kglite: CreateNode requires at least one kind")
	}

	var propsMap map[string]any
	if node.Properties != nil {
		propsMap = node.Properties.Map
	} else {
		propsMap = map[string]any{}
	}

	pattern, params := propsPattern("p_", propsMap)
	_, err := b.driver.kg.Cypher(
		fmt.Sprintf("CREATE (n:%s %s)", quoteIdent(node.Kinds[0].String()), pattern),
		params,
	)
	return err
}

// DeleteNode deletes a node by ID.
func (b *Batch) DeleteNode(id graph.ID) error {
	_, err := b.driver.kg.Cypher(
		"MATCH (n) WHERE id(n) = $id DETACH DELETE n",
		map[string]any{"id": uint64(id)},
	)
	return err
}

// CreateRelationship creates a relationship (upsert on start_id+end_id+kind).
func (b *Batch) CreateRelationship(relationship *graph.Relationship) error {
	return b.CreateRelationshipByIDs(relationship.StartID, relationship.EndID, relationship.Kind, relationship.Properties)
}

// CreateRelationshipByIDs creates or updates a relationship between two nodes.
func (b *Batch) CreateRelationshipByIDs(startNodeID, endNodeID graph.ID, kind graph.Kind, properties *graph.Properties) error {
	if kind == nil {
		return fmt.Errorf("kglite: CreateRelationshipByIDs: relationship kind is nil")
	}
	kindStr := kind.String()
	if kindStr == "" {
		return fmt.Errorf("kglite: CreateRelationshipByIDs: relationship kind cannot be empty")
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

	relPattern, relParams := propsPattern("rp_", propsMap)
	cypher := fmt.Sprintf(
		`MATCH (s) WHERE id(s) = $start_id MATCH (e) WHERE id(e) = $end_id MERGE (s)-[r:%s %s]->(e)`,
		quoteIdent(kindStr), relPattern,
	)
	_, err := b.driver.kg.Cypher(cypher, mergeParams(baseParams, relParams))
	return err
}

// DeleteRelationship deletes a relationship by ID.
func (b *Batch) DeleteRelationship(id graph.ID) error {
	_, err := b.driver.kg.Cypher(
		"MATCH ()-[r]->() WHERE id(r) = $id DELETE r",
		map[string]any{"id": uint64(id)},
	)
	return err
}

// UpdateNodeBy performs an upsert of a node identified by identity kind and properties.
func (b *Batch) UpdateNodeBy(update graph.NodeUpdate) error {
	if update.Node == nil || len(update.Node.Kinds) == 0 {
		return fmt.Errorf("kglite: UpdateNodeBy: node must have at least one kind")
	}

	kindStr := ""
	if update.IdentityKind != nil {
		kindStr = update.IdentityKind.String()
	}
	if kindStr == "" {
		kindStr = update.Node.Kinds[0].String()
	}
	if kindStr == "" {
		return fmt.Errorf("kglite: UpdateNodeBy: cannot determine node kind")
	}

	// Build identity match map from IdentityProperties
	identityMap := make(map[string]any)
	if update.Node.Properties != nil {
		for _, prop := range update.IdentityProperties {
			if val, ok := update.Node.Properties.Map[prop]; ok {
				identityMap[prop] = val
			}
		}
	}

	var propsMap map[string]any
	if update.Node.Properties != nil {
		propsMap = update.Node.Properties.Map
	} else {
		propsMap = map[string]any{}
	}

	// Store extra kinds as a __kinds property so they're queryable.
	// kglite nodes have a single node_type; SET n:Label is not supported.
	if len(update.Node.Kinds) > 1 {
		allKinds := make([]string, len(update.Node.Kinds))
		for i, k := range update.Node.Kinds {
			allKinds[i] = k.String()
		}
		propsMap["__kinds"] = allKinds
	}

	identityPattern, identityParams := propsPattern("id_", identityMap)
	setFrag, propParams := setClause("n", "p_", propsMap)

	var cypher string
	if setFrag == "" {
		cypher = fmt.Sprintf("MERGE (n:%s %s)", quoteIdent(kindStr), identityPattern)
	} else {
		cypher = fmt.Sprintf("MERGE (n:%s %s) SET %s", quoteIdent(kindStr), identityPattern, setFrag)
	}

	_, err := b.driver.kg.Cypher(cypher, mergeParams(identityParams, propParams))
	return err
}

// UpdateRelationshipBy performs an upsert of a relationship identified by start/end/kind/properties.
func (b *Batch) UpdateRelationshipBy(update graph.RelationshipUpdate) error {
	if update.Relationship == nil {
		return fmt.Errorf("kglite: UpdateRelationshipBy: relationship is nil")
	}

	if update.Relationship.Kind == nil {
		return fmt.Errorf("kglite: UpdateRelationshipBy: relationship kind is nil")
	}
	relKindStr := update.Relationship.Kind.String()
	if relKindStr == "" {
		return fmt.Errorf("kglite: UpdateRelationshipBy: relationship kind cannot be empty")
	}

	startKindStr := ""
	if update.StartIdentityKind != nil && update.StartIdentityKind.String() != "" {
		startKindStr = ":" + quoteIdent(update.StartIdentityKind.String())
	}
	endKindStr := ""
	if update.EndIdentityKind != nil && update.EndIdentityKind.String() != "" {
		endKindStr = ":" + quoteIdent(update.EndIdentityKind.String())
	}

	// Build identity match maps
	startIdentity := make(map[string]any)
	if update.Start != nil && update.Start.Properties != nil {
		for _, p := range update.StartIdentityProperties {
			if val, ok := update.Start.Properties.Map[p]; ok {
				startIdentity[p] = val
			}
		}
	}
	endIdentity := make(map[string]any)
	if update.End != nil && update.End.Properties != nil {
		for _, p := range update.EndIdentityProperties {
			if val, ok := update.End.Properties.Map[p]; ok {
				endIdentity[p] = val
			}
		}
	}

	relProps := make(map[string]any)
	if update.Relationship.Properties != nil {
		relProps = update.Relationship.Properties.Map
	}
	startProps := make(map[string]any)
	if update.Start != nil && update.Start.Properties != nil {
		startProps = update.Start.Properties.Map
	}
	endProps := make(map[string]any)
	if update.End != nil && update.End.Properties != nil {
		endProps = update.End.Properties.Map
	}

	startPattern, startIdParams := propsPattern("si_", startIdentity)
	endPattern, endIdParams := propsPattern("ei_", endIdentity)
	relPattern, relParams := propsPattern("rp_", relProps)

	// Inline relationship props in MERGE pattern; kglite doesn't support SET for relationship vars.
	// Backtick-quote kind names to avoid reserved keyword collisions (e.g. "Contains").
	cypher := fmt.Sprintf(
		`MERGE (s%s %s) MERGE (e%s %s) MERGE (s)-[r:%s %s]->(e)`,
		startKindStr, startPattern, endKindStr, endPattern, quoteIdent(relKindStr), relPattern,
	)

	// SET node properties (node vars are supported in SET)
	setParts := []string{}
	startSetFrag, startPropParams := setClause("s", "sp_", startProps)
	endSetFrag, endPropParams := setClause("e", "ep_", endProps)
	if startSetFrag != "" {
		setParts = append(setParts, startSetFrag)
	}
	if endSetFrag != "" {
		setParts = append(setParts, endSetFrag)
	}

	if len(setParts) > 0 {
		cypher += " SET " + strings.Join(setParts, ", ")
	}

	_, err := b.driver.kg.Cypher(cypher, mergeParams(startIdParams, endIdParams, relParams, startPropParams, endPropParams))
	return err
}
