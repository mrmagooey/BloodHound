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

package graphify

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIngestFilePriority verifies that known filenames are mapped to the
// correct priority buckets.
func TestIngestFilePriority(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     int
	}{
		// Node-heavy types — priority 0
		{name: "users.json", filename: "users.json", want: 0},
		{name: "computers.json", filename: "computers.json", want: 0},
		{name: "groups.json", filename: "groups.json", want: 0},
		{name: "domains.json", filename: "domains.json", want: 0},
		{name: "ous.json", filename: "ous.json", want: 0},
		{name: "containers.json", filename: "containers.json", want: 0},
		{name: "gpos.json", filename: "gpos.json", want: 0},
		{name: "aiacas.json", filename: "aiacas.json", want: 0},
		{name: "rootcas.json", filename: "rootcas.json", want: 0},
		{name: "enterprisecas.json", filename: "enterprisecas.json", want: 0},
		{name: "ntauthstores.json", filename: "ntauthstores.json", want: 0},
		{name: "certtemplates.json", filename: "certtemplates.json", want: 0},
		{name: "issuancepolicies.json", filename: "issuancepolicies.json", want: 0},
		// Uppercase/mixed-case variations should normalise correctly.
		{name: "USERS.JSON uppercase", filename: "USERS.JSON", want: 0},
		{name: "Computers.Json mixed case", filename: "Computers.Json", want: 0},
		// Path prefix inside a ZIP archive should be stripped.
		{name: "archive prefix: dir/users.json", filename: "v5ingest/users.json", want: 0},
		{name: "archive prefix: dir/sessions.json", filename: "v5ingest/sessions.json", want: 1},

		// Relationship-heavy types — priority 1
		{name: "sessions.json", filename: "sessions.json", want: 1},
		{name: "localgroups.json", filename: "localgroups.json", want: 1},
		{name: "azure.json", filename: "azure.json", want: 1},
		{name: "opengraph.json", filename: "opengraph.json", want: 1},

		// Unknown types — priority 2
		{name: "custom.json unknown type", filename: "custom.json", want: 2},
		{name: "deleted.json unknown type", filename: "deleted.json", want: 2},
		{name: "empty filename", filename: "", want: 2},
		{name: "no extension", filename: "users", want: 0}, // basename without extension still matches
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ingestFilePriority(tt.filename)
			assert.Equal(t, tt.want, got, "ingestFilePriority(%q)", tt.filename)
		})
	}
}

