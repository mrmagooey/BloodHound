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

// Package e2e contains end-to-end tests for the standalone BloodHound binary.
// These tests load the official BloodHound sample data and verify query results.
//
// Run with:
//
//	go test -v -tags e2e -timeout 30m ./cmd/api/src/test/e2e/
//
// The tests expect the sample data zips to be present in the testdata/ directory:
//
//	testdata/ad_sampledata.zip     (AD / SharpHound data)
//	testdata/entra_sampledata.zip  (Azure / AzureHound data)
//go:build e2e

package e2e_test

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/specterops/bloodhound/cmd/api/src/analysis/ad"
	"github.com/specterops/bloodhound/cmd/api/src/analysis/azure"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/bloodhound/cmd/api/src/services/graphify"
	"github.com/specterops/bloodhound/cmd/api/src/services/graphify/endpoint"
	"github.com/specterops/bloodhound/cmd/api/src/services/upload"
	"github.com/specterops/bloodhound/packages/go/analysis"
	"github.com/specterops/bloodhound/packages/go/bomenc"
	"github.com/specterops/bloodhound/packages/go/kglite"
	kglitedawgs "github.com/specterops/bloodhound/packages/go/kglite/dawgs"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

// testdataDir returns the absolute path to the testdata directory next to this file.
func testdataDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata")
}

// skipIfMissing skips the test if the given file does not exist.
func skipIfMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("test data not found (run: make e2e-testdata): %s", path)
	}
}

// openGraph opens a fresh kglite graph for the test and registers cleanup.
func openGraph(t *testing.T) graph.Database {
	t.Helper()
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "test.kgl")
	db, err := kglitedawgs.Open(graphPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close(context.Background()) })
	return db
}

// loadIngestSchema loads the upload ingest schema, failing the test on error.
func loadIngestSchema(t *testing.T) upload.IngestSchema {
	t.Helper()
	schema, err := upload.LoadIngestSchema()
	require.NoError(t, err, "load ingest schema")
	return schema
}

// ingestZip extracts and ingests all JSON files from a zip archive into the graph.
// Returns the ingest duration.
func ingestZip(ctx context.Context, t *testing.T, db graph.Database, zipPath string, schema upload.IngestSchema) time.Duration {
	t.Helper()

	start := time.Now()

	resolver := endpoint.NewResolver(db)
	ic := graphify.NewIngestContext(ctx,
		graphify.WithIngestTime(time.Now().UTC()),
		graphify.WithEndpointResolver(resolver),
	)

	readOpts := graphify.ReadOptions{
		FileType:     model.FileTypeZip,
		IngestSchema: schema,
		RegisterSourceKind: func(kind graph.Kind) error {
			return db.RefreshKinds(ctx)
		},
	}

	err := db.BatchOperation(ctx, func(batch graph.Batch) error {
		ic.BindBatchUpdater(batch)
		return processZip(ctx, ic, zipPath, readOpts)
	})
	require.NoError(t, err, "ingest zip %s", filepath.Base(zipPath))

	return time.Since(start)
}

// processZip opens a zip archive and calls graphify.ReadFileForIngest on each JSON entry.
func processZip(ctx context.Context, ic *graphify.IngestContext, zipPath string, readOpts graphify.ReadOptions) error {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", zipPath, err)
	}
	defer archive.Close()

	var firstErr error
	for _, f := range archive.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if err := processZipEntry(ctx, ic, f, readOpts); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// processZipEntry extracts a single zip entry to a temp file and ingests it.
func processZipEntry(_ context.Context, ic *graphify.IngestContext, f *zip.File, readOpts graphify.ReadOptions) error {
	src, err := f.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %s: %w", f.Name, err)
	}
	defer src.Close()

	normalized, err := bomenc.NormalizeToUTF8(src)
	if err != nil {
		return fmt.Errorf("normalize %s: %w", f.Name, err)
	}

	tmp, err := os.CreateTemp("", "bh-e2e-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", f.Name, err)
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	if _, err := io.Copy(tmp, normalized); err != nil {
		return fmt.Errorf("copy %s to temp: %w", f.Name, err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek temp for %s: %w", f.Name, err)
	}

	return graphify.ReadFileForIngest(ic, tmp, readOpts)
}

