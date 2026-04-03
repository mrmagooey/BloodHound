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
	"testing"

	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertUnifiedGraphToNeo4jResult_EmptyGraph(t *testing.T) {
	graphResponse := model.NewUnifiedGraph()
	result := ConvertUnifiedGraphToNeo4jResult(graphResponse)

	assert.Empty(t, result.Columns)
	assert.Empty(t, result.Data)
}

func TestConvertUnifiedGraphToNeo4jResult_Nodes(t *testing.T) {
	graphResponse := model.UnifiedGraph{
		Nodes: map[string]model.UnifiedNode{
			"123": {
				Label:    "Alice",
				Kind:     "User",
				Kinds:    []string{"Base", "User"},
				ObjectId: "S-1-5-21-abc",
				Properties: map[string]any{
					"enabled": true,
				},
			},
			"456": {
				Label:    "Bob",
				Kind:     "User",
				Kinds:    []string{"Base", "User"},
				ObjectId: "S-1-5-21-def",
			},
		},
		Edges:    []model.UnifiedEdge{},
		Literals: graph.Literals{},
	}

	result := ConvertUnifiedGraphToNeo4jResult(graphResponse)

	assert.Equal(t, []string{"n"}, result.Columns)
	assert.Len(t, result.Data, 2)

	// Check that each row has proper structure
	for _, row := range result.Data {
		require.Len(t, row.Row, 1)
		require.Len(t, row.Meta, 1)

		nodeMap, ok := row.Row[0].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, nodeMap, "name")
		assert.Contains(t, nodeMap, "objectid")
		assert.Equal(t, "node", row.Meta[0].Type)
		assert.False(t, row.Meta[0].Deleted)
	}
}

func TestConvertUnifiedGraphToNeo4jResult_Edges(t *testing.T) {
	graphResponse := model.UnifiedGraph{
		Nodes: map[string]model.UnifiedNode{
			"1": {Label: "Alice", Kind: "User", ObjectId: "S-1"},
			"2": {Label: "Admins", Kind: "Group", ObjectId: "S-2"},
		},
		Edges: []model.UnifiedEdge{
			{
				Source: "1",
				Target: "2",
				Kind:   "MemberOf",
				Label:  "MemberOf",
			},
		},
		Literals: graph.Literals{},
	}

	result := ConvertUnifiedGraphToNeo4jResult(graphResponse)

	assert.Equal(t, []string{"r", "source", "target"}, result.Columns)
	require.Len(t, result.Data, 1)

	row := result.Data[0]
	require.Len(t, row.Row, 3)
	require.Len(t, row.Meta, 3)

	// Check edge
	edgeMap, ok := row.Row[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "MemberOf", edgeMap["type"])

	// Check source node
	sourceMap, ok := row.Row[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Alice", sourceMap["name"])

	// Check target node
	targetMap, ok := row.Row[2].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Admins", targetMap["name"])

	// Check meta types
	assert.Equal(t, "relationship", row.Meta[0].Type)
	assert.Equal(t, "node", row.Meta[1].Type)
	assert.Equal(t, "node", row.Meta[2].Type)
}

func TestConvertUnifiedGraphToNeo4jResult_Literals(t *testing.T) {
	graphResponse := model.UnifiedGraph{
		Nodes: map[string]model.UnifiedNode{},
		Edges: []model.UnifiedEdge{},
		Literals: graph.Literals{
			{Key: "count", Value: 42},
			{Key: "label", Value: "test"},
		},
	}

	result := ConvertUnifiedGraphToNeo4jResult(graphResponse)

	require.Len(t, result.Columns, 2)
	assert.Contains(t, result.Columns, "count")
	assert.Contains(t, result.Columns, "label")

	require.Len(t, result.Data, 1)
	row := result.Data[0]
	require.Len(t, row.Row, 2)

	// Find value by column index
	for i, col := range result.Columns {
		switch col {
		case "count":
			assert.Equal(t, 42, row.Row[i])
		case "label":
			assert.Equal(t, "test", row.Row[i])
		}
	}
}

func TestConvertUnifiedGraphToNeo4jResult_EdgesWithMissingNodes(t *testing.T) {
	graphResponse := model.UnifiedGraph{
		Nodes: map[string]model.UnifiedNode{},
		Edges: []model.UnifiedEdge{
			{
				Source: "1",
				Target: "2",
				Kind:   "MemberOf",
			},
		},
		Literals: graph.Literals{},
	}

	result := ConvertUnifiedGraphToNeo4jResult(graphResponse)

	require.Len(t, result.Data, 1)
	row := result.Data[0]

	// Source should be a fallback map with _id
	sourceMap, ok := row.Row[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "1", sourceMap["_id"])

	targetMap, ok := row.Row[2].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "2", targetMap["_id"])
}

func TestBuildNodeMap(t *testing.T) {
	node := model.UnifiedNode{
		Label:    "TestNode",
		Kind:     "Computer",
		Kinds:    []string{"Base", "Computer"},
		ObjectId: "obj-123",
		Properties: map[string]any{
			"enabled":        true,
			"operatingsystem": "Windows",
		},
	}

	nodeMap := buildNodeMap(node)

	assert.Equal(t, "TestNode", nodeMap["name"])
	assert.Equal(t, "obj-123", nodeMap["objectid"])
	assert.Equal(t, "Computer", nodeMap["_kind"])
	assert.Equal(t, "Base:Computer", nodeMap["_labels"])
	assert.Equal(t, true, nodeMap["enabled"])
	assert.Equal(t, "Windows", nodeMap["operatingsystem"])
}

func TestParseNodeID(t *testing.T) {
	assert.Equal(t, int64(123), parseNodeID("123"))
	assert.Equal(t, int64(0), parseNodeID("abc"))
	assert.Equal(t, int64(0), parseNodeID(""))
}