// TestSortIngestFilesByType verifies that sortIngestFilesByType places
// node-heavy files before relationship-heavy files, while preserving
// relative order within each priority bucket (stable sort).
func TestSortIngestFilesByType(t *testing.T) {
	t.Run("mixed order: sessions before nodes is corrected", func(t *testing.T) {
		files := []IngestFileData{
			{Name: "sessions.json"},
			{Name: "users.json"},
			{Name: "computers.json"},
			{Name: "groups.json"},
		}
		sortIngestFilesByType(files)

		// All node files should appear before sessions.
		require.Len(t, files, 4)
		assert.Equal(t, "users.json", files[0].Name)
		assert.Equal(t, "computers.json", files[1].Name)
		assert.Equal(t, "groups.json", files[2].Name)
		assert.Equal(t, "sessions.json", files[3].Name)
	})

	t.Run("already sorted: node files before relationship files unchanged", func(t *testing.T) {
		files := []IngestFileData{
			{Name: "users.json"},
			{Name: "computers.json"},
			{Name: "sessions.json"},
		}
		sortIngestFilesByType(files)

		require.Len(t, files, 3)
		assert.Equal(t, "users.json", files[0].Name)
		assert.Equal(t, "computers.json", files[1].Name)
		assert.Equal(t, "sessions.json", files[2].Name)
	})

	t.Run("all node files: relative order preserved (stable sort)", func(t *testing.T) {
		files := []IngestFileData{
			{Name: "users.json"},
			{Name: "computers.json"},
			{Name: "groups.json"},
			{Name: "domains.json"},
		}
		sortIngestFilesByType(files)

		// All have priority 0; stable sort must preserve original order.
		require.Len(t, files, 4)
		assert.Equal(t, "users.json", files[0].Name)
		assert.Equal(t, "computers.json", files[1].Name)
		assert.Equal(t, "groups.json", files[2].Name)
		assert.Equal(t, "domains.json", files[3].Name)
	})

	t.Run("all relationship files: relative order preserved (stable sort)", func(t *testing.T) {
		files := []IngestFileData{
			{Name: "sessions.json"},
			{Name: "localgroups.json"},
		}
		sortIngestFilesByType(files)

		require.Len(t, files, 2)
		assert.Equal(t, "sessions.json", files[0].Name)
		assert.Equal(t, "localgroups.json", files[1].Name)
	})

	t.Run("unknown types sort after known types", func(t *testing.T) {
		files := []IngestFileData{
			{Name: "custom.json"},
			{Name: "sessions.json"},
			{Name: "users.json"},
		}
		sortIngestFilesByType(files)

		require.Len(t, files, 3)
		assert.Equal(t, "users.json", files[0].Name, "node file should be first")
		assert.Equal(t, "sessions.json", files[1].Name, "relationship file should be second")
		assert.Equal(t, "custom.json", files[2].Name, "unknown file should be last")
	})

	t.Run("empty slice: no panic", func(t *testing.T) {
		var files []IngestFileData
		require.NotPanics(t, func() { sortIngestFilesByType(files) })
		assert.Empty(t, files)
	})

	t.Run("single element: no change", func(t *testing.T) {
		files := []IngestFileData{{Name: "sessions.json"}}
		sortIngestFilesByType(files)
		require.Len(t, files, 1)
		assert.Equal(t, "sessions.json", files[0].Name)
	})

	t.Run("archive path prefixes are handled correctly", func(t *testing.T) {
		// SharpHound ZIPs place files inside a subdirectory, e.g. "v5ingest/sessions.json".
		files := []IngestFileData{
			{Name: "v5ingest/sessions.json"},
			{Name: "v5ingest/users.json"},
			{Name: "v5ingest/computers.json"},
			{Name: "v5ingest/groups.json"},
		}
		sortIngestFilesByType(files)

		require.Len(t, files, 4)
		// Node files first.
		assert.Equal(t, "v5ingest/users.json", files[0].Name)
		assert.Equal(t, "v5ingest/computers.json", files[1].Name)
		assert.Equal(t, "v5ingest/groups.json", files[2].Name)
		// Sessions last.
		assert.Equal(t, "v5ingest/sessions.json", files[3].Name)
	})

	t.Run("full SharpHound v5 archive order is corrected", func(t *testing.T) {
		// Replicates the archive order seen in the Version5ZIP fixture, where
		// sessions.json comes before the node files.
		files := []IngestFileData{
			{Name: "v5ingest/sessions.json"},
			{Name: "v5ingest/gpos.json"},
			{Name: "v5ingest/users.json"},
			{Name: "v5ingest/containers.json"},
			{Name: "v5ingest/ous.json"},
			{Name: "v5ingest/domains.json"},
			{Name: "v5ingest/groups.json"},
			{Name: "v5ingest/computers.json"},
		}
		sortIngestFilesByType(files)

		require.Len(t, files, 8)

		// Sessions (priority 1) must come after all node files (priority 0).
		sessionsIdx := -1
		for i, f := range files {
			if f.Name == "v5ingest/sessions.json" {
				sessionsIdx = i
				break
			}
		}
		require.NotEqual(t, -1, sessionsIdx, "sessions.json must be in the slice")

		for i, f := range files {
			if i >= sessionsIdx {
				break
			}
			assert.NotEqual(t, "v5ingest/sessions.json", f.Name,
				"sessions.json must not appear before index %d (found at %d)", sessionsIdx, i)
		}

		// The last file should be sessions.json.
		assert.Equal(t, "v5ingest/sessions.json", files[len(files)-1].Name)
	})
}
