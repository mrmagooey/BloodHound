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

//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/specterops/bloodhound/cmd/api/src/api/neo4jcompat"
	"github.com/specterops/bloodhound/cmd/api/src/config"
	dbmocks "github.com/specterops/bloodhound/cmd/api/src/database/mocks"
	"github.com/specterops/bloodhound/cmd/api/src/queries"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/specterops/bloodhound/packages/go/cache"
)

// newCompatResource constructs a Neo4jResource backed by a real kglite graph.
// database.Database is mocked to return an empty display-kinds map, which is
// sufficient for the Cypher query path.
func newCompatResource(t *testing.T) *neo4jcompat.Neo4jResource {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{
		EnableCypherMutations: true,
	})

	return neo4jcompat.NewNeo4jResource(gq, mockDB)
}

// compatPost sends a POST request with the given body through the named handler,
// injecting mux route variables for databaseName. Returns the recorded response.
func compatPost(t *testing.T, handler http.HandlerFunc, path string, body any, vars map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if vars != nil {
		req = mux.SetURLVars(req, vars)
	}
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

// compatDelete sends a DELETE request through the named handler with mux vars.
func compatDelete(t *testing.T, handler http.HandlerFunc, path string, vars map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	if vars != nil {
		req = mux.SetURLVars(req, vars)
	}
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

// decodeCompatResponse unmarshals a JSON body into a TransactionResponse.
func decodeCompatResponse(t *testing.T, rr *httptest.ResponseRecorder) neo4jcompat.TransactionResponse {
	t.Helper()
	var resp neo4jcompat.TransactionResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp), "response body: %s", rr.Body.String())
	return resp
}

// ── single-statement auto-commit ──────────────────────────────────────────────

// TestHTTPCompat_SingleStatement sends a single Cypher query via
// POST /db/neo4j/tx/commit and verifies the happy-path response shape.
func TestHTTPCompat_SingleStatement(t *testing.T) {
	r := newCompatResource(t)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "MATCH (n) RETURN count(n) AS c"},
		},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	assert.Empty(t, resp.Errors, "unexpected errors: %v", resp.Errors)
	assert.Len(t, resp.Results, 1)
}

// TestHTTPCompat_EmptyStatement sends an empty statement list (valid per Neo4j spec).
func TestHTTPCompat_EmptyStatement(t *testing.T) {
	r := newCompatResource(t)

	body := neo4jcompat.TransactionRequest{Statements: []neo4jcompat.Statement{}}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	assert.Empty(t, resp.Errors)
	assert.Empty(t, resp.Results)
}

// TestHTTPCompat_MultipleStatements sends two statements in one request and
// verifies both are executed and returned.
func TestHTTPCompat_MultipleStatements(t *testing.T) {
	r := newCompatResource(t)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "MATCH (n) RETURN count(n) AS total"},
			{Statement: "MATCH ()-[r]->() RETURN count(r) AS rels"},
		},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	assert.Empty(t, resp.Errors)
	assert.Len(t, resp.Results, 2)
}

// TestHTTPCompat_ResultStructure verifies that the result has the expected
// columns / data / meta shape that Neo4j clients rely on.
func TestHTTPCompat_ResultStructure(t *testing.T) {
	r := newCompatResource(t)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "MATCH (n) RETURN count(n) AS c"},
		},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	require.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	require.Len(t, resp.Results, 1)

	result := resp.Results[0]
	assert.NotNil(t, result.Columns, "columns must not be nil")
	assert.NotNil(t, result.Data, "data must not be nil")
}

// TestHTTPCompat_SyntaxError verifies that an invalid Cypher query returns an
// error in the response body (not an HTTP error status) per the Neo4j spec.
func TestHTTPCompat_SyntaxError(t *testing.T) {
	r := newCompatResource(t)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "THIS IS NOT VALID CYPHER !!!"},
		},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	require.NotEmpty(t, resp.Errors, "expected at least one error for invalid Cypher")
	assert.NotEmpty(t, resp.Errors[0].Code)
	assert.NotEmpty(t, resp.Errors[0].Message)
}

