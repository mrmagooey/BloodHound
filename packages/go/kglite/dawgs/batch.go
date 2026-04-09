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
	"time"

	"github.com/specterops/bloodhound/packages/go/kglite"
	"github.com/specterops/dawgs/graph"
	neo4jquery "github.com/specterops/dawgs/query/neo4j"
)

const defaultBatchFlushSize = 2000
const defaultEdgeFlushSize = 5000

// Batch implements graph.Batch using kglite with accumulated flush.
// Operations are buffered and sent to kglite in a single CypherBatch
// call when the buffer reaches flushSize or when Commit() is called.
// Edge creation is batched separately using the bulk edge FFI for performance.
type edgeKey struct {
	src, dst uint64
	typ      string
}

type Batch struct {
	ctx          context.Context
	driver       *Driver
	pending      []kglite.BatchQuery
	pendingEdges []kglite.EdgeSpec
	flushSize    int
	kindsWritten map[string]bool    // tracks objectids that already have __kinds set
	edgesSeen    map[edgeKey]struct{} // cross-flush dedup: tracks all edges written in this batch lifecycle
}

func (b *Batch) WithGraph(_ graph.Graph) graph.Batch {
	return b
}

func (b *Batch) Commit() error {
	return b.flush()
}

func (b *Batch) flush() error {
	// Flush bulk edge creates via direct FFI (no Cypher parsing)
	if len(b.pendingEdges) > 0 {
		b.pendingEdges = b.deduplicateEdges(b.pendingEdges)
		if len(b.pendingEdges) > 0 {
			start := time.Now()
			if _, err := b.driver.kg.CreateEdgesBatch(b.pendingEdges, true); err != nil {
				return err
			}
			if ProfilingEnabled() {
				recordQuery(fmt.Sprintf("[batch-edges: %d edges]", len(b.pendingEdges)), time.Since(start))
			}
		}
		b.pendingEdges = b.pendingEdges[:0]
	}
	// Flush remaining Cypher queries
	if len(b.pending) > 0 {
		start := time.Now()
		if _, err := b.driver.kg.CypherBatch(b.pending); err != nil {
			return err
		}
		if ProfilingEnabled() {
			recordQuery(fmt.Sprintf("[batch-cypher: %d queries]", len(b.pending)), time.Since(start))
		}
		b.pending = b.pending[:0]
	}
	return nil
}

func (b *Batch) maybeFlush() error {
	if len(b.pending) >= b.flushSize || len(b.pendingEdges) >= defaultEdgeFlushSize {
		return b.flush()
	}
	return nil
}

// deduplicateEdges removes duplicate (src, dst, type) triples within this batch
// and across previous flushes, keeping the last occurrence for within-batch dupes.
func (b *Batch) deduplicateEdges(edges []kglite.EdgeSpec) []kglite.EdgeSpec {
	if b.edgesSeen == nil {
		b.edgesSeen = make(map[edgeKey]struct{}, len(edges))
	}
	// First pass: deduplicate within this batch (keep last occurrence)
	localSeen := make(map[edgeKey]int, len(edges))
	deduped := make([]kglite.EdgeSpec, 0, len(edges))
	for _, e := range edges {
		k := edgeKey{e.Src, e.Dst, e.Type}
		if idx, ok := localSeen[k]; ok {
			deduped[idx] = e
		} else {
			localSeen[k] = len(deduped)
			deduped = append(deduped, e)
		}
	}
	// Second pass: filter out edges already flushed in previous batches
	result := deduped[:0]
	for _, e := range deduped {
		k := edgeKey{e.Src, e.Dst, e.Type}
		if _, seen := b.edgesSeen[k]; !seen {
			b.edgesSeen[k] = struct{}{}
			result = append(result, e)
		}
	}
	return result
}

