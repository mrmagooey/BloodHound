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

const defaultBatchFlushSize = 5000
const defaultEdgeFlushSize = 20000

// defaultDeleteFlushSize is the number of relationship IDs to accumulate
// before issuing a single batched DELETE using an IN clause, reducing
// per-edge CGO overhead during post-processing.
const defaultDeleteFlushSize = 500

// Batch implements graph.Batch using kglite with accumulated flush.
// Operations are buffered and sent to kglite in a single CypherBatch
// call when the buffer reaches flushSize or when Commit() is called.
// Edge creation is batched separately using the bulk edge FFI for performance.
type Batch struct {
	ctx            context.Context
	driver         *Driver
	pending        []kglite.BatchQuery
	pendingEdges   []kglite.EdgeSpec
	pendingDeletes []uint64 // relationship IDs pending batched DELETE
	flushSize      int
	kindsWritten   map[string]bool   // tracks objectids that already have __kinds set
	oidToIdx       map[string]uint64 // objectid -> node index cache for bulk edge FFI
	pendingLookups []string          // objectids awaiting node-index lookup after next Cypher flush
}

func (b *Batch) WithGraph(_ graph.Graph) graph.Batch {
	return b
}

func (b *Batch) Commit() error {
	return b.flush()
}

// resolvePendingLookups issues a single MATCH query to populate oidToIdx for
// all objectids in pendingLookups. This is called after the Cypher batch flush
// so that the nodes are guaranteed to exist before we try to look them up.
func (b *Batch) resolvePendingLookups() error {
	if len(b.pendingLookups) == 0 {
		return nil
	}

	if b.oidToIdx == nil {
		b.oidToIdx = make(map[string]uint64, len(b.pendingLookups))
	}

	// Build a list of objectids to look up (only those not already cached).
	toFetch := make([]string, 0, len(b.pendingLookups))
	for _, oid := range b.pendingLookups {
		if _, cached := b.oidToIdx[oid]; !cached {
			toFetch = append(toFetch, oid)
		}
	}
	b.pendingLookups = b.pendingLookups[:0]

	if len(toFetch) == 0 {
		return nil
	}

	// Convert []string to []interface{} for Cypher parameter encoding.
	oidList := make([]interface{}, len(toFetch))
	for i, oid := range toFetch {
		oidList[i] = oid
	}

	// Issue a single MATCH query for all objectids at once.
	start := time.Now()
	result, err := b.driver.kg.Cypher(
		"MATCH (n:Base) WHERE n.objectid IN $oids RETURN n.objectid, id(n)",
		map[string]interface{}{"oids": oidList},
	)
	if err != nil {
		return fmt.Errorf("kglite: resolvePendingLookups: %w", err)
	}

	for _, row := range result.Rows {
		if len(row) < 2 {
			continue
		}
		oid, ok := row[0].(string)
		if !ok {
			continue
		}
		var idx uint64
		switch v := row[1].(type) {
		case float64:
			idx = uint64(v)
		case int64:
			idx = uint64(v)
		case uint64:
			idx = v
		default:
			continue
		}
		b.oidToIdx[oid] = idx
	}
	if ProfilingEnabled() {
		recordQuery(fmt.Sprintf("[oid-lookup: %d oids]", len(toFetch)), time.Since(start))
	}
	return nil
}

func (b *Batch) flush() error {
	// Flush bulk edge creates via direct FFI (no Cypher parsing).
	// skipExisting=false lets the Rust side handle duplicate suppression,
	// which is cheaper than maintaining a Go-side cross-flush seen-map.
	if len(b.pendingEdges) > 0 {
		start := time.Now()
		if _, err := b.driver.kg.CreateEdgesBatch(b.pendingEdges, false); err != nil {
			return err
		}
		if ProfilingEnabled() {
			recordQuery(fmt.Sprintf("[batch-edges: %d edges]", len(b.pendingEdges)), time.Since(start))
		}
		b.pendingEdges = b.pendingEdges[:0]
	}
	// Flush remaining Cypher queries.
	// Use CypherBatchExec (not CypherBatch) because batch mutations -- MERGE, SET,
	// DELETE, CREATE -- do not return rows that the caller needs.  Skipping the
	// full JSON unmarshal of the result array saves one allocation + parse per flush.
	if len(b.pending) > 0 {
		start := time.Now()
		if err := b.driver.kg.CypherBatchExec(b.pending); err != nil {
			return err
		}
		if ProfilingEnabled() {
			recordQuery(fmt.Sprintf("[batch-cypher: %d queries]", len(b.pending)), time.Since(start))
		}
		b.pending = b.pending[:0]
	}
	// After the Cypher flush, resolve any pending node-index lookups so that
	// subsequent UpdateRelationshipBy calls can use the FFI path.
	if len(b.pendingLookups) > 0 {
		if err := b.resolvePendingLookups(); err != nil {
			return err
		}
	}
	// Flush any remaining pending deletes that haven't yet reached the batch threshold.
	if len(b.pendingDeletes) > 0 {
		if err := b.flushDeletes(); err != nil {
			return err
		}
	}
	return nil
}

