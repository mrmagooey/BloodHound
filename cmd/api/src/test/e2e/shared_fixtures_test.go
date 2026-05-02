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

//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/specterops/bloodhound/cmd/api/src/analysis/ad"
	"github.com/specterops/bloodhound/cmd/api/src/analysis/azure"
	"github.com/specterops/bloodhound/cmd/api/src/services/upload"
	"github.com/specterops/bloodhound/packages/go/analysis"
	kglitedawgs "github.com/specterops/bloodhound/packages/go/kglite/dawgs"
	"github.com/specterops/dawgs/graph"
)

// Package-level shared graph databases, loaded once in TestMain.
// nil means the dataset was not available (zip missing).
var (
	fixtureADGraph       graph.Database
	fixtureAzureGraph    graph.Database
	fixtureCombinedGraph graph.Database
	fixtureKNexusGraph   graph.Database

	// Temp directories for shared graphs (cleaned up after tests).
	fixtureTempDirs []string
)

// sharedADGraph returns the pre-loaded AD graph with analysis already run.
// Skips the test if the AD dataset was not available.
func sharedADGraph(t *testing.T) graph.Database {
	t.Helper()
	if fixtureADGraph == nil {
		t.Skip("AD dataset not available (run: make e2e-testdata)")
	}
	return fixtureADGraph
}

// sharedAzureGraph returns the pre-loaded Azure graph with analysis already run.
// Skips the test if the Azure dataset was not available.
func sharedAzureGraph(t *testing.T) graph.Database {
	t.Helper()
	if fixtureAzureGraph == nil {
		t.Skip("Azure dataset not available (run: make e2e-testdata)")
	}
	return fixtureAzureGraph
}

// sharedCombinedGraph returns the pre-loaded AD+Azure graph with analysis already run.
// Skips the test if either dataset was not available.
func sharedCombinedGraph(t *testing.T) graph.Database {
	t.Helper()
	if fixtureCombinedGraph == nil {
		t.Skip("AD+Azure datasets not both available (run: make e2e-testdata)")
	}
	return fixtureCombinedGraph
}

// sharedKNexusGraph returns the pre-loaded k-nexus-global graph with analysis already run.
// Skips the test if the k-nexus dataset was not available.
func sharedKNexusGraph(t *testing.T) graph.Database {
	t.Helper()
	if fixtureKNexusGraph == nil {
		t.Skip("k-nexus-global dataset not available (run: make e2e-testdata)")
	}
	return fixtureKNexusGraph
}

// TestMain loads all available datasets into shared graphs exactly once,
// runs analysis on each, then executes all tests.
func TestMain(m *testing.M) {
	ctx := context.Background()

	schema, err := upload.LoadIngestSchema()
	if err != nil {
		log.Fatalf("failed to load ingest schema: %v", err)
	}

	tdDir := testdataDir()
	adZip := filepath.Join(tdDir, "ad_sampledata.zip")
	azureZip := filepath.Join(tdDir, "entra_sampledata.zip")
	knexusZip := filepath.Join(tdDir, "k-nexusglobal_sampledata.zip")

	adAvailable := fileExists(adZip)
	azureAvailable := fileExists(azureZip)
	knexusAvailable := fileExists(knexusZip)

	// Load AD graph
	if adAvailable {
		db, dir, err := openFixtureGraph("ad")
		if err != nil {
			log.Fatalf("failed to open AD fixture graph: %v", err)
		}
		fixtureTempDirs = append(fixtureTempDirs, dir)

		if err := ingestFixtureZip(ctx, db, adZip, schema, false); err != nil {
			log.Fatalf("failed to ingest AD data: %v", err)
		}
		if err := maybeRunFixtureAnalysis(ctx, db); err != nil {
			log.Fatalf("failed to run AD analysis: %v", err)
		}
		fixtureADGraph = db
		log.Println("shared fixture: AD graph loaded and analyzed")
	} else {
		log.Println("shared fixture: AD dataset not found, skipping")
	}

	// Load Azure graph
	if azureAvailable {
		db, dir, err := openFixtureGraph("azure")
		if err != nil {
			log.Fatalf("failed to open Azure fixture graph: %v", err)
		}
		fixtureTempDirs = append(fixtureTempDirs, dir)

		if err := ingestFixtureZip(ctx, db, azureZip, schema, false); err != nil {
			log.Fatalf("failed to ingest Azure data: %v", err)
		}
		if err := maybeRunFixtureAnalysis(ctx, db); err != nil {
			log.Fatalf("failed to run Azure analysis: %v", err)
		}
		fixtureAzureGraph = db
		log.Println("shared fixture: Azure graph loaded and analyzed")
	} else {
		log.Println("shared fixture: Azure dataset not found, skipping")
	}

	// Load Combined AD+Azure graph (skip if BH_SKIP_COMBINED=1 to save time)
	if adAvailable && azureAvailable && os.Getenv("BH_SKIP_COMBINED") != "1" {
		db, dir, err := openFixtureGraph("combined")
		if err != nil {
			log.Fatalf("failed to open Combined fixture graph: %v", err)
		}
		fixtureTempDirs = append(fixtureTempDirs, dir)

		if err := ingestFixtureZip(ctx, db, adZip, schema, false); err != nil {
			log.Fatalf("failed to ingest AD data into combined graph: %v", err)
		}
		if err := ingestFixtureZip(ctx, db, azureZip, schema, false); err != nil {
			log.Fatalf("failed to ingest Azure data into combined graph: %v", err)
		}
		if err := maybeRunFixtureAnalysis(ctx, db); err != nil {
			log.Fatalf("failed to run combined analysis: %v", err)
		}
		fixtureCombinedGraph = db
		log.Println("shared fixture: Combined AD+Azure graph loaded and analyzed")
	} else {
		log.Println("shared fixture: Combined dataset not fully available, skipping")
	}

	// Load K-Nexus graph
	if knexusAvailable && os.Getenv("BH_SKIP_KNEXUS") != "1" {
		db, dir, err := openFixtureGraph("knexus")
		if err != nil {
			log.Fatalf("failed to open K-Nexus fixture graph: %v", err)
		}
		fixtureTempDirs = append(fixtureTempDirs, dir)

		if err := ingestFixtureZip(ctx, db, knexusZip, schema, true); err != nil {
			// K-Nexus ingestion uses tolerant mode — log but don't fail
			log.Printf("shared fixture: K-Nexus ingest had batch errors (non-fatal): %v", err)
		}
		if err := maybeRunFixtureAnalysis(ctx, db); err != nil {
			log.Fatalf("failed to run K-Nexus analysis: %v", err)
		}
		fixtureKNexusGraph = db
		log.Println("shared fixture: K-Nexus graph loaded and analyzed")
	} else if os.Getenv("BH_SKIP_KNEXUS") == "1" {
		log.Println("shared fixture: K-Nexus skipped (BH_SKIP_KNEXUS=1)")
	} else {
		log.Println("shared fixture: K-Nexus dataset not found, skipping")
	}

	// Run all tests
	code := m.Run()

	// Cleanup: close graphs and remove temp dirs
	cleanupFixtures(ctx)

	os.Exit(code)
}