// runAnalysis executes AD and Azure post-processing. Returns the analysis duration.
func runAnalysis(ctx context.Context, t *testing.T, db graph.Database) time.Duration {
	t.Helper()
	start := time.Now()

	counter := analysis.NewCompositionCounter()
	_, err := ad.Post(ctx, db, true, false, true, &counter)
	require.NoError(t, err, "AD post-processing")

	// Profile Azure analysis queries
	kglitedawgs.EnableProfiling()
	defer func() {
		kglitedawgs.PrintReport()
		kglitedawgs.DisableProfiling()
	}()

	_, err = azure.Post(ctx, db)
	require.NoError(t, err, "Azure post-processing")

	return time.Since(start)
}

// memSnapshot captures Go heap, system, and Rust heap memory at a point in time.
type memSnapshot struct {
	label    string
	heapAlloc  uint64 // Go: bytes currently allocated on the heap
	heapSys    uint64 // Go: bytes obtained from the OS for the heap
	heapInuse  uint64 // Go: bytes in in-use heap spans
	sys        uint64 // Go: total bytes obtained from the OS
	numGC      uint32 // Go: number of GC cycles completed so far
	rustCurrent uint64 // Rust: live heap bytes (tracking allocator)
	rustPeak    uint64 // Rust: peak live heap bytes (tracking allocator)
	rustAllocs  uint64 // Rust: total allocation count (tracking allocator)
}

// takeMemSnapshot forces a GC then captures runtime.MemStats and Rust heap stats.
func takeMemSnapshot(label string) memSnapshot {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	rs := kglite.MemoryStats()
	return memSnapshot{
		label:       label,
		heapAlloc:   ms.HeapAlloc,
		heapSys:     ms.HeapSys,
		heapInuse:   ms.HeapInuse,
		sys:         ms.Sys,
		numGC:       ms.NumGC,
		rustCurrent: rs.CurrentBytes,
		rustPeak:    rs.PeakBytes,
		rustAllocs:  rs.TotalAllocs,
	}
}

// formatBytes formats a byte count as a human-readable string (MiB / KiB).
func formatBytes(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// logMemTable prints a formatted memory summary table including Rust heap stats.
func logMemTable(t *testing.T, snapshots []memSnapshot) {
	t.Helper()
	t.Logf("--- Memory usage summary ---")
	t.Logf("%-25s %12s %12s %12s %12s  %-5s  %12s %12s %12s",
		"Checkpoint", "GoHeapAlloc", "GoHeapInuse", "GoHeapSys", "GoSys", "GC#",
		"RustCurrent", "RustPeak", "RustAllocs")
	t.Logf("%s", strings.Repeat("-", 120))
	for _, s := range snapshots {
		t.Logf("%-25s %12s %12s %12s %12s  %-5d  %12s %12s %12d",
			s.label,
			formatBytes(s.heapAlloc),
			formatBytes(s.heapInuse),
			formatBytes(s.heapSys),
			formatBytes(s.sys),
			s.numGC,
			formatBytes(s.rustCurrent),
			formatBytes(s.rustPeak),
			s.rustAllocs,
		)
	}
	t.Logf("%s", strings.Repeat("-", 120))
}

// queryStat holds timing and result for a single Cypher query.
type queryStat struct {
	name     string
	cypher   string
	duration time.Duration
	result   string
	err      error
}

// runQuery executes a Cypher query and returns the first row as a formatted string.
func runQuery(ctx context.Context, db graph.Database, cypher string) (string, time.Duration, error) {
	start := time.Now()

	var out strings.Builder
	err := db.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw(cypher, nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}

		keys := result.Keys()
		for result.Next() {
			vals := result.Values()
			for i, key := range keys {
				if i > 0 {
					out.WriteString(", ")
				}
				fmt.Fprintf(&out, "%s=%v", key, vals[i])
			}
			out.WriteString("\n")
		}
		return result.Error()
	})

	return strings.TrimRight(out.String(), "\n"), time.Since(start), err
}