// TestHTTPCompat_StopsOnFirstError verifies that execution stops after the
// first failing statement.
func TestHTTPCompat_StopsOnFirstError(t *testing.T) {
	r := newCompatResource(t)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "NOT VALID CYPHER"},
			{Statement: "MATCH (n) RETURN count(n) AS c"}, // should not execute
		},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	assert.NotEmpty(t, resp.Errors)
	// Only the error result, no successful results
	assert.Empty(t, resp.Results)
}

// ── node query ───────────────────────────────────────────────────────────────

// TestHTTPCompat_NodeQuery seeds the graph, then runs a node-returning query
// through the HTTP endpoint and checks the columns.
func TestHTTPCompat_NodeQuery(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	r := neo4jcompat.NewNeo4jResource(gq, mockDB)

	// Seed the graph
	setupCypherTestGraph(t.Context(), t, graphDB)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "MATCH (n:CypherTest) RETURN n"},
		},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	require.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	assert.Empty(t, resp.Errors)
	require.Len(t, resp.Results, 1)
	// Node query returns "n" column
	assert.Equal(t, []string{"n"}, resp.Results[0].Columns)
	// Should have 3 rows (Alice, Bob, Charlie)
	assert.Len(t, resp.Results[0].Data, 3)
}

// TestHTTPCompat_CountQuery verifies that a count literal is returned correctly.
func TestHTTPCompat_CountQuery(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	r := neo4jcompat.NewNeo4jResource(gq, mockDB)

	setupCypherTestGraph(t.Context(), t, graphDB)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "MATCH (n:CypherTest) RETURN count(n) AS c"},
		},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	require.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	assert.Empty(t, resp.Errors)
	require.Len(t, resp.Results, 1)

	result := resp.Results[0]
	require.Contains(t, result.Columns, "c")
	require.NotEmpty(t, result.Data)
}

// TestHTTPCompat_ContentTypeHeader verifies that responses include the correct
// Content-Type header.
func TestHTTPCompat_ContentTypeHeader(t *testing.T) {
	r := newCompatResource(t)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "MATCH (n) RETURN count(n) AS c"},
		},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
}

// ── multi-statement transaction flow ─────────────────────────────────────────

// TestHTTPCompat_TransactionBeginRunCommit exercises the full
// BEGIN → RUN → COMMIT flow of the open-transaction API.
func TestHTTPCompat_TransactionBeginRunCommit(t *testing.T) {
	r := newCompatResource(t)
	dbName := "neo4j"

	// Step 1: BEGIN (no statements)
	beginRR := compatPost(t, r.TransactionBegin, "/db/neo4j/tx", nil,
		map[string]string{"databaseName": dbName})
	require.Equal(t, http.StatusCreated, beginRR.Code)
	beginResp := decodeCompatResponse(t, beginRR)
	require.Empty(t, beginResp.Errors)
	require.NotNil(t, beginResp.Transaction)
	require.NotEmpty(t, beginResp.Commit)

	// Extract txId from the commit URL: "/db/neo4j/tx/{txId}/commit"
	var txID int64
	_, err := fmt.Sscanf(beginResp.Commit, "/db/neo4j/tx/%d/commit", &txID)
	require.NoError(t, err, "failed to parse txId from commit URL %q", beginResp.Commit)

	// Step 2: RUN a statement inside the open transaction
	runBody := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "MATCH (n) RETURN count(n) AS c"},
		},
	}
	runRR := compatPost(t, r.TransactionRun,
		fmt.Sprintf("/db/%s/tx/%d", dbName, txID),
		runBody,
		map[string]string{"databaseName": dbName, "txId": fmt.Sprintf("%d", txID)})
	require.Equal(t, http.StatusOK, runRR.Code)
	runResp := decodeCompatResponse(t, runRR)
	assert.Empty(t, runResp.Errors)
	assert.Len(t, runResp.Results, 1)

	// Step 3: COMMIT the open transaction
	commitRR := compatPost(t, r.TransactionCommitOpen,
		fmt.Sprintf("/db/%s/tx/%d/commit", dbName, txID),
		neo4jcompat.TransactionRequest{Statements: []neo4jcompat.Statement{}},
		map[string]string{"databaseName": dbName, "txId": fmt.Sprintf("%d", txID)})
	require.Equal(t, http.StatusOK, commitRR.Code)
	commitResp := decodeCompatResponse(t, commitRR)
	assert.Empty(t, commitResp.Errors)
	// Accumulated results from the RUN step should be present
	assert.NotEmpty(t, commitResp.Results)
}