func (b *Batch) maybeFlush() error {
	if len(b.pending) >= b.flushSize || len(b.pendingEdges) >= defaultEdgeFlushSize {
		return b.flush()
	}
	return nil
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

// DeleteRelationship enqueues a relationship ID for batched deletion.
// IDs are accumulated in pendingDeletes; when the buffer reaches
// defaultDeleteFlushSize a single IN-clause DELETE is issued, reducing
// CGO round-trips during bulk post-processing (e.g. DeleteTransitEdges).
func (b *Batch) DeleteRelationship(id graph.ID) error {
	b.pendingDeletes = append(b.pendingDeletes, uint64(id))
	if len(b.pendingDeletes) >= defaultDeleteFlushSize {
		return b.flushDeletes()
	}
	return nil
}

// flushDeletes issues a single batched DELETE for all accumulated relationship
// IDs using an IN clause, then resets the pendingDeletes slice.
func (b *Batch) flushDeletes() error {
	if len(b.pendingDeletes) == 0 {
		return nil
	}

	// Build an inline list of integer literals: [id1, id2, ...]
	// We inline the IDs rather than using a parameter because kglite's Cypher
	// engine may not support list parameters in an IN predicate.
	var sb strings.Builder
	sb.WriteString("MATCH ()-[r]->() WHERE id(r) IN [")
	for i, id := range b.pendingDeletes {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "%d", id)
	}
	sb.WriteString("] DELETE r")

	start := time.Now()
	if err := b.driver.kg.CypherBatchExec([]kglite.BatchQuery{{Query: sb.String()}}); err != nil {
		return fmt.Errorf("kglite: flushDeletes: %w", err)
	}
	if ProfilingEnabled() {
		recordQuery(fmt.Sprintf("[batch-deletes: %d ids]", len(b.pendingDeletes)), time.Since(start))
	}

	b.pendingDeletes = b.pendingDeletes[:0]
	return nil
}

// UpdateNodeBy performs an upsert of a node identified by identity kind and properties.
// If the identity property is "objectid", the node's index is scheduled for lookup after
// the next Cypher flush so that subsequent UpdateRelationshipBy calls can use the fast
// bulk edge FFI path instead of a full Cypher MERGE per relationship.
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

	// Schedule a node-index lookup for this objectid so that subsequent
	// UpdateRelationshipBy calls can use the bulk FFI path. The lookup is
	// deferred until after the Cypher flush so the node is guaranteed to exist.
	if objID, ok := identityMap["objectid"]; ok {
		if oid, ok := objID.(string); ok && oid != "" {
			b.pendingLookups = append(b.pendingLookups, oid)
		}
	}

	return b.enqueue(cypher, mergeParams(identityParams, propParams))
}