func (b *Batch) enqueue(cypher string, params map[string]any) error {
	b.pending = append(b.pending, kglite.BatchQuery{
		Query:  cypher,
		Params: params,
	})
	return b.maybeFlush()
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
	return b.enqueue(
		fmt.Sprintf("CREATE (n:%s %s)", quoteIdent(node.Kinds[0].String()), pattern),
		params,
	)
}

// DeleteNode deletes a node by ID.
func (b *Batch) DeleteNode(id graph.ID) error {
	return b.enqueue(
		"MATCH (n) WHERE id(n) = $id DETACH DELETE n",
		map[string]any{"id": uint64(id)},
	)
}

// CreateRelationship creates a relationship (upsert on start_id+end_id+kind).
func (b *Batch) CreateRelationship(relationship *graph.Relationship) error {
	return b.CreateRelationshipByIDs(relationship.StartID, relationship.EndID, relationship.Kind, relationship.Properties)
}

// CreateRelationshipByIDs creates a relationship between two nodes.
// Uses the bulk edge FFI which bypasses Cypher parsing for maximum throughput.
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
		propsMap = make(map[string]any, len(properties.Map))
		for k, v := range properties.Map {
			propsMap[k] = scalarize(v)
		}
	}

	b.pendingEdges = append(b.pendingEdges, kglite.EdgeSpec{
		Src:   uint64(startNodeID),
		Dst:   uint64(endNodeID),
		Type:  kindStr,
		Props: propsMap,
	})
	return b.maybeFlush()
}