// TestHTTPCompat_TransactionRollback exercises BEGIN → ROLLBACK and verifies
// the transaction is gone afterwards.
func TestHTTPCompat_TransactionRollback(t *testing.T) {
	r := newCompatResource(t)
	dbName := "neo4j"

	// BEGIN
	beginRR := compatPost(t, r.TransactionBegin, "/db/neo4j/tx", nil,
		map[string]string{"databaseName": dbName})
	require.Equal(t, http.StatusCreated, beginRR.Code)
	beginResp := decodeCompatResponse(t, beginRR)
	require.NotNil(t, beginResp.Transaction)

	var txID int64
	_, err := fmt.Sscanf(beginResp.Commit, "/db/neo4j/tx/%d/commit", &txID)
	require.NoError(t, err)

	// ROLLBACK
	rollbackRR := compatDelete(t, r.TransactionRollback,
		fmt.Sprintf("/db/%s/tx/%d", dbName, txID),
		map[string]string{"databaseName": dbName, "txId": fmt.Sprintf("%d", txID)})
	require.Equal(t, http.StatusOK, rollbackRR.Code)
	rollbackResp := decodeCompatResponse(t, rollbackRR)
	assert.Empty(t, rollbackResp.Errors)

	// Attempting to run against the now-rolled-back transaction should 404
	runBody := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{{Statement: "MATCH (n) RETURN n"}},
	}
	runRR := compatPost(t, r.TransactionRun,
		fmt.Sprintf("/db/%s/tx/%d", dbName, txID),
		runBody,
		map[string]string{"databaseName": dbName, "txId": fmt.Sprintf("%d", txID)})
	assert.Equal(t, http.StatusNotFound, runRR.Code)
}

// TestHTTPCompat_CommitNotFound verifies that committing a non-existent txId
// returns 404 with the expected error code.
func TestHTTPCompat_CommitNotFound(t *testing.T) {
	r := newCompatResource(t)

	rr := compatPost(t, r.TransactionCommitOpen, "/db/neo4j/tx/99999/commit", nil,
		map[string]string{"databaseName": "neo4j", "txId": "99999"})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	resp := decodeCompatResponse(t, rr)
	require.NotEmpty(t, resp.Errors)
	assert.Equal(t, "Neo.ClientError.Transaction.TransactionNotFound", resp.Errors[0].Code)
}

