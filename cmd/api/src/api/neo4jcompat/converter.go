// Copyright 2024 Specter Ops, Inc.
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

package neo4jcompat

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/specterops/bloodhound/cmd/api/src/model"
)

// ConvertUnifiedGraphToNeo4jResult converts a BloodHound UnifiedGraph response into
// a Neo4j HTTP API StatementResult. The conversion maps nodes and edges into a columnar
// format that Neo4j clients expect.
//
// For graph queries returning nodes, each node becomes a row with column "n".
// For graph queries returning edges, each edge becomes a row with columns "r", "source", "target".
// For literal results, each literal becomes a row with its key as a column name.
func ConvertUnifiedGraphToNeo4jResult(graphResponse model.UnifiedGraph) StatementResult {
	// If there are literals, return them in columnar format
	if len(graphResponse.Literals) > 0 {
		return convertLiterals(graphResponse)
	}

	// If there are only nodes (no edges), return nodes
	if len(graphResponse.Nodes) > 0 && len(graphResponse.Edges) == 0 {
		return convertNodes(graphResponse)
	}

	// If there are edges (and possibly nodes), return edges with source/target info
	if len(graphResponse.Edges) > 0 {
		return convertEdges(graphResponse)
	}

	// Empty result
	return StatementResult{
		Columns: []string{},
		Data:    []RowResult{},
	}
}

func convertLiterals(graphResponse model.UnifiedGraph) StatementResult {
	// Determine column names by scanning until we see a repeated key,
	// which signals the start of a second row.
	columns := []string{}
	columnIndex := map[string]int{}

	for _, lit := range graphResponse.Literals {
		if _, exists := columnIndex[lit.Key]; exists {
			// Repeated key means we've seen all columns already
			break
		}
		columnIndex[lit.Key] = len(columns)
		columns = append(columns, lit.Key)
	}

	if len(columns) == 0 {
		return StatementResult{
			Columns: []string{},
			Data:    []RowResult{},
		}
	}

	// Group literals into rows using the column count
	numCols := len(columns)
	numLits := len(graphResponse.Literals)
	rows := make([]RowResult, 0, (numLits+numCols-1)/numCols)

	for i := 0; i < numLits; i += numCols {
		row := make([]any, numCols)
		meta := make([]RowMeta, numCols)
		for j := 0; j < numCols && i+j < numLits; j++ {
			lit := graphResponse.Literals[i+j]
			colIdx := columnIndex[lit.Key]
			row[colIdx] = lit.Value
			meta[colIdx] = RowMeta{
				ID:        0,
				ElementID: "",
				Type:      "literal",
				Deleted:   false,
			}
		}
		rows = append(rows, RowResult{Row: row, Meta: meta})
	}

	return StatementResult{
		Columns: columns,
		Data:    rows,
	}
}

func convertNodes(graphResponse model.UnifiedGraph) StatementResult {
	result := StatementResult{
		Columns: []string{"n"},
		Data:    make([]RowResult, 0, len(graphResponse.Nodes)),
	}

	for idStr, node := range graphResponse.Nodes {
		nodeMap := buildNodeMap(node)

		nodeID := parseNodeID(idStr)
		result.Data = append(result.Data, RowResult{
			Row: []any{nodeMap},
			Meta: []RowMeta{
				{
					ID:        nodeID,
					ElementID: fmt.Sprintf("4:bloodhound:0:%s", idStr),
					Type:      "node",
					Deleted:   false,
				},
			},
		})
	}

	return result
}

func convertEdges(graphResponse model.UnifiedGraph) StatementResult {
	result := StatementResult{
		Columns: []string{"r", "source", "target"},
		Data:    make([]RowResult, 0, len(graphResponse.Edges)),
	}

	for i, edge := range graphResponse.Edges {
		edgeMap := map[string]any{
			"type": edge.Kind,
		}
		if edge.Properties != nil {
			for k, v := range edge.Properties {
				edgeMap[k] = v
			}
		}

		// Build source node if available
		var sourceMap any
		if sourceNode, ok := graphResponse.Nodes[edge.Source]; ok {
			sourceMap = buildNodeMap(sourceNode)
		} else {
			sourceMap = map[string]any{"_id": edge.Source}
		}

		// Build target node if available
		var targetMap any
		if targetNode, ok := graphResponse.Nodes[edge.Target]; ok {
			targetMap = buildNodeMap(targetNode)
		} else {
			targetMap = map[string]any{"_id": edge.Target}
		}

		result.Data = append(result.Data, RowResult{
			Row: []any{edgeMap, sourceMap, targetMap},
			Meta: []RowMeta{
				{
					ID:        int64(i),
					ElementID: fmt.Sprintf("5:bloodhound:0:%d", i),
					Type:      "relationship",
					Deleted:   false,
				},
				{
					ID:        parseNodeID(edge.Source),
					ElementID: fmt.Sprintf("4:bloodhound:0:%s", edge.Source),
					Type:      "node",
					Deleted:   false,
				},
				{
					ID:        parseNodeID(edge.Target),
					ElementID: fmt.Sprintf("4:bloodhound:0:%s", edge.Target),
					Type:      "node",
					Deleted:   false,
				},
			},
		})
	}

	return result
}

func buildNodeMap(node model.UnifiedNode) map[string]any {
	nodeMap := map[string]any{}

	// Include properties first so explicit fields can override
	if node.Properties != nil {
		for k, v := range node.Properties {
			nodeMap[k] = v
		}
	}

	// Always include core fields
	nodeMap["name"] = node.Label
	nodeMap["objectid"] = node.ObjectId

	if node.Kind != "" {
		nodeMap["_kind"] = node.Kind
	}

	if len(node.Kinds) > 0 {
		nodeMap["_labels"] = strings.Join(node.Kinds, ":")
	}

	return nodeMap
}

func parseNodeID(idStr string) int64 {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0
	}
	return id
}
