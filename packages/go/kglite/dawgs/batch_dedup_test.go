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

// Package dawgs — edge deduplication is now handled entirely by the Rust
// CreateEdgesBatch implementation (skipExisting=false). The Go-side
// deduplicateEdges function and the per-batch edgesSeen map have been
// removed (OPT-5) to eliminate unbounded memory growth on large ingestions.
//
// Integration tests covering duplicate-edge behaviour live in driver_test.go
// (TestBatchOperationCreateRelationships and neighbours).  This file is
// intentionally empty of unit tests for the removed function.
package dawgs
