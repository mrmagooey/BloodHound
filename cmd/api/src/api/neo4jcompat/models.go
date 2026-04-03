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

// Neo4j HTTP API request/response models.
// See: https://neo4j.com/docs/http-api/current/actions/

// Request models

type TransactionRequest struct {
	Statements []Statement `json:"statements"`
}

type Statement struct {
	Statement  string         `json:"statement"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// Response models

type TransactionResponse struct {
	Results     []StatementResult `json:"results"`
	Errors      []Neo4jError      `json:"errors"`
	Transaction *TransactionInfo  `json:"transaction,omitempty"`
	Commit      string            `json:"commit,omitempty"`
}

type StatementResult struct {
	Columns []string    `json:"columns"`
	Data    []RowResult `json:"data"`
}

type RowResult struct {
	Row  []any      `json:"row"`
	Meta []RowMeta  `json:"meta"`
}

type RowMeta struct {
	ID        int64  `json:"id"`
	ElementID string `json:"elementId"`
	Type      string `json:"type"`
	Deleted   bool   `json:"deleted"`
}

type Neo4jError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type TransactionInfo struct {
	Expires string `json:"expires"`
}