// TestHTTPCompat_RollbackNotFound verifies that rolling back a non-existent txId
// returns 404.
func TestHTTPCompat_RollbackNotFound(t *testing.T) {
	r := newCompatResource(t)

	rr := compatDelete(t, r.TransactionRollback, "/db/neo4j/tx/99999",
		map[string]string{"databaseName": "neo4j", "txId": "99999"})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// TestHTTPCompat_RunNotFound verifies that running statements in a non-existent
// transaction returns 404.
func TestHTTPCompat_RunNotFound(t *testing.T) {
	r := newCompatResource(t)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{{Statement: "MATCH (n) RETURN n"}},
	}
	rr := compatPost(t, r.TransactionRun, "/db/neo4j/tx/99999", body,
		map[string]string{"databaseName": "neo4j", "txId": "99999"})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// TestHTTPCompat_EmptyBody exercises the empty/nil-body path (valid for begin/rollback).
func TestHTTPCompat_EmptyBody(t *testing.T) {
	r := newCompatResource(t)

	req := httptest.NewRequest(http.MethodPost, "/db/neo4j/tx/commit", nil)
	req = mux.SetURLVars(req, map[string]string{"databaseName": "neo4j"})
	rr := httptest.NewRecorder()
	r.TransactionCommit(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	assert.Empty(t, resp.Errors)
	assert.Empty(t, resp.Results)
}

// ── richer WHERE / WITH / ORDER BY / DISTINCT / functions / CASE ─────────────

// TestHTTPCompat_WhereFiltersExtended covers WHERE operators not tested in the
// basic WhereFilters test: <>, AND, OR, ENDS WITH, CONTAINS.
func TestHTTPCompat_WhereFiltersExtended(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	r := neo4jcompat.NewNeo4jResource(gq, mockDB)

	setupCypherTestGraph(t.Context(), t, graphDB)

	runCount := func(t *testing.T, cypher string, wantCount float64) {
		t.Helper()
		body := neo4jcompat.TransactionRequest{
			Statements: []neo4jcompat.Statement{{Statement: cypher}},
		}
		rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
			map[string]string{"databaseName": "neo4j"})
		require.Equal(t, http.StatusOK, rr.Code)
		resp := decodeCompatResponse(t, rr)
		assert.Empty(t, resp.Errors, "query %q returned errors: %v", cypher, resp.Errors)
		require.Len(t, resp.Results, 1)
		require.NotEmpty(t, resp.Results[0].Data)
		got := resp.Results[0].Data[0].Row[0]
		assert.EqualValues(t, wantCount, got, "count mismatch for %q", cypher)
	}

	t.Run("not_equals", func(t *testing.T) {
		runCount(t, "MATCH (n:CypherTest) WHERE n.name <> 'Alice' RETURN count(n) AS c", 2)
	})
	t.Run("AND", func(t *testing.T) {
		runCount(t, "MATCH (n:CypherTest) WHERE n.enabled = true AND n.score > 100 RETURN count(n) AS c", 1)
	})
	t.Run("OR", func(t *testing.T) {
		runCount(t, "MATCH (n:CypherTest) WHERE n.name = 'Alice' OR n.name = 'Bob' RETURN count(n) AS c", 2)
	})
	t.Run("ends_with", func(t *testing.T) {
		runCount(t, "MATCH (n:CypherTest) WHERE n.objectid ENDS WITH '-3' RETURN count(n) AS c", 1)
	})
	t.Run("contains", func(t *testing.T) {
		runCount(t, "MATCH (n:CypherTest) WHERE n.name CONTAINS 'ob' RETURN count(n) AS c", 1)
	})
}

// TestHTTPCompat_WithClause verifies that the WITH clause works as a pipeline
// filter via the HTTP endpoint.
func TestHTTPCompat_WithClause(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	r := neo4jcompat.NewNeo4jResource(gq, mockDB)

	setupCypherTestGraph(t.Context(), t, graphDB)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "MATCH (n:CypherTest) WITH n WHERE n.enabled = true RETURN count(n) AS c"},
		},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	require.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	assert.Empty(t, resp.Errors)
	require.Len(t, resp.Results, 1)
	require.NotEmpty(t, resp.Results[0].Data)
	assert.EqualValues(t, 2, resp.Results[0].Data[0].Row[0])
}

// TestHTTPCompat_OrderBy verifies that ORDER BY returns rows in the correct
// lexicographic order via the HTTP endpoint.
func TestHTTPCompat_OrderBy(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	r := neo4jcompat.NewNeo4jResource(gq, mockDB)

	setupCypherTestGraph(t.Context(), t, graphDB)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "MATCH (n:CypherTest) RETURN n.name AS name ORDER BY n.name"},
		},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	require.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	assert.Empty(t, resp.Errors)
	require.Len(t, resp.Results, 1)
	result := resp.Results[0]
	require.Len(t, result.Data, 3)
	assert.EqualValues(t, "Alice", result.Data[0].Row[0])
	assert.EqualValues(t, "Bob", result.Data[1].Row[0])
	assert.EqualValues(t, "Charlie", result.Data[2].Row[0])
}

// TestHTTPCompat_Distinct verifies that count(DISTINCT ...) returns the correct
// number of unique values via the HTTP endpoint.
func TestHTTPCompat_Distinct(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	r := neo4jcompat.NewNeo4jResource(gq, mockDB)

	setupCypherTestGraph(t.Context(), t, graphDB)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "MATCH (n:CypherTest) RETURN count(DISTINCT n.enabled) AS c"},
		},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	require.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	assert.Empty(t, resp.Errors)
	require.Len(t, resp.Results, 1)
	require.NotEmpty(t, resp.Results[0].Data)
	// Alice & Charlie have enabled=true; Bob has enabled=false → 2 distinct values
	assert.EqualValues(t, 2, resp.Results[0].Data[0].Row[0])
}

