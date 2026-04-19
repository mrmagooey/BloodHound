// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0
package endpoint

import (
	"context"
	"strings"
	"sync"

	"github.com/specterops/bloodhound/packages/go/ein"
	"github.com/specterops/bloodhound/packages/go/errorlist"
	"github.com/specterops/dawgs/cache"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
	"github.com/specterops/dawgs/util/channels"
)

// Resolver orchestrates the asynchronous resolution of ingestible relationships.
// It manages a pool of workers that consume incoming relationship definitions,
// resolve their Source and Target endpoints against the database using a
// cache-first strategy, and emit fully resolved relationships downstream.
type Resolver struct {
	db               graph.Database
	cacheKeyDigester *CacheEntryDigester
	cache            cache.Cache[uint64, ein.IngestibleEndpoint]
	fieldLock        sync.RWMutex
	workC            chan *ein.IngestibleRelationship
	workWG           sync.WaitGroup
	started          bool
	workerErrors     *errorlist.ErrorBuilder
	stateLock        sync.Mutex
	// snapshot is an optional frozen view of lowerName→objectid populated before a
	// BatchOperation begins. When non-nil, name-based resolution uses this map
	// instead of live DB queries, matching Neo4j's read-isolation semantics.
	snapshot map[string]string
}

// NewResolver initializes a new Resolver instance with the provided database connection
// and pre-configures it with a 500,000 entry sieve cache.
func NewResolver(db graph.Database) *Resolver {
	return &Resolver{
		db:               db,
		cacheKeyDigester: NewCacheEntryDigester(),
		cache:            cache.NewSieve[uint64, ein.IngestibleEndpoint](500_000),
	}
}

// PopulateSnapshot takes a pre-batch snapshot of all named nodes in the graph
// (lowerName → objectid). While the snapshot is active, name-based resolution
// uses this frozen map instead of live DB queries, which matches Neo4j's
// read-isolation semantics and prevents in-batch nodes from being resolved.
// It also resets the sieve cache so stale entries from previous batches are evicted.
func (s *Resolver) PopulateSnapshot(ctx context.Context) error {
	snap := make(map[string]string)

	if err := s.db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		return tx.Nodes().Filter(
			query.Exists(query.NodeProperty("name")),
		).Query(func(results graph.Result) error {
			defer results.Close()

			for results.Next() {
				var (
					name     string
					objectID string
				)

				if err := results.Scan(&name, &objectID); err != nil {
					return err
				}

				snap[strings.ToLower(name)] = objectID
			}

			return results.Error()
		}, query.Returning(
			query.NodeProperty("name"),
			query.NodeProperty("objectid"),
		))
	}); err != nil {
		return err
	}

	s.fieldLock.Lock()
	defer s.fieldLock.Unlock()

	s.snapshot = snap
	// Reset the sieve cache to evict any stale entries from prior batches.
	s.cache = cache.NewSieve[uint64, ein.IngestibleEndpoint](500_000)

	return nil
}

// ClearSnapshot removes the frozen snapshot map, restoring live DB query behaviour
// for name-based resolution. Call this after a BatchOperation completes.
func (s *Resolver) ClearSnapshot() {
	s.fieldLock.Lock()
	defer s.fieldLock.Unlock()

	s.snapshot = nil
}

// snapshotLookupByName returns the objectid for a node whose lowercased name
// equals lowerName, using the frozen snapshot if one is active.
// Returns ("", false) when no snapshot is set or the name is not found.
func (s *Resolver) snapshotLookupByName(lowerName string) (string, bool) {
	s.fieldLock.RLock()
	defer s.fieldLock.RUnlock()

	if s.snapshot == nil {
		return "", false
	}

	objectID, found := s.snapshot[lowerName]
	return objectID, found
}

// snapshotIsActive returns true when a frozen snapshot map is currently set.
func (s *Resolver) snapshotIsActive() bool {
	s.fieldLock.RLock()
	defer s.fieldLock.RUnlock()

	return s.snapshot != nil
}

// Submit attempts to queue an IngestibleRelationship for processing.
// It returns true if the item was successfully sent to the work channel,
// or false if the context has been cancelled or the channel is closed.
func (s *Resolver) Submit(ctx context.Context, ingestEntry *ein.IngestibleRelationship) bool {
	return channels.Submit(ctx, s.workC, ingestEntry)
}

// CachedEndpointLookup performs a thread-safe read lookup in the internal endpoint cache.
// It returns the cached endpoint and a boolean indicating if the key was found.
func (s *Resolver) CachedEndpointLookup(key uint64) (ein.IngestibleEndpoint, bool) {
	s.fieldLock.RLock()
	defer s.fieldLock.RUnlock()

	cachedEndpoint, cached := s.cache.Get(key)
	return cachedEndpoint, cached
}

// cacheEndpoint stores an endpoint in the internal cache under the given key.
// This operation acquires a write lock on the field mutex.
func (s *Resolver) cacheEndpoint(key uint64, endpoint ein.IngestibleEndpoint) {
	s.fieldLock.Lock()
	defer s.fieldLock.Unlock()

	s.cache.Put(key, endpoint)
}

// addWorkerError records an error encountered by one of the worker goroutines
// into the internal error builder for later retrieval via Done().
func (s *Resolver) addWorkerError(err error) {
	s.fieldLock.Lock()
	defer s.fieldLock.Unlock()

	s.workerErrors.Add(err)
}

