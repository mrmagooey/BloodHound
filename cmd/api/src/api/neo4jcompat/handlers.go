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
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"github.com/specterops/bloodhound/cmd/api/src/api"
	"github.com/specterops/bloodhound/cmd/api/src/database"
	"github.com/specterops/bloodhound/cmd/api/src/queries"
)

// Neo4jResource holds the dependencies for the Neo4j HTTP API compatibility handlers.
type Neo4jResource struct {
	GraphQuery queries.Graph
	DB         database.Database
	TxManager  *TransactionManager
}

// NewNeo4jResource creates a new Neo4jResource with a fresh transaction manager.
func NewNeo4jResource(graphQuery queries.Graph, db database.Database) *Neo4jResource {
	return &Neo4jResource{
		GraphQuery: graphQuery,
		DB:         db,
		TxManager:  NewTransactionManager(),
	}
}

// TransactionCommit handles POST /db/{databaseName}/tx/commit
// This runs all statements in an auto-committed transaction and returns results.
func (r *Neo4jResource) TransactionCommit(response http.ResponseWriter, request *http.Request) {
	txReq, ok := r.readTransactionRequest(response, request)
	if !ok {
		return
	}

	results, errors := r.executeStatements(request, txReq.Statements)

	r.writeNeo4jResponse(response, request, TransactionResponse{
		Results: results,
		Errors:  errors,
	}, http.StatusOK)
}

// TransactionBegin handles POST /db/{databaseName}/tx
// This begins a new transaction, optionally runs statements, and returns a transaction ID.
func (r *Neo4jResource) TransactionBegin(response http.ResponseWriter, request *http.Request) {
	txReq, ok := r.readTransactionRequest(response, request)
	if !ok {
		return
	}

	tx := r.TxManager.Begin()

	var results []StatementResult
	var errors []Neo4jError

	if len(txReq.Statements) > 0 {
		results, errors = r.executeStatements(request, txReq.Statements)
		if err := r.TxManager.AddResults(tx.ID, results); err != nil {
			slog.ErrorContext(request.Context(), fmt.Sprintf("Failed to add results to transaction: %v", err))
		}
	}

	dbName := mux.Vars(request)["databaseName"]
	commitURL := fmt.Sprintf("/db/%s/tx/%d/commit", dbName, tx.ID)

	r.writeNeo4jResponse(response, request, TransactionResponse{
		Results: results,
		Errors:  errors,
		Commit:  commitURL,
		Transaction: &TransactionInfo{
			Expires: tx.ExpiresAt.Format(time.RFC3339),
		},
	}, http.StatusCreated)
}

// TransactionRun handles POST /db/{databaseName}/tx/{txId}
// This runs statements within an existing open transaction.
func (r *Neo4jResource) TransactionRun(response http.ResponseWriter, request *http.Request) {
	txID, ok := r.parseTxID(response, request)
	if !ok {
		return
	}

	tx := r.TxManager.Get(txID)
	if tx == nil {
		r.writeNeo4jResponse(response, request, TransactionResponse{
			Errors: []Neo4jError{{
				Code:    "Neo.ClientError.Transaction.TransactionNotFound",
				Message: fmt.Sprintf("transaction %d not found", txID),
			}},
		}, http.StatusNotFound)
		return
	}

	txReq, ok := r.readTransactionRequest(response, request)
	if !ok {
		return
	}

	results, errors := r.executeStatements(request, txReq.Statements)
	if addErr := r.TxManager.AddResults(txID, results); addErr != nil {
		slog.ErrorContext(request.Context(), fmt.Sprintf("Failed to add results to transaction: %v", addErr))
	}

	dbName := mux.Vars(request)["databaseName"]
	commitURL := fmt.Sprintf("/db/%s/tx/%d/commit", dbName, txID)

	r.writeNeo4jResponse(response, request, TransactionResponse{
		Results: results,
		Errors:  errors,
		Commit:  commitURL,
		Transaction: &TransactionInfo{
			Expires: tx.ExpiresAt.Format(time.RFC3339),
		},
	}, http.StatusOK)
}

// TransactionCommitOpen handles POST /db/{databaseName}/tx/{txId}/commit
// This commits an open transaction, running any final statements first.
func (r *Neo4jResource) TransactionCommitOpen(response http.ResponseWriter, request *http.Request) {
	txID, ok := r.parseTxID(response, request)
	if !ok {
		return
	}

	tx := r.TxManager.Get(txID)
	if tx == nil {
		r.writeNeo4jResponse(response, request, TransactionResponse{
			Errors: []Neo4jError{{
				Code:    "Neo.ClientError.Transaction.TransactionNotFound",
				Message: fmt.Sprintf("transaction %d not found", txID),
			}},
		}, http.StatusNotFound)
		return
	}

	txReq, ok := r.readTransactionRequest(response, request)
	if !ok {
		return
	}

	// Execute any final statements
	if len(txReq.Statements) > 0 {
		results, _ := r.executeStatements(request, txReq.Statements)
		if addErr := r.TxManager.AddResults(txID, results); addErr != nil {
			slog.ErrorContext(request.Context(), fmt.Sprintf("Failed to add results to transaction: %v", addErr))
		}
	}

	// Commit and return all accumulated results
	allResults, err := r.TxManager.Commit(txID)
	if err != nil {
		r.writeNeo4jResponse(response, request, TransactionResponse{
			Errors: []Neo4jError{{
				Code:    "Neo.ClientError.Transaction.TransactionNotFound",
				Message: err.Error(),
			}},
		}, http.StatusNotFound)
		return
	}

	r.writeNeo4jResponse(response, request, TransactionResponse{
		Results: allResults,
		Errors:  []Neo4jError{},
	}, http.StatusOK)
}

