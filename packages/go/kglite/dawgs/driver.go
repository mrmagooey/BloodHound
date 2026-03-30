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

// Package dawgs implements the graph.Database interface backed by kglite.
package dawgs

import (
	"context"
	"fmt"

	"github.com/specterops/bloodhound/packages/go/kglite"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/util/size"
)

// Driver implements graph.Database using kglite as the backing store.
// kglite is internally goroutine-safe (Rust Mutex); no additional Go locking is needed.
type Driver struct {
	kg             *kglite.KnowledgeGraph
	graphPath      string
	writeFlushSize int
	batchWriteSize int
}

// Open opens or creates a kglite graph database at the given file path.
// If the file exists, it is loaded; otherwise an empty graph is created.
func Open(graphPath string) (*Driver, error) {
	var (
		kg  *kglite.KnowledgeGraph
		err error
	)

	if graphPath != "" {
		kg, err = kglite.Load(graphPath)
		if err != nil {
			// If load fails, try creating a new graph (file may not exist yet)
			kg, err = kglite.New()
			if err != nil {
				return nil, fmt.Errorf("kglite: failed to create graph: %w", err)
			}
		}
	} else {
		kg, err = kglite.New()
		if err != nil {
			return nil, fmt.Errorf("kglite: failed to create graph: %w", err)
		}
	}

	return &Driver{
		kg:             kg,
		graphPath:      graphPath,
		writeFlushSize: 100_000,
		batchWriteSize: 20_000,
	}, nil
}

// Close flushes any pending writes to disk and frees resources.
func (d *Driver) Close(ctx context.Context) error {
	if d.kg == nil {
		return nil
	}

	var err error
	if d.graphPath != "" {
		err = d.kg.Save(d.graphPath)
	}
	d.kg.Free()
	d.kg = nil
	return err
}

func (d *Driver) SetWriteFlushSize(interval int) {
	d.writeFlushSize = interval
}

func (d *Driver) SetBatchWriteSize(interval int) {
	d.batchWriteSize = interval
}

// AssertSchema is a no-op — kglite doesn't need schema pre-declaration.
func (d *Driver) AssertSchema(ctx context.Context, dbSchema graph.Schema) error {
	return nil
}

// SetDefaultGraph is a no-op — kglite doesn't have graph namespaces.
func (d *Driver) SetDefaultGraph(ctx context.Context, graphSchema graph.Graph) error {
	return nil
}

// FetchKinds returns empty kinds — kglite tracks kinds dynamically.
func (d *Driver) FetchKinds(ctx context.Context) (graph.Kinds, error) {
	return graph.Kinds{}, nil
}

// RefreshKinds is a no-op.
func (d *Driver) RefreshKinds(ctx context.Context) error {
	return nil
}

// Run executes a raw Cypher statement discarding results.
func (d *Driver) Run(ctx context.Context, query string, parameters map[string]any) error {
	_, err := d.kg.Cypher(query, parameters)
	return err
}

// ReadTransaction opens a read-only transaction context.
func (d *Driver) ReadTransaction(ctx context.Context, txDelegate graph.TransactionDelegate, options ...graph.TransactionOption) error {
	tx := &Transaction{
		ctx:      ctx,
		driver:   d,
		readOnly: true,
	}
	return txDelegate(tx)
}

// WriteTransaction opens a read-write transaction context.
func (d *Driver) WriteTransaction(ctx context.Context, txDelegate graph.TransactionDelegate, options ...graph.TransactionOption) error {
	tx := &Transaction{
		ctx:      ctx,
		driver:   d,
		readOnly: false,
	}
	return txDelegate(tx)
}

// BatchOperation opens a batch write context.
func (d *Driver) BatchOperation(ctx context.Context, batchDelegate graph.BatchDelegate) error {
	batch := &Batch{
		ctx:    ctx,
		driver: d,
	}
	return batchDelegate(batch)
}

// Save persists the graph to disk at the configured graph path.
func (d *Driver) Save() error {
	if d.graphPath == "" {
		return nil
	}
	return d.kg.Save(d.graphPath)
}

// graphQueryMemoryLimit returns a generous default limit.
func graphQueryMemoryLimit() size.Size {
	return size.Gibibyte * 2
}