// updateRelationshipByFFI handles the fast path for UpdateRelationshipBy when both
// endpoint node indices are known. It still applies node property updates and extra
// labels via Cypher MERGE stubs, but the relationship itself is created via the bulk
// edge FFI (pendingEdges) instead of a triple-MERGE Cypher query.
func (b *Batch) updateRelationshipByFFI(
	update graph.RelationshipUpdate,
	relKindStr string,
	startIdx, endIdx uint64,
	startKindStr, endKindStr string,
	startIdentity, endIdentity map[string]any,
	startProps, endProps map[string]any,
	relProps map[string]any,
) error {
	startPattern, startIdParams := propsPattern("si_", startIdentity)
	endPattern, endIdParams := propsPattern("ei_", endIdentity)

	startSetFrag, startPropParams := setClause("s", "sp_", startProps)
	endSetFrag, endPropParams := setClause("e", "ep_", endProps)

	// Build extra-label SET parts for start node.
	startSetParts := []string{}
	if startSetFrag != "" {
		startSetParts = append(startSetParts, startSetFrag)
	}
	for _, k := range update.Start.Kinds {
		if k == graph.EmptyKind {
			continue
		}
		kindLabelStr := k.String()
		if kindLabelStr == "" {
			continue
		}
		if update.StartIdentityKind != nil && kindLabelStr == update.StartIdentityKind.String() {
			continue
		}
		startSetParts = append(startSetParts, fmt.Sprintf("s:%s", quoteIdent(kindLabelStr)))
	}

	// Emit start node MERGE (with optional SET).
	startCypher := fmt.Sprintf("MERGE (s%s %s)", startKindStr, startPattern)
	if len(startSetParts) > 0 {
		startCypher += " SET " + strings.Join(startSetParts, ", ")
	}
	if err := b.enqueue(startCypher, mergeParams(startIdParams, startPropParams)); err != nil {
		return err
	}

	// Build extra-label SET parts for end node.
	endSetParts := []string{}
	if endSetFrag != "" {
		endSetParts = append(endSetParts, endSetFrag)
	}
	for _, k := range update.End.Kinds {
		if k == graph.EmptyKind {
			continue
		}
		kindLabelStr := k.String()
		if kindLabelStr == "" {
			continue
		}
		if update.EndIdentityKind != nil && kindLabelStr == update.EndIdentityKind.String() {
			continue
		}
		endSetParts = append(endSetParts, fmt.Sprintf("e:%s", quoteIdent(kindLabelStr)))
	}

	// Emit end node MERGE (with optional SET).
	endCypher := fmt.Sprintf("MERGE (e%s %s)", endKindStr, endPattern)
	if len(endSetParts) > 0 {
		endCypher += " SET " + strings.Join(endSetParts, ", ")
	}
	if err := b.enqueue(endCypher, mergeParams(endIdParams, endPropParams)); err != nil {
		return err
	}

	// Enqueue the relationship via the bulk edge FFI -- no Cypher parsing required.
	var propsMap map[string]any
	if len(relProps) > 0 {
		propsMap = make(map[string]any, len(relProps))
		for k, v := range relProps {
			propsMap[k] = scalarize(v)
		}
	}
	b.pendingEdges = append(b.pendingEdges, kglite.EdgeSpec{
		Src:   startIdx,
		Dst:   endIdx,
		Type:  relKindStr,
		Props: propsMap,
	})
	return b.maybeFlush()
}

// UpdateRelationshipBy performs an upsert of a relationship identified by start/end/kind/properties.
//
// Fast path: if both endpoint node indices are already in the oidToIdx cache (populated
// by prior UpdateNodeBy calls followed by a flush), the relationship is created via the
// bulk edge FFI (pendingEdges -> CreateEdgesBatch) which bypasses Cypher parsing entirely.
// Node stub MERGEs are still issued via Cypher so that property updates and extra labels
// are applied correctly.
//
// Slow path: if either endpoint index is unknown, the original triple-MERGE Cypher query
// is used (MERGE start, MERGE end, MERGE relationship in one statement).
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

	// -- Fast path: both endpoint indices are cached ----------------------------
	// If both endpoint objectids are in the oidToIdx cache, we can use the bulk
	// edge FFI to create the relationship without a triple-MERGE Cypher query.
	if b.oidToIdx != nil {
		startOid, startHasOid := startIdentity["objectid"].(string)
		endOid, endHasOid := endIdentity["objectid"].(string)
		if startHasOid && endHasOid && startOid != "" && endOid != "" {
			startIdx, startCached := b.oidToIdx[startOid]
			endIdx, endCached := b.oidToIdx[endOid]
			if startCached && endCached {
				return b.updateRelationshipByFFI(
					update, relKindStr,
					startIdx, endIdx,
					startKindStr, endKindStr,
					startIdentity, endIdentity,
					startProps, endProps,
					relProps,
				)
			}
		}
	}

	// -- Slow path: fall back to the original triple-MERGE Cypher query ---------
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

	// Add extra labels for endpoint stubs.
	for _, k := range update.Start.Kinds {
		if k == graph.EmptyKind {
			continue
		}
		kindLabelStr := k.String()
		if kindLabelStr == "" {
			continue
		}
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