// TestHTTPCompat_CypherFunctions tests Cypher built-in functions (type, coalesce,
// toLower, toUpper, labels) via the HTTP endpoint.
func TestHTTPCompat_CypherFunctions(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	r := neo4jcompat.NewNeo4jResource(gq, mockDB)

	setupCypherTestGraph(t.Context(), t, graphDB)

	post := func(t *testing.T, cypher string) neo4jcompat.TransactionResponse {
		t.Helper()
		body := neo4jcompat.TransactionRequest{
			Statements: []neo4jcompat.Statement{{Statement: cypher}},
		}
		rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
			map[string]string{"databaseName": "neo4j"})
		require.Equal(t, http.StatusOK, rr.Code)
		resp := decodeCompatResponse(t, rr)
		assert.Empty(t, resp.Errors, "query %q errors: %v", cypher, resp.Errors)
		return resp
	}

	t.Run("type", func(t *testing.T) {
		resp := post(t, "MATCH ()-[r:CypherEdge]->() RETURN type(r) AS t LIMIT 1")
		require.Len(t, resp.Results, 1)
		require.NotEmpty(t, resp.Results[0].Data)
		val, _ := resp.Results[0].Data[0].Row[0].(string)
		assert.Equal(t, "CypherEdge", val)
	})

	t.Run("coalesce", func(t *testing.T) {
		resp := post(t, "MATCH (n:CypherTest) WHERE n.name = 'Alice' RETURN coalesce(n.name, 'unknown') AS name")
		require.Len(t, resp.Results, 1)
		require.NotEmpty(t, resp.Results[0].Data)
		assert.EqualValues(t, "Alice", resp.Results[0].Data[0].Row[0])
	})

	t.Run("toLower", func(t *testing.T) {
		resp := post(t, "MATCH (n:CypherTest) WHERE n.name = 'Alice' RETURN toLower(n.name) AS l")
		require.Len(t, resp.Results, 1)
		require.NotEmpty(t, resp.Results[0].Data)
		assert.EqualValues(t, "alice", resp.Results[0].Data[0].Row[0])
	})

	t.Run("toUpper", func(t *testing.T) {
		resp := post(t, "MATCH (n:CypherTest) WHERE n.name = 'Alice' RETURN toUpper(n.name) AS u")
		require.Len(t, resp.Results, 1)
		require.NotEmpty(t, resp.Results[0].Data)
		assert.EqualValues(t, "ALICE", resp.Results[0].Data[0].Row[0])
	})

	t.Run("labels", func(t *testing.T) {
		resp := post(t, "MATCH (n:CypherTest) WHERE n.name = 'Alice' RETURN labels(n) AS l")
		require.Len(t, resp.Results, 1)
		require.NotEmpty(t, resp.Results[0].Data)
		// labels() may come back as a []interface{} or a string — just check it contains "CypherTest"
		labelsVal := fmt.Sprintf("%v", resp.Results[0].Data[0].Row[0])
		assert.Contains(t, labelsVal, "CypherTest")
	})
}

// TestHTTPCompat_CaseExpression verifies the CASE WHEN … THEN … ELSE … END
// expression via the HTTP endpoint.
func TestHTTPCompat_CaseExpression(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	r := neo4jcompat.NewNeo4jResource(gq, mockDB)

	setupCypherTestGraph(t.Context(), t, graphDB)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "MATCH (n:CypherTest) WHERE n.name = 'Alice' RETURN CASE WHEN n.enabled = true THEN 'active' ELSE 'inactive' END AS status"},
		},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	require.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	assert.Empty(t, resp.Errors)
	require.Len(t, resp.Results, 1)
	require.NotEmpty(t, resp.Results[0].Data)
	assert.EqualValues(t, "active", resp.Results[0].Data[0].Row[0])
}

// ── variable-length paths, pattern match, pipe types, label-check, path queries ─

