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
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const transactionTTL = 60 * time.Second

// OpenTransaction holds state for a Neo4j-style open transaction.
type OpenTransaction struct {
	ID        int64
	CreatedAt time.Time
	ExpiresAt time.Time
	// Results accumulated across multiple statements in the transaction
	Results []StatementResult
}

// TransactionManager manages in-memory Neo4j-style transactions with TTL.
type TransactionManager struct {
	mu           sync.Mutex
	transactions map[int64]*OpenTransaction
	nextID       atomic.Int64
}

// NewTransactionManager creates a new TransactionManager and starts a background
// goroutine to reap expired transactions. The goroutine stops when ctx is cancelled.
func NewTransactionManager() *TransactionManager {
	return NewTransactionManagerWithContext(context.Background())
}

// NewTransactionManagerWithContext creates a new TransactionManager and starts a background
// goroutine to reap expired transactions. The reap goroutine exits when ctx is cancelled.
func NewTransactionManagerWithContext(ctx context.Context) *TransactionManager {
	tm := &TransactionManager{
		transactions: make(map[int64]*OpenTransaction),
	}

	go tm.reapLoop(ctx)

	return tm
}

// Begin creates a new open transaction and returns it.
func (tm *TransactionManager) Begin() *OpenTransaction {
	id := tm.nextID.Add(1)
	now := time.Now()

	tx := &OpenTransaction{
		ID:        id,
		CreatedAt: now,
		ExpiresAt: now.Add(transactionTTL),
		Results:   []StatementResult{},
	}

	tm.mu.Lock()
	tm.transactions[id] = tx
	tm.mu.Unlock()

	return tx
}

// Get retrieves an open transaction by ID. Returns nil if not found or expired.
func (tm *TransactionManager) Get(id int64) *OpenTransaction {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tx, ok := tm.transactions[id]
	if !ok {
		return nil
	}

	if time.Now().After(tx.ExpiresAt) {
		delete(tm.transactions, id)
		return nil
	}

	// Refresh TTL on access
	tx.ExpiresAt = time.Now().Add(transactionTTL)
	return tx
}

// AddResults appends statement results to an open transaction.
func (tm *TransactionManager) AddResults(id int64, results []StatementResult) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tx, ok := tm.transactions[id]
	if !ok {
		return fmt.Errorf("transaction %d not found", id)
	}

	if time.Now().After(tx.ExpiresAt) {
		delete(tm.transactions, id)
		return fmt.Errorf("transaction %d has expired", id)
	}

	tx.Results = append(tx.Results, results...)
	tx.ExpiresAt = time.Now().Add(transactionTTL)
	return nil
}

// Commit removes a transaction and returns its accumulated results.
func (tm *TransactionManager) Commit(id int64) ([]StatementResult, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tx, ok := tm.transactions[id]
	if !ok {
		return nil, fmt.Errorf("transaction %d not found", id)
	}

	results := tx.Results
	delete(tm.transactions, id)
	return results, nil
}

// Rollback removes a transaction without returning results.
func (tm *TransactionManager) Rollback(id int64) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, ok := tm.transactions[id]; !ok {
		return fmt.Errorf("transaction %d not found", id)
	}

	delete(tm.transactions, id)
	return nil
}

// reapLoop periodically removes expired transactions. It exits when ctx is cancelled.
func (tm *TransactionManager) reapLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tm.mu.Lock()
			now := time.Now()
			for id, tx := range tm.transactions {
				if now.After(tx.ExpiresAt) {
					delete(tm.transactions, id)
				}
			}
			tm.mu.Unlock()
		}
	}
}