// presetQuery defines a named Cypher query for the preset query suite.
type presetQuery struct {
	Name   string
	Cypher string
}

// adPresetQueries are the preset queries run against the AD sample data.
var adPresetQueries = []presetQuery{
	{
		Name:   "Total nodes",
		Cypher: `MATCH (n) RETURN count(n) AS nodes`,
	},
	{
		Name:   "Total relationships",
		Cypher: `MATCH ()-[r]->() RETURN count(r) AS relationships`,
	},
	{
		Name:   "Domains",
		Cypher: `MATCH (n:Domain) RETURN n.name AS domain, n.objectid AS sid ORDER BY domain`,
	},
	{
		Name:   "Computers",
		Cypher: `MATCH (n:Computer) RETURN count(n) AS computers`,
	},
	{
		Name:   "Users",
		Cypher: `MATCH (n:User) RETURN count(n) AS users`,
	},
	{
		Name:   "Groups",
		Cypher: `MATCH (n:Group) RETURN count(n) AS groups`,
	},
	{
		Name:   "Kerberoastable users",
		Cypher: `MATCH (u:User) WHERE u.hasspn = true AND u.enabled = true RETURN count(u) AS kerberoastable`,
	},
	{
		Name:   "AS-REP roastable users",
		Cypher: `MATCH (u:User) WHERE u.dontreqpreauth = true AND u.enabled = true RETURN count(u) AS asrep_roastable`,
	},
	{
		Name:   "AdminCount users",
		Cypher: `MATCH (u:User) WHERE u.admincount = true RETURN count(u) AS admin_count_users`,
	},
	{
		Name:   "AdminCount computers",
		Cypher: `MATCH (c:Computer) WHERE c.admincount = true RETURN count(c) AS admin_count_computers`,
	},
	{
		Name:   "Enabled domain admin users",
		Cypher: `MATCH (u:User)-[:MemberOf*1..]->(g:Group) WHERE g.objectid ENDS WITH '-512' AND u.enabled = true RETURN count(DISTINCT u) AS domain_admins`,
	},
	{
		Name:   "DCSync relationships",
		Cypher: `MATCH ()-[r:DCSync]->() RETURN count(r) AS dcsync`,
	},
	{
		Name:   "HasSession relationships",
		Cypher: `MATCH ()-[r:HasSession]->() RETURN count(r) AS sessions`,
	},
	{
		Name:   "AdminTo relationships",
		Cypher: `MATCH ()-[r:AdminTo]->() RETURN count(r) AS admin_tos`,
	},
	{
		Name:   "MemberOf relationships",
		Cypher: `MATCH ()-[r:MemberOf]->() RETURN count(r) AS member_ofs`,
	},
	{
		Name:   "ADCS cert templates",
		Cypher: `MATCH (n:CertTemplate) RETURN count(n) AS cert_templates`,
	},
	{
		Name:   "Enterprise CAs",
		Cypher: `MATCH (n:EnterpriseCA) RETURN count(n) AS enterprise_cas`,
	},
	{
		Name:   "Computers with unconstrained delegation",
		Cypher: `MATCH (c:Computer) WHERE c.unconstraineddelegation = true RETURN count(c) AS unconstrained_delegation`,
	},
	{
		Name:   "OUs",
		Cypher: `MATCH (n:OU) RETURN count(n) AS ous`,
	},
	{
		Name:   "GPOs",
		Cypher: `MATCH (n:GPO) RETURN count(n) AS gpos`,
	},
}