// TestHTTPCompat_VariableLengthPaths tests Cypher variable-length path patterns
// via the HTTP endpoint.
func TestHTTPCompat_VariableLengthPaths(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	r := neo4jcompat.NewNeo4jResource(gq, mockDB)

	setupCypherTestGraph(t.Context(), t, graphDB) // Alice->Bob->Charlie

	runCount := func(t *testing.T, cypher string, wantCount float64) {
		t.Helper()
		body := neo4jcompat.TransactionRequest{
			Statements: []neo4jcompat.Statement{{Statement: cypher}},
		}
		rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
			map[string]string{"databaseName": "neo4j"})
		require.Equal(t, http.StatusOK, rr.Code)
		resp := decodeCompatResponse(t, rr)
		assert.Empty(t, resp.Errors, "query %q errors: %v", cypher, resp.Errors)
		require.Len(t, resp.Results, 1)
		require.NotEmpty(t, resp.Results[0].Data)
		assert.EqualValues(t, wantCount, resp.Results[0].Data[0].Row[0], "count mismatch for %q", cypher)
	}

	t.Run("unbounded", func(t *testing.T) {
		runCount(t, "MATCH (a:CypherTest)-[:CypherEdge*1..]->(x) WHERE a.name = 'Alice' RETURN count(x) AS c", 2)
	})
	t.Run("exact_length", func(t *testing.T) {
		runCount(t, "MATCH (a:CypherTest)-[:CypherEdge*2]->(x) WHERE a.name = 'Alice' RETURN count(x) AS c", 1)
	})
	t.Run("bounded", func(t *testing.T) {
		runCount(t, "MATCH (a:CypherTest)-[:CypherEdge*1..1]->(x) WHERE a.name = 'Alice' RETURN count(x) AS c", 1)
	})
}

// TestHTTPCompat_PatternMatchWithRelationships verifies that a pattern with an
// explicit relationship type returns the correct count via the HTTP endpoint.
func TestHTTPCompat_PatternMatchWithRelationships(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	r := neo4jcompat.NewNeo4jResource(gq, mockDB)

	setupCypherTestGraph(t.Context(), t, graphDB)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "MATCH (a:CypherTest)-[:CypherEdge]->(b:CypherTest) RETURN count(*) AS c"},
		},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	require.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	assert.Empty(t, resp.Errors)
	require.Len(t, resp.Results, 1)
	require.NotEmpty(t, resp.Results[0].Data)
	assert.EqualValues(t, 2, resp.Results[0].Data[0].Row[0])
}

// TestHTTPCompat_PipeSeparatedTypes verifies that pipe-separated relationship
// types (e.g. :TypeA|TypeB) work via the HTTP endpoint.
func TestHTTPCompat_PipeSeparatedTypes(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	r := neo4jcompat.NewNeo4jResource(gq, mockDB)

	// Create 3 nodes: ids[0] -> ids[1] via TypeA, ids[0] -> ids[2] via TypeB
	ids := createNodes(t.Context(), t, graphDB, cypherNodeKind, 3)
	err := graphDB.WriteTransaction(t.Context(), func(tx graph.Transaction) error {
		if _, err := tx.CreateRelationshipByIDs(ids[0], ids[1], graph.StringKind("TypeA"), graph.NewProperties()); err != nil {
			return err
		}
		if _, err := tx.CreateRelationshipByIDs(ids[0], ids[2], graph.StringKind("TypeB"), graph.NewProperties()); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)

	cypher := fmt.Sprintf("MATCH (a)-[:TypeA|TypeB]->(x) WHERE id(a) = %d RETURN count(x) AS c", ids[0])
	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{{Statement: cypher}},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	require.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	assert.Empty(t, resp.Errors)
	require.Len(t, resp.Results, 1)
	require.NotEmpty(t, resp.Results[0].Data)
	assert.EqualValues(t, 2, resp.Results[0].Data[0].Row[0])
}

// TestHTTPCompat_LabelCheckInWhere verifies the WHERE n:Label syntax
// via the HTTP endpoint.
func TestHTTPCompat_LabelCheckInWhere(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	r := neo4jcompat.NewNeo4jResource(gq, mockDB)

	setupCypherTestGraph(t.Context(), t, graphDB)

	body := neo4jcompat.TransactionRequest{
		Statements: []neo4jcompat.Statement{
			{Statement: "MATCH (n) WHERE n:CypherTest RETURN count(n) AS c"},
		},
	}
	rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
		map[string]string{"databaseName": "neo4j"})

	require.Equal(t, http.StatusOK, rr.Code)
	resp := decodeCompatResponse(t, rr)
	assert.Empty(t, resp.Errors)
	require.Len(t, resp.Results, 1)
	require.NotEmpty(t, resp.Results[0].Data)
	assert.EqualValues(t, 3, resp.Results[0].Data[0].Row[0])
}

