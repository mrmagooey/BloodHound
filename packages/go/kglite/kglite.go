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

// Package kglite provides a Go CGO wrapper around the kglite Rust graph database.
// It requires the kglite-ffi static library (libkglite.a) to be compiled first:
//
//	cd kglite-ffi && cargo build --release --no-default-features --features ffi
package kglite

/*
#cgo LDFLAGS: ${SRCDIR}/../../../kglite-ffi/target/release/libkglite.a -lm
#cgo windows LDFLAGS: -lws2_32 -luserenv -lntdll -lbcrypt
#include "kglite.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"unsafe"
)

// KnowledgeGraph is a handle to a kglite embedded graph database.
// All methods are goroutine-safe (the Rust side uses a Mutex internally).
type KnowledgeGraph struct {
	h *C.KgHandle
}

// New creates a new empty KnowledgeGraph.
func New() (*KnowledgeGraph, error) {
	h := C.kg_new()
	if h == nil {
		return nil, errors.New("kglite: kg_new returned null")
	}
	return &KnowledgeGraph{h: h}, nil
}

// Load opens a KnowledgeGraph from a .kgl file.
func Load(path string) (*KnowledgeGraph, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	h := C.kg_load(cpath)
	if h == nil {
		return nil, fmt.Errorf("kglite: kg_load: %s", lastError())
	}
	return &KnowledgeGraph{h: h}, nil
}

// Free releases the graph handle. It is safe to call Free on a nil KnowledgeGraph.
func (kg *KnowledgeGraph) Free() {
	if kg != nil && kg.h != nil {
		C.kg_free(kg.h)
		kg.h = nil
	}
}

// Save persists the graph to a .kgl file.
func (kg *KnowledgeGraph) Save(path string) error {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	if rc := C.kg_save(kg.h, cpath); rc != 0 {
		return fmt.Errorf("kglite: kg_save: %s", lastError())
	}
	return nil
}

// CypherResult holds the decoded result of a Cypher query.
type CypherResult struct {
	Columns []string        `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
}

// Cypher executes a Cypher query with optional parameters.
// params may be nil. Returns a CypherResult on success.
func (kg *KnowledgeGraph) Cypher(query string, params map[string]interface{}) (*CypherResult, error) {
	cquery := C.CString(query)
	defer C.free(unsafe.Pointer(cquery))

	var cparams *C.char
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("kglite: marshal params: %w", err)
		}
		cparams = C.CString(string(b))
		defer C.free(unsafe.Pointer(cparams))
	}

	var out *C.char
	rc := C.kg_cypher(kg.h, cquery, cparams, &out)
	if rc != 0 {
		return nil, fmt.Errorf("kglite: kg_cypher: %s", lastError())
	}
	defer C.kg_free_string(out)

	goJSON := C.GoString(out)
	var result CypherResult
	if err := json.Unmarshal([]byte(goJSON), &result); err != nil {
		return nil, fmt.Errorf("kglite: unmarshal result: %w", err)
	}
	return &result, nil
}

// BatchQuery represents a single query in a batch execution.
type BatchQuery struct {
	Query  string                 `json:"query"`
	Params map[string]interface{} `json:"params,omitempty"`
}

// CypherBatch executes multiple Cypher queries in a single Mutex lock acquisition.
// This is significantly faster than calling Cypher() in a loop because it avoids
// repeated CGO boundary crossings and Mutex lock/unlock per query.
func (kg *KnowledgeGraph) CypherBatch(queries []BatchQuery) ([]*CypherResult, error) {
	if len(queries) == 0 {
		return nil, nil
	}

	b, err := json.Marshal(queries)
	if err != nil {
		return nil, fmt.Errorf("kglite: marshal batch: %w", err)
	}
	cjson := C.CString(string(b))
	defer C.free(unsafe.Pointer(cjson))

	var out *C.char
	rc := C.kg_cypher_batch(kg.h, cjson, &out)
	if rc != 0 {
		return nil, fmt.Errorf("kglite: kg_cypher_batch: %s", lastError())
	}
	defer C.kg_free_string(out)

	goJSON := C.GoString(out)
	var rawResults []json.RawMessage
	if err := json.Unmarshal([]byte(goJSON), &rawResults); err != nil {
		return nil, fmt.Errorf("kglite: unmarshal batch result: %w", err)
	}

	results := make([]*CypherResult, len(rawResults))
	for i, raw := range rawResults {
		var r CypherResult
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("kglite: unmarshal batch result[%d]: %w", i, err)
		}
		results[i] = &r
	}
	return results, nil
}

// EdgeSpec describes a single edge to create in a bulk operation.
type EdgeSpec struct {
	Src   uint64                 `json:"src"`
	Dst   uint64                 `json:"dst"`
	Type  string                 `json:"type"`
	Props map[string]interface{} `json:"props,omitempty"`
}

// CreateEdgesBatch bulk-creates edges by node index, bypassing Cypher entirely.
// This is significantly faster than individual MATCH+MERGE Cypher queries because
// it uses kglite's ConnectionBatchProcessor directly with NodeIndex values.
// If skipExisting is true, duplicate-edge checks are skipped (use when edges are
// known to be new, e.g. after DeleteTransitEdges).
// Returns the number of edges created.
func (kg *KnowledgeGraph) CreateEdgesBatch(edges []EdgeSpec, skipExisting bool) (int64, error) {
	if len(edges) == 0 {
		return 0, nil
	}

	b, err := json.Marshal(edges)
	if err != nil {
		return 0, fmt.Errorf("kglite: marshal edges: %w", err)
	}
	cjson := C.CString(string(b))
	defer C.free(unsafe.Pointer(cjson))

	skipFlag := C.int(0)
	if skipExisting {
		skipFlag = 1
	}

	var out *C.char
	rc := C.kg_create_edges_batch(kg.h, cjson, skipFlag, &out)
	if rc != 0 {
		return 0, fmt.Errorf("kglite: kg_create_edges_batch: %s", lastError())
	}
	defer C.kg_free_string(out)

	goJSON := C.GoString(out)
	var result struct {
		Created int64 `json:"created"`
	}
	if err := json.Unmarshal([]byte(goJSON), &result); err != nil {
		return 0, fmt.Errorf("kglite: unmarshal edge result: %w", err)
	}
	return result.Created, nil
}

// CypherRows is a convenience wrapper that returns rows as []map[string]interface{}.
func (kg *KnowledgeGraph) CypherRows(query string, params map[string]interface{}) ([]map[string]interface{}, error) {
	result, err := kg.Cypher(query, params)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]interface{}, 0, len(result.Rows))
	for _, row := range result.Rows {
		m := make(map[string]interface{}, len(result.Columns))
		for i, col := range result.Columns {
			if i < len(row) {
				m[col] = row[i]
			}
		}
		rows = append(rows, m)
	}
	return rows, nil
}

// RustMemStats holds Rust heap statistics from the tracking allocator.
type RustMemStats struct {
	CurrentBytes uint64
	PeakBytes    uint64
	TotalAllocs  uint64
}

// MemoryStats returns current Rust heap statistics.
// All fields will be zero in builds that do not include the tracking allocator.
func MemoryStats() RustMemStats {
	s := C.kg_memory_stats()
	return RustMemStats{
		CurrentBytes: uint64(s.current_bytes),
		PeakBytes:    uint64(s.peak_bytes),
		TotalAllocs:  uint64(s.total_allocs),
	}
}

func lastError() string {
	p := C.kg_last_error()
	if p == nil {
		return "(no error)"
	}
	return C.GoString(p)
}