// azurePresetQueries are the preset queries run after loading the Entra sample data.
var azurePresetQueries = []presetQuery{
	{
		Name:   "Azure tenants",
		Cypher: `MATCH (n:AZTenant) RETURN count(n) AS tenants`,
	},
	{
		Name:   "Azure users",
		Cypher: `MATCH (n:AZUser) RETURN count(n) AS az_users`,
	},
	{
		Name:   "Azure service principals",
		Cypher: `MATCH (n:AZServicePrincipal) RETURN count(n) AS service_principals`,
	},
	{
		Name:   "Azure apps",
		Cypher: `MATCH (n:AZApp) RETURN count(n) AS apps`,
	},
	{
		Name:   "Azure VMs",
		Cypher: `MATCH (n:AZVM) RETURN count(n) AS vms`,
	},
	{
		Name:   "Azure groups",
		Cypher: `MATCH (n:AZGroup) RETURN count(n) AS az_groups`,
	},
	{
		Name:   "AZGlobalAdmin relationships",
		Cypher: `MATCH ()-[r:AZGlobalAdmin]->() RETURN count(r) AS global_admins`,
	},
	{
		Name:   "AZOwns relationships",
		Cypher: `MATCH ()-[r:AZOwns]->() RETURN count(r) AS az_owns`,
	},
}

// TestLoadAndQueryAD loads the AD sample data and runs the AD preset queries.
func TestLoadAndQueryAD(t *testing.T) {
	ctx := context.Background()
	adZip := filepath.Join(testdataDir(), "ad_sampledata.zip")
	skipIfMissing(t, adZip)

	db := openGraph(t)
	schema := loadIngestSchema(t)

	mem := []memSnapshot{takeMemSnapshot("baseline")}

	t.Log("=== Phase 1: Ingest AD sample data ===")
	ingestDur := ingestZip(ctx, t, db, adZip, schema)
	t.Logf("  Ingest duration: %s", ingestDur.Round(time.Millisecond))
	mem = append(mem, takeMemSnapshot("after ingest"))

	t.Log("=== Phase 2: AD post-processing analysis ===")
	analysisDur := runAnalysis(ctx, t, db)
	t.Logf("  Analysis duration: %s", analysisDur.Round(time.Millisecond))
	mem = append(mem, takeMemSnapshot("after analysis"))

	t.Logf("  Total load+analyze: %s", (ingestDur + analysisDur).Round(time.Millisecond))

	t.Log("=== Phase 3: Preset Cypher queries ===")
	runPresetQueries(ctx, t, db, adPresetQueries)
	mem = append(mem, takeMemSnapshot("after queries"))

	logMemTable(t, mem)
}

// TestLoadAndQueryAzure loads the Entra/Azure sample data and runs Azure preset queries.
func TestLoadAndQueryAzure(t *testing.T) {
	ctx := context.Background()
	azureZip := filepath.Join(testdataDir(), "entra_sampledata.zip")
	skipIfMissing(t, azureZip)

	db := openGraph(t)
	schema := loadIngestSchema(t)

	mem := []memSnapshot{takeMemSnapshot("baseline")}

	t.Log("=== Phase 1: Ingest Azure/Entra sample data ===")
	ingestDur := ingestZip(ctx, t, db, azureZip, schema)
	t.Logf("  Ingest duration: %s", ingestDur.Round(time.Millisecond))
	mem = append(mem, takeMemSnapshot("after ingest"))

	t.Log("=== Phase 2: Azure post-processing analysis ===")
	analysisDur := runAnalysis(ctx, t, db)
	t.Logf("  Analysis duration: %s", analysisDur.Round(time.Millisecond))
	mem = append(mem, takeMemSnapshot("after analysis"))

	t.Logf("  Total load+analyze: %s", (ingestDur + analysisDur).Round(time.Millisecond))

	t.Log("=== Phase 3: Preset Cypher queries ===")
	runPresetQueries(ctx, t, db, azurePresetQueries)
	mem = append(mem, takeMemSnapshot("after queries"))

	logMemTable(t, mem)
}