// TestHTTPCompat_PathQueries tests shortestPath, allShortestPaths, and bounded
// variable-length path queries via the HTTP endpoint.
func TestHTTPCompat_PathQueries(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	r := neo4jcompat.NewNeo4jResource(gq, mockDB)

	// Create a 3-node chain: ids[0] -> ids[1] -> ids[2] via TestEdge
	ids := createChain(t.Context(), t, graphDB, graph.StringKind("TestNode"), graph.StringKind("TestEdge"), 3)

	post := func(t *testing.T, cypher string) neo4jcompat.TransactionResponse {
		t.Helper()
		body := neo4jcompat.TransactionRequest{
			Statements: []neo4jcompat.Statement{{Statement: cypher}},
		}
		rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
			map[string]string{"databaseName": "neo4j"})
		require.Equal(t, http.StatusOK, rr.Code)
		resp := decodeCompatResponse(t, rr)
		assert.Empty(t, resp.Errors, "query %q errors: %v", cypher, resp.Errors)
		return resp
	}

	t.Run("shortestPath", func(t *testing.T) {
		cypher := fmt.Sprintf(
			"MATCH p = shortestPath((a)-[*1..10]->(c)) WHERE id(a) = %d AND id(c) = %d RETURN p",
			ids[0], ids[2])
		resp := post(t, cypher)
		require.Len(t, resp.Results, 1)
		assert.NotEmpty(t, resp.Results[0].Data, "shortestPath should return at least 1 row")
	})

	t.Run("allShortestPaths", func(t *testing.T) {
		cypher := fmt.Sprintf(
			"MATCH p = allShortestPaths((a)-[*1..10]->(c)) WHERE id(a) = %d AND id(c) = %d RETURN p",
			ids[0], ids[2])
		resp := post(t, cypher)
		require.Len(t, resp.Results, 1)
		assert.NotEmpty(t, resp.Results[0].Data, "allShortestPaths should return at least 1 row")
	})

	t.Run("variable_length_bounded", func(t *testing.T) {
		cypher := fmt.Sprintf(
			"MATCH (a)-[:TestEdge*1..2]->(x) WHERE id(a) = %d RETURN count(x) AS c",
			ids[0])
		resp := post(t, cypher)
		require.Len(t, resp.Results, 1)
		require.NotEmpty(t, resp.Results[0].Data)
		assert.EqualValues(t, 2, resp.Results[0].Data[0].Row[0])
	})
}

// TestHTTPCompat_WhereFilters seeds the graph and sends various WHERE clause
// queries through the HTTP endpoint to verify filter support.
func TestHTTPCompat_WhereFilters(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := dbmocks.NewMockDatabase(ctrl)
	mockDB.EXPECT().
		GetDisplayNodeGraphKinds(gomock.Any()).
		Return(map[graph.Kind]bool{}, nil).
		AnyTimes()

	graphDB := openGraph(t)
	gq := queries.NewGraphQuery(graphDB, cache.Cache{}, config.Configuration{EnableCypherMutations: true})
	r := neo4jcompat.NewNeo4jResource(gq, mockDB)

	setupCypherTestGraph(t.Context(), t, graphDB)

	queries := []struct {
		name  string
		cypher string
	}{
		{"equals", "MATCH (n:CypherTest) WHERE n.name = 'Alice' RETURN count(n) AS c"},
		{"greater_than", "MATCH (n:CypherTest) WHERE n.score > 100 RETURN count(n) AS c"},
		{"boolean", "MATCH (n:CypherTest) WHERE n.enabled = true RETURN count(n) AS c"},
		{"starts_with", "MATCH (n:CypherTest) WHERE n.name STARTS WITH 'Al' RETURN count(n) AS c"},
		{"in_list", "MATCH (n:CypherTest) WHERE n.name IN ['Alice', 'Charlie'] RETURN count(n) AS c"},
	}

	for _, q := range queries {
		t.Run(q.name, func(t *testing.T) {
			body := neo4jcompat.TransactionRequest{
				Statements: []neo4jcompat.Statement{{Statement: q.cypher}},
			}
			rr := compatPost(t, r.TransactionCommit, "/db/neo4j/tx/commit", body,
				map[string]string{"databaseName": "neo4j"})

			require.Equal(t, http.StatusOK, rr.Code)
			resp := decodeCompatResponse(t, rr)
			assert.Empty(t, resp.Errors, "query %q returned errors: %v", q.cypher, resp.Errors)
			assert.Len(t, resp.Results, 1)
		})
	}
}
