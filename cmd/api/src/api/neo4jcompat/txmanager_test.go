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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransactionManager_BeginAndGet(t *testing.T) {
	tm := &TransactionManager{
		transactions: make(map[int64]*OpenTransaction),
	}

	tx := tm.Begin()
	require.NotNil(t, tx)
	assert.True(t, tx.ID > 0)
	assert.WithinDuration(t, time.Now().Add(transactionTTL), tx.ExpiresAt, 2*time.Second)

	retrieved := tm.Get(tx.ID)
	require.NotNil(t, retrieved)
	assert.Equal(t, tx.ID, retrieved.ID)
}

func TestTransactionManager_GetNonExistent(t *testing.T) {
	tm := &TransactionManager{
		transactions: make(map[int64]*OpenTransaction),
	}

	retrieved := tm.Get(999)
	assert.Nil(t, retrieved)
}

func TestTransactionManager_GetExpired(t *testing.T) {
	tm := &TransactionManager{
		transactions: make(map[int64]*OpenTransaction),
	}

	tx := tm.Begin()
	// Manually expire the transaction
	tm.mu.Lock()
	tm.transactions[tx.ID].ExpiresAt = time.Now().Add(-1 * time.Second)
	tm.mu.Unlock()

	retrieved := tm.Get(tx.ID)
	assert.Nil(t, retrieved)
}

func TestTransactionManager_AddResults(t *testing.T) {
	tm := &TransactionManager{
		transactions: make(map[int64]*OpenTransaction),
	}

	tx := tm.Begin()
	results := []StatementResult{
		{Columns: []string{"n"}, Data: []RowResult{}},
	}

	err := tm.AddResults(tx.ID, results)
	require.NoError(t, err)

	retrieved := tm.Get(tx.ID)
	require.NotNil(t, retrieved)
	assert.Len(t, retrieved.Results, 1)
}

func TestTransactionManager_AddResultsNonExistent(t *testing.T) {
	tm := &TransactionManager{
		transactions: make(map[int64]*OpenTransaction),
	}

	err := tm.AddResults(999, []StatementResult{})
	assert.Error(t, err)
}

func TestTransactionManager_Commit(t *testing.T) {
	tm := &TransactionManager{
		transactions: make(map[int64]*OpenTransaction),
	}

	tx := tm.Begin()
	results := []StatementResult{
		{Columns: []string{"n"}, Data: []RowResult{}},
	}
	require.NoError(t, tm.AddResults(tx.ID, results))

	committed, err := tm.Commit(tx.ID)
	require.NoError(t, err)
	assert.Len(t, committed, 1)

	// Transaction should be gone
	assert.Nil(t, tm.Get(tx.ID))
}

func TestTransactionManager_CommitNonExistent(t *testing.T) {
	tm := &TransactionManager{
		transactions: make(map[int64]*OpenTransaction),
	}

	_, err := tm.Commit(999)
	assert.Error(t, err)
}

func TestTransactionManager_Rollback(t *testing.T) {
	tm := &TransactionManager{
		transactions: make(map[int64]*OpenTransaction),
	}

	tx := tm.Begin()
	err := tm.Rollback(tx.ID)
	require.NoError(t, err)

	// Transaction should be gone
	assert.Nil(t, tm.Get(tx.ID))
}

func TestTransactionManager_RollbackNonExistent(t *testing.T) {
	tm := &TransactionManager{
		transactions: make(map[int64]*OpenTransaction),
	}

	err := tm.Rollback(999)
	assert.Error(t, err)
}

func TestTransactionManager_MultipleTransactions(t *testing.T) {
	tm := &TransactionManager{
		transactions: make(map[int64]*OpenTransaction),
	}

	tx1 := tm.Begin()
	tx2 := tm.Begin()
	tx3 := tm.Begin()

	assert.NotEqual(t, tx1.ID, tx2.ID)
	assert.NotEqual(t, tx2.ID, tx3.ID)

	// Roll back one, commit another, leave third open
	require.NoError(t, tm.Rollback(tx1.ID))
	_, err := tm.Commit(tx2.ID)
	require.NoError(t, err)

	assert.Nil(t, tm.Get(tx1.ID))
	assert.Nil(t, tm.Get(tx2.ID))
	assert.NotNil(t, tm.Get(tx3.ID))
}