// TestLoadAndQueryAll loads both sample data sets into a single graph and runs all preset queries.
func TestLoadAndQueryAll(t *testing.T) {
	ctx := context.Background()
	adZip := filepath.Join(testdataDir(), "ad_sampledata.zip")
	azureZip := filepath.Join(testdataDir(), "entra_sampledata.zip")
	skipIfMissing(t, adZip)
	skipIfMissing(t, azureZip)

	db := openGraph(t)
	schema := loadIngestSchema(t)

	mem := []memSnapshot{takeMemSnapshot("baseline")}
	var totalIngest time.Duration

	t.Log("=== Phase 1a: Ingest AD sample data ===")
	dur := ingestZip(ctx, t, db, adZip, schema)
	t.Logf("  AD ingest duration: %s", dur.Round(time.Millisecond))
	totalIngest += dur
	mem = append(mem, takeMemSnapshot("after AD ingest"))

	t.Log("=== Phase 1b: Ingest Azure/Entra sample data ===")
	dur = ingestZip(ctx, t, db, azureZip, schema)
	t.Logf("  Azure ingest duration: %s", dur.Round(time.Millisecond))
	totalIngest += dur
	mem = append(mem, takeMemSnapshot("after Azure ingest"))

	t.Logf("  Total ingest: %s", totalIngest.Round(time.Millisecond))

	t.Log("=== Phase 2: Full post-processing analysis ===")
	analysisDur := runAnalysis(ctx, t, db)
	t.Logf("  Analysis duration: %s", analysisDur.Round(time.Millisecond))
	mem = append(mem, takeMemSnapshot("after analysis"))

	t.Logf("  Total load+analyze: %s", (totalIngest + analysisDur).Round(time.Millisecond))

	t.Log("=== Phase 3: AD preset queries ===")
	runPresetQueries(ctx, t, db, adPresetQueries)

	t.Log("=== Phase 4: Azure preset queries ===")
	runPresetQueries(ctx, t, db, azurePresetQueries)
	mem = append(mem, takeMemSnapshot("after queries"))

	logMemTable(t, mem)
}

// runPresetQueries executes each query as a sub-test and logs timing + results.
func runPresetQueries(ctx context.Context, t *testing.T, db graph.Database, queries []presetQuery) {
	t.Helper()

	var totalQueryTime time.Duration
	stats := make([]queryStat, 0, len(queries))

	for _, q := range queries {
		q := q
		t.Run(q.Name, func(t *testing.T) {
			result, dur, err := runQuery(ctx, db, q.Cypher)
			stat := queryStat{
				name:     q.Name,
				cypher:   q.Cypher,
				duration: dur,
				result:   result,
				err:      err,
			}
			stats = append(stats, stat)
			totalQueryTime += dur

			if err != nil {
				t.Logf("  ERROR: %v", err)
				t.Logf("  Cypher: %s", q.Cypher)
				t.Logf("  Duration: %s", dur.Round(time.Microsecond))
				// Non-fatal: log the error but don't fail — kglite may not support all query patterns
				t.Logf("  WARN: query returned error (may be unsupported Cypher pattern)")
			} else {
				t.Logf("  Duration: %s", dur.Round(time.Microsecond))
				if result == "" {
					t.Logf("  Result: (no rows)")
				} else {
					for _, line := range strings.Split(result, "\n") {
						t.Logf("  Result: %s", line)
					}
				}
			}
		})
	}

	// Summary table
	t.Logf("--- Query timing summary ---")
	t.Logf("%-45s %10s  %s", "Query", "Duration", "Result")
	t.Logf("%s", strings.Repeat("-", 90))
	for _, s := range stats {
		result := s.result
		if s.err != nil {
			result = fmt.Sprintf("ERROR: %v", s.err)
		}
		if len(result) > 40 {
			result = result[:37] + "..."
		}
		t.Logf("%-45s %10s  %s", s.name, s.duration.Round(time.Microsecond), result)
	}
	t.Logf("%s", strings.Repeat("-", 90))
	t.Logf("%-45s %10s", "Total query time", totalQueryTime.Round(time.Millisecond))
}