// TransactionRollback handles DELETE /db/{databaseName}/tx/{txId}
// This rolls back an open transaction.
func (r *Neo4jResource) TransactionRollback(response http.ResponseWriter, request *http.Request) {
	txID, ok := r.parseTxID(response, request)
	if !ok {
		return
	}

	if err := r.TxManager.Rollback(txID); err != nil {
		r.writeNeo4jResponse(response, request, TransactionResponse{
			Errors: []Neo4jError{{
				Code:    "Neo.ClientError.Transaction.TransactionNotFound",
				Message: err.Error(),
			}},
		}, http.StatusNotFound)
		return
	}

	r.writeNeo4jResponse(response, request, TransactionResponse{
		Results: []StatementResult{},
		Errors:  []Neo4jError{},
	}, http.StatusOK)
}

// executeStatements runs a list of Cypher statements through BloodHound's graph query layer.
func (r *Neo4jResource) executeStatements(request *http.Request, statements []Statement) ([]StatementResult, []Neo4jError) {
	results := make([]StatementResult, 0, len(statements))
	errors := make([]Neo4jError, 0)

	if len(statements) == 0 {
		return results, errors
	}

	validPrimaryKinds, err := r.DB.GetDisplayNodeGraphKinds(request.Context())
	if err != nil {
		return results, []Neo4jError{{
			Code:    "Neo.DatabaseError.General.UnknownError",
			Message: fmt.Sprintf("failed to get display node kinds: %v", err),
		}}
	}

	for _, stmt := range statements {
		preparedQuery, err := r.GraphQuery.PrepareCypherQuery(stmt.Statement, queries.DefaultQueryFitnessLowerBoundExplore)
		if err != nil {
			errors = append(errors, Neo4jError{
				Code:    "Neo.ClientError.Statement.SyntaxError",
				Message: err.Error(),
			})
			// Neo4j stops executing on first error
			break
		}

		graphResponse, err := r.GraphQuery.RawCypherQuery(request.Context(), validPrimaryKinds, preparedQuery, true)
		if err != nil {
			errors = append(errors, Neo4jError{
				Code:    "Neo.DatabaseError.Statement.ExecutionFailed",
				Message: err.Error(),
			})
			break
		}

		results = append(results, ConvertUnifiedGraphToNeo4jResult(graphResponse))
	}

	return results, errors
}

// readTransactionRequest reads and parses the JSON request body.
func (r *Neo4jResource) readTransactionRequest(response http.ResponseWriter, request *http.Request) (TransactionRequest, bool) {
	var txReq TransactionRequest

	if request.Body == nil || request.ContentLength == 0 {
		// Empty body is valid for begin/rollback
		return TransactionRequest{Statements: []Statement{}}, true
	}

	if err := api.ReadJSONRequestPayloadLimited(&txReq, request); err != nil {
		r.writeNeo4jResponse(response, request, TransactionResponse{
			Errors: []Neo4jError{{
				Code:    "Neo.ClientError.Request.InvalidFormat",
				Message: "invalid JSON request body",
			}},
		}, http.StatusBadRequest)
		return txReq, false
	}

	return txReq, true
}

// parseTxID extracts and validates the transaction ID from the URL path.
func (r *Neo4jResource) parseTxID(response http.ResponseWriter, request *http.Request) (int64, bool) {
	vars := mux.Vars(request)
	txIDStr := vars["txId"]

	txID, err := strconv.ParseInt(txIDStr, 10, 64)
	if err != nil {
		r.writeNeo4jResponse(response, request, TransactionResponse{
			Errors: []Neo4jError{{
				Code:    "Neo.ClientError.Request.Invalid",
				Message: fmt.Sprintf("invalid transaction ID: %s", txIDStr),
			}},
		}, http.StatusBadRequest)
		return 0, false
	}

	return txID, true
}

// writeNeo4jResponse writes a Neo4j-formatted JSON response.
func (r *Neo4jResource) writeNeo4jResponse(response http.ResponseWriter, request *http.Request, body TransactionResponse, statusCode int) {
	response.Header().Set("Content-Type", "application/json")

	// Ensure non-nil slices for JSON serialization
	if body.Results == nil {
		body.Results = []StatementResult{}
	}
	if body.Errors == nil {
		body.Errors = []Neo4jError{}
	}

	content, err := json.Marshal(body)
	if err != nil {
		slog.ErrorContext(request.Context(), fmt.Sprintf("Failed to marshal Neo4j response: %v", err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}

	response.WriteHeader(statusCode)
	if _, err := response.Write(content); err != nil {
		slog.ErrorContext(request.Context(), fmt.Sprintf("Failed to write Neo4j response: %v", err))
	}
}
