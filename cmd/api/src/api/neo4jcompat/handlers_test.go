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
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	dbmocks "github.com/specterops/bloodhound/cmd/api/src/database/mocks"
	querymocks "github.com/specterops/bloodhound/cmd/api/src/queries/mocks"
	"github.com/specterops/bloodhound/cmd/api/src/queries"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func setupTestResource(t *testing.T) (*Neo4jResource, *querymocks.MockGraph, *dbmocks.MockDatabase) {
	ctrl := gomock.NewController(t)
	mockGraph := querymocks.NewMockGraph(ctrl)
	mockDB := dbmocks.NewMockDatabase(ctrl)

	resource := &Neo4jResource{
		GraphQuery: mockGraph,
		DB:         mockDB,
		TxManager: &TransactionManager{
			transactions: make(map[int64]*OpenTransaction),
		},
	}

	return resource, mockGraph, mockDB
}

func makeRequest(t *testing.T, handler http.HandlerFunc, method, path string, body any, vars map[string]string) *httptest.ResponseRecorder {
	var reqBody []byte
	if body != nil {
		var err error
		reqBody, err = json.Marshal(body)
		require.NoError(t, err)
	}

	req := httptest.NewRequest(method, path, bytes.NewReader(reqBody))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if vars != nil {
		req = mux.SetURLVars(req, vars)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestTransactionCommit_Success(t *testing.T) {
	resource, mockGraph, mockDB := setupTestResource(t)

	mockDB.EXPECT().GetDisplayNodeGraphKinds(gomock.Any()).Return(map[graph.Kind]bool{}, nil)
	mockGraph.EXPECT().PrepareCypherQuery("RETURN 1", int64(queries.DefaultQueryFitnessLowerBoundExplore)).Return(queries.PreparedQuery{}, nil)
	mockGraph.EXPECT().RawCypherQuery(gomock.Any(), gomock.Any(), gomock.Any(), true).Return(model.UnifiedGraph{
		Nodes:    map[string]model.UnifiedNode{},
		Edges:    []model.UnifiedEdge{},
		Literals: graph.Literals{{Key: "1", Value: 1}},
	}, nil)

	body := TransactionRequest{
		Statements: []Statement{{Statement: "RETURN 1"}},
	}

	rr := makeRequest(t, resource.TransactionCommit, http.MethodPost, "/db/neo4j/tx/commit", body, map[string]string{"databaseName": "neo4j"})

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp TransactionResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Empty(t, resp.Errors)
	assert.Len(t, resp.Results, 1)
}

func TestTransactionCommit_SyntaxError(t *testing.T) {
	resource, mockGraph, mockDB := setupTestResource(t)

	mockDB.EXPECT().GetDisplayNodeGraphKinds(gomock.Any()).Return(map[graph.Kind]bool{}, nil)
	mockGraph.EXPECT().PrepareCypherQuery("INVALID QUERY", int64(queries.DefaultQueryFitnessLowerBoundExplore)).Return(queries.PreparedQuery{}, fmt.Errorf("syntax error"))

	body := TransactionRequest{
		Statements: []Statement{{Statement: "INVALID QUERY"}},
	}

	rr := makeRequest(t, resource.TransactionCommit, http.MethodPost, "/db/neo4j/tx/commit", body, map[string]string{"databaseName": "neo4j"})

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp TransactionResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Errors, 1)
	assert.Equal(t, "Neo.ClientError.Statement.SyntaxError", resp.Errors[0].Code)
}

func TestTransactionCommit_EmptyBody(t *testing.T) {
	resource, _, _ := setupTestResource(t)

	req := httptest.NewRequest(http.MethodPost, "/db/neo4j/tx/commit", nil)
	req = mux.SetURLVars(req, map[string]string{"databaseName": "neo4j"})
	rr := httptest.NewRecorder()

	resource.TransactionCommit(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp TransactionResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Empty(t, resp.Errors)
	assert.Empty(t, resp.Results)
}

func TestTransactionBegin(t *testing.T) {
	resource, _, _ := setupTestResource(t)

	req := httptest.NewRequest(http.MethodPost, "/db/neo4j/tx", nil)
	req = mux.SetURLVars(req, map[string]string{"databaseName": "neo4j"})
	rr := httptest.NewRecorder()

	resource.TransactionBegin(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)

	var resp TransactionResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Empty(t, resp.Errors)
	assert.NotNil(t, resp.Transaction)
	assert.Contains(t, resp.Commit, "/db/neo4j/tx/")
	assert.Contains(t, resp.Commit, "/commit")
}

func TestTransactionBegin_WithStatements(t *testing.T) {
	resource, mockGraph, mockDB := setupTestResource(t)

	mockDB.EXPECT().GetDisplayNodeGraphKinds(gomock.Any()).Return(map[graph.Kind]bool{}, nil)
	mockGraph.EXPECT().PrepareCypherQuery("RETURN 1", int64(queries.DefaultQueryFitnessLowerBoundExplore)).Return(queries.PreparedQuery{}, nil)
	mockGraph.EXPECT().RawCypherQuery(gomock.Any(), gomock.Any(), gomock.Any(), true).Return(model.UnifiedGraph{
		Nodes:    map[string]model.UnifiedNode{},
		Edges:    []model.UnifiedEdge{},
		Literals: graph.Literals{{Key: "1", Value: 1}},
	}, nil)

	body := TransactionRequest{
		Statements: []Statement{{Statement: "RETURN 1"}},
	}

	rr := makeRequest(t, resource.TransactionBegin, http.MethodPost, "/db/neo4j/tx", body, map[string]string{"databaseName": "neo4j"})

	assert.Equal(t, http.StatusCreated, rr.Code)

	var resp TransactionResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Empty(t, resp.Errors)
	assert.Len(t, resp.Results, 1)
	assert.NotNil(t, resp.Transaction)
}

func TestTransactionRollback(t *testing.T) {
	resource, _, _ := setupTestResource(t)

	tx := resource.TxManager.Begin()

	rr := makeRequest(t, resource.TransactionRollback, http.MethodDelete, fmt.Sprintf("/db/neo4j/tx/%d", tx.ID), nil, map[string]string{
		"databaseName": "neo4j",
		"txId":         fmt.Sprintf("%d", tx.ID),
	})

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp TransactionResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Empty(t, resp.Errors)
}

func TestTransactionRollback_NotFound(t *testing.T) {
	resource, _, _ := setupTestResource(t)

	rr := makeRequest(t, resource.TransactionRollback, http.MethodDelete, "/db/neo4j/tx/999", nil, map[string]string{
		"databaseName": "neo4j",
		"txId":         "999",
	})

	assert.Equal(t, http.StatusNotFound, rr.Code)

	var resp TransactionResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Errors, 1)
	assert.Equal(t, "Neo.ClientError.Transaction.TransactionNotFound", resp.Errors[0].Code)
}

func TestTransactionRollback_InvalidTxID(t *testing.T) {
	resource, _, _ := setupTestResource(t)

	rr := makeRequest(t, resource.TransactionRollback, http.MethodDelete, "/db/neo4j/tx/abc", nil, map[string]string{
		"databaseName": "neo4j",
		"txId":         "abc",
	})

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestTransactionRun(t *testing.T) {
	resource, mockGraph, mockDB := setupTestResource(t)

	tx := resource.TxManager.Begin()

	mockDB.EXPECT().GetDisplayNodeGraphKinds(gomock.Any()).Return(map[graph.Kind]bool{}, nil)
	mockGraph.EXPECT().PrepareCypherQuery("RETURN 2", int64(queries.DefaultQueryFitnessLowerBoundExplore)).Return(queries.PreparedQuery{}, nil)
	mockGraph.EXPECT().RawCypherQuery(gomock.Any(), gomock.Any(), gomock.Any(), true).Return(model.UnifiedGraph{
		Nodes:    map[string]model.UnifiedNode{},
		Edges:    []model.UnifiedEdge{},
		Literals: graph.Literals{{Key: "2", Value: 2}},
	}, nil)

	body := TransactionRequest{
		Statements: []Statement{{Statement: "RETURN 2"}},
	}

	rr := makeRequest(t, resource.TransactionRun, http.MethodPost, fmt.Sprintf("/db/neo4j/tx/%d", tx.ID), body, map[string]string{
		"databaseName": "neo4j",
		"txId":         fmt.Sprintf("%d", tx.ID),
	})

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp TransactionResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Empty(t, resp.Errors)
	assert.Len(t, resp.Results, 1)
	assert.NotNil(t, resp.Transaction)
}

func TestTransactionRun_NotFound(t *testing.T) {
	resource, _, _ := setupTestResource(t)

	body := TransactionRequest{
		Statements: []Statement{{Statement: "RETURN 1"}},
	}

	rr := makeRequest(t, resource.TransactionRun, http.MethodPost, "/db/neo4j/tx/999", body, map[string]string{
		"databaseName": "neo4j",
		"txId":         "999",
	})

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestTransactionCommitOpen(t *testing.T) {
	resource, mockGraph, mockDB := setupTestResource(t)

	tx := resource.TxManager.Begin()

	// Add initial results
	resource.TxManager.AddResults(tx.ID, []StatementResult{
		{Columns: []string{"a"}, Data: []RowResult{}},
	})

	// Execute final statement during commit
	mockDB.EXPECT().GetDisplayNodeGraphKinds(gomock.Any()).Return(map[graph.Kind]bool{}, nil)
	mockGraph.EXPECT().PrepareCypherQuery("RETURN 3", int64(queries.DefaultQueryFitnessLowerBoundExplore)).Return(queries.PreparedQuery{}, nil)
	mockGraph.EXPECT().RawCypherQuery(gomock.Any(), gomock.Any(), gomock.Any(), true).Return(model.UnifiedGraph{
		Nodes:    map[string]model.UnifiedNode{},
		Edges:    []model.UnifiedEdge{},
		Literals: graph.Literals{{Key: "3", Value: 3}},
	}, nil)

	body := TransactionRequest{
		Statements: []Statement{{Statement: "RETURN 3"}},
	}

	rr := makeRequest(t, resource.TransactionCommitOpen, http.MethodPost, fmt.Sprintf("/db/neo4j/tx/%d/commit", tx.ID), body, map[string]string{
		"databaseName": "neo4j",
		"txId":         fmt.Sprintf("%d", tx.ID),
	})

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp TransactionResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Empty(t, resp.Errors)
	// Should have both the initial result and the final statement result
	assert.Len(t, resp.Results, 2)

	// Transaction should be gone
	assert.Nil(t, resource.TxManager.Get(tx.ID))
}

func TestTransactionCommitOpen_NotFound(t *testing.T) {
	resource, _, _ := setupTestResource(t)

	rr := makeRequest(t, resource.TransactionCommitOpen, http.MethodPost, "/db/neo4j/tx/999/commit", nil, map[string]string{
		"databaseName": "neo4j",
		"txId":         "999",
	})

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestTransactionCommit_MultipleStatements(t *testing.T) {
	resource, mockGraph, mockDB := setupTestResource(t)

	mockDB.EXPECT().GetDisplayNodeGraphKinds(gomock.Any()).Return(map[graph.Kind]bool{}, nil)
	mockGraph.EXPECT().PrepareCypherQuery("RETURN 1", int64(queries.DefaultQueryFitnessLowerBoundExplore)).Return(queries.PreparedQuery{}, nil)
	mockGraph.EXPECT().RawCypherQuery(gomock.Any(), gomock.Any(), gomock.Any(), true).Return(model.UnifiedGraph{
		Nodes:    map[string]model.UnifiedNode{},
		Edges:    []model.UnifiedEdge{},
		Literals: graph.Literals{{Key: "1", Value: 1}},
	}, nil)
	mockGraph.EXPECT().PrepareCypherQuery("RETURN 2", int64(queries.DefaultQueryFitnessLowerBoundExplore)).Return(queries.PreparedQuery{}, nil)
	mockGraph.EXPECT().RawCypherQuery(gomock.Any(), gomock.Any(), gomock.Any(), true).Return(model.UnifiedGraph{
		Nodes:    map[string]model.UnifiedNode{},
		Edges:    []model.UnifiedEdge{},
		Literals: graph.Literals{{Key: "2", Value: 2}},
	}, nil)

	body := TransactionRequest{
		Statements: []Statement{
			{Statement: "RETURN 1"},
			{Statement: "RETURN 2"},
		},
	}

	rr := makeRequest(t, resource.TransactionCommit, http.MethodPost, "/db/neo4j/tx/commit", body, map[string]string{"databaseName": "neo4j"})

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp TransactionResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Empty(t, resp.Errors)
	assert.Len(t, resp.Results, 2)
}

func TestTransactionCommit_StopsOnError(t *testing.T) {
	resource, mockGraph, mockDB := setupTestResource(t)

	mockDB.EXPECT().GetDisplayNodeGraphKinds(gomock.Any()).Return(map[graph.Kind]bool{}, nil)
	mockGraph.EXPECT().PrepareCypherQuery("BAD QUERY", int64(queries.DefaultQueryFitnessLowerBoundExplore)).Return(queries.PreparedQuery{}, fmt.Errorf("syntax error"))

	body := TransactionRequest{
		Statements: []Statement{
			{Statement: "BAD QUERY"},
			{Statement: "RETURN 2"}, // Should not be reached
		},
	}

	rr := makeRequest(t, resource.TransactionCommit, http.MethodPost, "/db/neo4j/tx/commit", body, map[string]string{"databaseName": "neo4j"})

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp TransactionResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Len(t, resp.Errors, 1)
	assert.Empty(t, resp.Results)
}

func TestWriteNeo4jResponse_NilSlices(t *testing.T) {
	resource, _, _ := setupTestResource(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	resource.writeNeo4jResponse(rr, req, TransactionResponse{
		Results: nil,
		Errors:  nil,
	}, http.StatusOK)

	var resp TransactionResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	// Should be empty arrays, not null
	assert.NotNil(t, resp.Results)
	assert.NotNil(t, resp.Errors)
}