// fileExists returns true if the path exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// openFixtureGraph creates a temp directory and opens a kglite graph in it.
// Returns the database, the temp directory path (for cleanup), and any error.
func openFixtureGraph(name string) (graph.Database, string, error) {
	dir, err := os.MkdirTemp("", fmt.Sprintf("bh-e2e-fixture-%s-*", name))
	if err != nil {
		return nil, "", fmt.Errorf("create temp dir for %s: %w", name, err)
	}
	graphPath := filepath.Join(dir, "test.kgl")
	db, err := kglitedawgs.Open(graphPath)
	if err != nil {
		os.RemoveAll(dir)
		return nil, "", fmt.Errorf("open kglite graph for %s: %w", name, err)
	}
	return db, dir, nil
}

// ingestFixtureZip ingests a zip into the given graph. If tolerant is true, batch
// errors are returned as non-nil error but do not prevent the graph from being used.
// If tolerant is false, any batch error is returned as a hard failure.
func ingestFixtureZip(ctx context.Context, db graph.Database, zipPath string, schema upload.IngestSchema, tolerant bool) error {
	_, err := doIngestZip(ctx, db, zipPath, schema)
	if err != nil && !tolerant {
		return fmt.Errorf("ingest %s: %w", filepath.Base(zipPath), err)
	}
	if err != nil && tolerant {
		// Return the error for logging, but caller should not treat it as fatal
		return err
	}
	return nil
}

// runFixtureAnalysis runs AD and Azure post-processing on the given graph.
func runFixtureAnalysis(ctx context.Context, db graph.Database) error {
	counter := analysis.NewCompositionCounter()
	if _, err := ad.Post(ctx, db, true, false, true, &counter); err != nil {
		return fmt.Errorf("AD post-processing: %w", err)
	}
	if _, err := azure.Post(ctx, db); err != nil {
		return fmt.Errorf("Azure post-processing: %w", err)
	}
	return nil
}

// maybeRunFixtureAnalysis honors BH_SKIP_ANALYSIS=1 to bypass post-processing.
// Use this when investigating ingest-time divergences (per-label counts, edge
// inventory) where the analysis step is irrelevant or known to deadlock.
func maybeRunFixtureAnalysis(ctx context.Context, db graph.Database) error {
	if os.Getenv("BH_SKIP_ANALYSIS") == "1" {
		log.Println("shared fixture: skipping analysis (BH_SKIP_ANALYSIS=1)")
		return nil
	}
	return runFixtureAnalysis(ctx, db)
}

// cleanupFixtures closes all shared graphs and removes temp directories.
func cleanupFixtures(ctx context.Context) {
	for _, db := range []graph.Database{fixtureADGraph, fixtureAzureGraph, fixtureCombinedGraph, fixtureKNexusGraph} {
		if db != nil {
			db.Close(ctx)
		}
	}
	for _, dir := range fixtureTempDirs {
		os.RemoveAll(dir)
	}
}