// dbLoop returns a transaction function designed to be executed within a database read transaction.
// It continuously drains the work channel, resolves endpoints using the cache and database,
// and pushes resolved relationships to the output channel. If the context is cancelled or
// the input channel closes, it exits gracefully.
func (s *Resolver) dbLoop(ctx context.Context) func(tx graph.Transaction) error {
	return func(tx graph.Transaction) error {
		for {
			ingestEntry, shouldContinue := channels.Receive(ctx, s.workC)

			if !shouldContinue {
				break
			}

			if cacheKey, err := s.cacheKeyDigester.DigestEndpoint(ingestEntry.Source); err != nil {
				return err
			} else if cachedEntry, cached := s.CachedEndpointLookup(cacheKey); cached {
				ingestEntry.Source = cachedEntry
			} else {
				switch ingestEntry.Source.MatchBy {
				case ein.MatchByName:
					// When a snapshot is active, resolve name lookups from the frozen
					// pre-batch map so that nodes written during this batch are invisible,
					// matching Neo4j's read-isolation semantics.
					if objectID, found := s.snapshotLookupByName(strings.ToLower(ingestEntry.Source.Value)); found {
						resolved := ein.IngestibleEndpoint{
							Kind:    ingestEntry.Source.Kind,
							MatchBy: ein.MatchByID,
							Value:   objectID,
						}
						ingestEntry.Source = resolved
						s.cacheEndpoint(cacheKey, resolved)
					} else if s.snapshotIsActive() {
						// Snapshot is set but name not found — drop this edge (matches Neo4j behaviour)
						s.addWorkerError(newPropertyMatcherError(ingestEntry.Source, graph.ErrNoResultsFound))
						continue
					} else {
						// No snapshot: fall through to live DB resolution
						if resolvedEndpoint, err := resolveIngestibleEndpoint(tx, ingestEntry.Source); err != nil {
							s.addWorkerError(err)
							continue
						} else {
							ingestEntry.Source = resolvedEndpoint
							s.cacheEndpoint(cacheKey, resolvedEndpoint)
						}
					}

				case ein.MatchByProperty:
					if resolvedEndpoint, err := resolveIngestibleEndpoint(tx, ingestEntry.Source); err != nil {
						s.addWorkerError(err)
						continue
					} else {
						// Update the source endpoint with the resolution
						ingestEntry.Source = resolvedEndpoint

						// Only cache lookups that passed through to the DB
						s.cacheEndpoint(cacheKey, resolvedEndpoint)
					}
				}
			}

			if cacheKey, err := s.cacheKeyDigester.DigestEndpoint(ingestEntry.Target); err != nil {
				return err
			} else if cachedEntry, cached := s.CachedEndpointLookup(cacheKey); cached {
				ingestEntry.Target = cachedEntry
			} else {
				switch ingestEntry.Target.MatchBy {
				case ein.MatchByName:
					// When a snapshot is active, resolve name lookups from the frozen
					// pre-batch map so that nodes written during this batch are invisible,
					// matching Neo4j's read-isolation semantics.
					if objectID, found := s.snapshotLookupByName(strings.ToLower(ingestEntry.Target.Value)); found {
						resolved := ein.IngestibleEndpoint{
							Kind:    ingestEntry.Target.Kind,
							MatchBy: ein.MatchByID,
							Value:   objectID,
						}
						ingestEntry.Target = resolved
						s.cacheEndpoint(cacheKey, resolved)
					} else if s.snapshotIsActive() {
						// Snapshot is set but name not found — drop this edge (matches Neo4j behaviour)
						s.addWorkerError(newPropertyMatcherError(ingestEntry.Target, graph.ErrNoResultsFound))
						continue
					} else {
						// No snapshot: fall through to live DB resolution
						if resolvedEndpoint, err := resolveIngestibleEndpoint(tx, ingestEntry.Target); err != nil {
							s.addWorkerError(err)
							continue
						} else {
							ingestEntry.Target = resolvedEndpoint
							s.cacheEndpoint(cacheKey, resolvedEndpoint)
						}
					}

				case ein.MatchByProperty:
					if resolvedEndpoint, err := resolveIngestibleEndpoint(tx, ingestEntry.Target); err != nil {
						s.addWorkerError(err)
						continue
					} else {
						// Update the target endpoint with the resolution
						ingestEntry.Target = resolvedEndpoint

						// Only cache lookups that passed through to the DB
						s.cacheEndpoint(cacheKey, resolvedEndpoint)
					}
				}
			}
		}

		return nil
	}
}

// Start launches the specified number of worker goroutines to process the work queue.
// Each worker runs a database read transaction loop. This method is idempotent;
// calling it multiple times will not spawn additional workers if already started.
func (s *Resolver) Start(ctx context.Context, maxWorkers int) {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	if s.started {
		return
	}

	s.workC = make(chan *ein.IngestibleRelationship)
	s.workerErrors = errorlist.NewBuilder()
	s.started = true

	for workerID := 0; workerID < maxWorkers; workerID += 1 {
		s.workWG.Add(1)

		go func() {
			defer s.workWG.Done()

			if err := s.db.ReadTransaction(ctx, s.dbLoop(ctx)); err != nil {
				s.addWorkerError(err)
			}
		}()
	}
}

// Done signals the resolver to stop accepting new work and waits for all active
// workers to finish processing. It closes the input channel, waits for the wait group,
// closes the output channel, and returns an aggregated error containing any failures
// encountered by the workers during their lifecycle.
func (s *Resolver) Done() error {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	if s.started {
		s.started = false

		// Stop the work channel to flush the workers clear
		close(s.workC)

		// Wait for the workers to exit
		s.workWG.Wait()
		return s.workerErrors.Build()
	}

	return nil
}