// DeleteRelationship deletes a relationship by ID.
func (b *Batch) DeleteRelationship(id graph.ID) error {
	return b.enqueue(
		"MATCH ()-[r]->() WHERE id(r) = $id DELETE r",
		map[string]any{"id": uint64(id)},
	)
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
		// Copy to avoid mutating the caller's properties map.
		propsMap = make(map[string]any, len(update.Node.Properties.Map))
		for k, v := range update.Node.Properties.Map {
			propsMap[k] = v
		}
	} else {
		propsMap = map[string]any{}
	}

	// Store extra kinds as a __kinds property so they're queryable.
	// UpdateNodeBy ALWAYS writes __kinds from authoritative node ingest data,
	// even if a relationship stub previously wrote a different value (e.g., a node
	// first seen as an ADLocalGroup endpoint will later be corrected to Group when
	// groups.json processes it explicitly). Setting kindsWritten here prevents
	// UpdateRelationshipBy from later overwriting these correct kinds with incomplete
	// kinds from relationship endpoint stubs.
	if len(update.Node.Kinds) > 1 {
		allKinds := make([]string, len(update.Node.Kinds))
		for i, k := range update.Node.Kinds {
			allKinds[i] = k.String()
		}
		propsMap["__kinds"] = allKinds
		if objID, ok := identityMap["objectid"]; ok {
			if key, ok := objID.(string); ok {
				if b.kindsWritten == nil {
					b.kindsWritten = make(map[string]bool, 1024)
				}
				b.kindsWritten[key] = true
			}
		}
	}

	identityPattern, identityParams := propsPattern("id_", identityMap)
	setFrag, propParams := setClause("n", "p_", propsMap)

	var cypher string
	if setFrag == "" {
		cypher = fmt.Sprintf("MERGE (n:%s %s)", quoteIdent(kindStr), identityPattern)
	} else {
		cypher = fmt.Sprintf("MERGE (n:%s %s) SET %s", quoteIdent(kindStr), identityPattern, setFrag)
	}

	return b.enqueue(cypher, mergeParams(identityParams, propParams))
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
	if update.Start != nil && len(update.Start.Kinds) > 1 {
		if objID, ok := startIdentity["objectid"]; ok {
			if key, ok := objID.(string); ok {
				if b.kindsWritten == nil {
					b.kindsWritten = make(map[string]bool, 1024)
				}
				if !b.kindsWritten[key] {
					copied := make(map[string]any, len(startProps)+1)
					for k, v := range startProps {
						copied[k] = v
					}
					allKinds := make([]string, len(update.Start.Kinds))
					for i, k := range update.Start.Kinds {
						allKinds[i] = k.String()
					}
					copied["__kinds"] = allKinds
					startProps = copied
					b.kindsWritten[key] = true
				}
			}
		}
	}
	endProps := make(map[string]any)
	if update.End != nil && update.End.Properties != nil {
		endProps = update.End.Properties.Map
	}
	if update.End != nil && len(update.End.Kinds) > 1 {
		if objID, ok := endIdentity["objectid"]; ok {
			if key, ok := objID.(string); ok {
				if b.kindsWritten == nil {
					b.kindsWritten = make(map[string]bool, 1024)
				}
				if !b.kindsWritten[key] {
					copied := make(map[string]any, len(endProps)+1)
					for k, v := range endProps {
						copied[k] = v
					}
					allKinds := make([]string, len(update.End.Kinds))
					for i, k := range update.End.Kinds {
						allKinds[i] = k.String()
					}
					copied["__kinds"] = allKinds
					endProps = copied
					b.kindsWritten[key] = true
				}
			}
		}
	}

	startPattern, startIdParams := propsPattern("si_", startIdentity)
	endPattern, endIdParams := propsPattern("ei_", endIdentity)
	relPattern, relParams := propsPattern("rp_", relProps)

	cypher := fmt.Sprintf(
		`MERGE (s%s %s) MERGE (e%s %s) MERGE (s)-[r:%s %s]->(e)`,
		startKindStr, startPattern, endKindStr, endPattern, quoteIdent(relKindStr), relPattern,
	)

	// SET node properties
	setParts := []string{}
	startSetFrag, startPropParams := setClause("s", "sp_", startProps)
	endSetFrag, endPropParams := setClause("e", "ep_", endProps)
	if startSetFrag != "" {
		setParts = append(setParts, startSetFrag)
	}
	if endSetFrag != "" {
		setParts = append(setParts, endSetFrag)
	}

	// Add extra labels for endpoint stubs — mirrors Neo4j's "SET s:Kind1, s:Kind2" behaviour.
	// When a relationship endpoint references a node by a different identity kind than its
	// declared kind (e.g., a "User" endpoint matched by "Base"), the stub node must also
	// receive its declared kind as an extra label so that label-filtered queries (e.g.
	// MATCH (n:User)) can find it. Without this, stub nodes are stranded under their
	// identity kind only (e.g. "Base") and are invisible to label-specific queries.
	// The identity kind (first kind in the list, used in the MERGE pattern) is already the
	// primary label of the node; extra kinds are secondary labels added here.
	for _, k := range update.Start.Kinds {
		if k == graph.EmptyKind {
			continue
		}
		kindLabelStr := k.String()
		if kindLabelStr == "" {
			continue
		}
		// Skip the identity kind — it is already the primary label from the MERGE pattern
		if update.StartIdentityKind != nil && kindLabelStr == update.StartIdentityKind.String() {
			continue
		}
		setParts = append(setParts, fmt.Sprintf("s:%s", quoteIdent(kindLabelStr)))
	}
	for _, k := range update.End.Kinds {
		if k == graph.EmptyKind {
			continue
		}
		kindLabelStr := k.String()
		if kindLabelStr == "" {
			continue
		}
		// Skip the identity kind — it is already the primary label from the MERGE pattern
		if update.EndIdentityKind != nil && kindLabelStr == update.EndIdentityKind.String() {
			continue
		}
		setParts = append(setParts, fmt.Sprintf("e:%s", quoteIdent(kindLabelStr)))
	}

	if len(setParts) > 0 {
		cypher += " SET " + strings.Join(setParts, ", ")
	}

	return b.enqueue(cypher, mergeParams(startIdParams, endIdParams, relParams, startPropParams, endPropParams))
}
