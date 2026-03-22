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

package main

import (
	"archive/zip"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/specterops/bloodhound/cmd/api/src/analysis/ad"
	"github.com/specterops/bloodhound/cmd/api/src/analysis/azure"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/bloodhound/cmd/api/src/services/graphify"
	"github.com/specterops/bloodhound/cmd/api/src/services/graphify/endpoint"
	"github.com/specterops/bloodhound/cmd/api/src/services/upload"
	"github.com/specterops/bloodhound/packages/go/analysis"
	"github.com/specterops/bloodhound/packages/go/bomenc"
	schema "github.com/specterops/bloodhound/packages/go/graphschema"
	"github.com/specterops/dawgs"
	"github.com/specterops/dawgs/drivers/neo4j"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/util/size"
)

func main() {
	var (
		filePath    string
		neo4jURL    string
		doAnalysis  bool
		skipSchema  bool
		adcsEnabled bool
		ntlmEnabled bool
		citrix      bool
	)

	flag.StringVar(&filePath, "file", "", "Path to .zip or .json file to ingest (required)")
	flag.StringVar(&neo4jURL, "neo4j", "neo4j://neo4j:bloodhound@localhost:7687", "Neo4j/Bolt-compatible connection URL")
	flag.BoolVar(&doAnalysis, "no-analysis", false, "Skip post-processing analysis after ingestion")
	flag.BoolVar(&skipSchema, "skip-schema", false, "Skip schema assertion (use for Bolt-compatible databases that don't support Neo4j index syntax, e.g. Memgraph)")
	flag.BoolVar(&adcsEnabled, "adcs", true, "Enable ADCS attack path analysis (requires --analysis)")
	flag.BoolVar(&ntlmEnabled, "ntlm", true, "Enable NTLM relay path analysis (requires --analysis)")
	flag.BoolVar(&citrix, "citrix", false, "Enable Citrix session analysis (requires --analysis)")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if filePath == "" {
		slog.Error("--file is required")
		flag.Usage()
		os.Exit(1)
	}

	if _, err := os.Stat(filePath); err != nil {
		slog.Error("File not found", "file", filePath, "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	slog.Info("Connecting to Neo4j", "url", neo4jURL)
	graphdb, err := dawgs.Open(ctx, neo4j.DriverName, dawgs.Config{
		GraphQueryMemoryLimit: 2 * size.Gibibyte,
		ConnectionString:      neo4jURL,
	})
	if err != nil {
		slog.Error("Failed to connect to Neo4j", "error", err)
		os.Exit(1)
	}
	defer graphdb.Close(ctx)

	if !skipSchema {
		slog.Info("Asserting graph schema")
		if err := graphdb.AssertSchema(ctx, schema.DefaultGraphSchema()); err != nil {
			slog.Error("Failed to assert schema", "error", err)
			os.Exit(1)
		}
	}

	ingestSchema, err := upload.LoadIngestSchema()
	if err != nil {
		slog.Error("Failed to load ingest schema", "error", err)
		os.Exit(1)
	}

	if err := run(ctx, graphdb, filePath, ingestSchema, !doAnalysis, adcsEnabled, ntlmEnabled, citrix, skipSchema); err != nil {
		slog.Error("Ingest failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, graphdb graph.Database, filePath string, ingestSchema upload.IngestSchema, doAnalysis, adcsEnabled, ntlmEnabled, citrix, skipSchema bool) error {
	fileType := model.FileTypeJson
	if strings.HasSuffix(strings.ToLower(filePath), ".zip") {
		fileType = model.FileTypeZip
	}

	resolver := endpoint.NewResolver(graphdb)
	ic := graphify.NewIngestContext(ctx,
		graphify.WithIngestTime(time.Now()),
		graphify.WithEndpointResolver(resolver),
	)

	readOpts := graphify.ReadOptions{
		FileType:           fileType,
		IngestSchema:       ingestSchema,
		RegisterSourceKind: makeRegisterFn(ctx, graphdb),
	}

	slog.Info("Starting ingestion", "file", filePath, "type", fileType)
	start := time.Now()

	if err := graphdb.BatchOperation(ctx, func(batch graph.Batch) error {
		ic.BindBatchUpdater(batch)
		if fileType == model.FileTypeZip {
			return ingestZip(ctx, ic, filePath, readOpts)
		}
		return ingestJSON(ctx, ic, filePath, readOpts)
	}); err != nil {
		return fmt.Errorf("ingestion error: %w", err)
	}

	nodesProcessed, relsProcessed, nodesWritten, relsWritten := ic.Stats.GetCounts()
	slog.Info("Ingestion complete",
		"duration", time.Since(start).Round(time.Millisecond),
		"nodes_processed", nodesProcessed,
		"nodes_written", nodesWritten,
		"relationships_processed", relsProcessed,
		"relationships_written", relsWritten,
	)

	if doAnalysis {
		return runAnalysis(ctx, graphdb, adcsEnabled, ntlmEnabled, citrix)
	}
	return nil
}

func ingestJSON(ctx context.Context, ic *graphify.IngestContext, path string, readOpts graphify.ReadOptions) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()
	return graphify.ReadFileForIngest(ic, file, readOpts)
}

func ingestZip(ctx context.Context, ic *graphify.IngestContext, zipPath string, readOpts graphify.ReadOptions) error {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer archive.Close()

	var firstErr error
	for _, f := range archive.File {
		if f.FileInfo().IsDir() {
			continue
		}

		tmpPath, err := extractZipEntryToTemp(f)
		if err != nil {
			slog.Error("Failed to extract zip entry", "name", f.Name, "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		if err := processZipEntry(ctx, ic, tmpPath, f.Name, readOpts); err != nil {
			slog.Error("Failed to ingest zip entry", "name", f.Name, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func processZipEntry(ctx context.Context, ic *graphify.IngestContext, tmpPath, name string, readOpts graphify.ReadOptions) error {
	file, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("open temp file for %s: %w", name, err)
	}
	defer func() {
		file.Close()
		os.Remove(tmpPath)
	}()

	slog.Debug("Processing zip entry", "name", name)
	return graphify.ReadFileForIngest(ic, file, readOpts)
}

func extractZipEntryToTemp(f *zip.File) (string, error) {
	tmp, err := os.CreateTemp("", "bh-ingestor-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	success := false
	defer func() {
		tmp.Close()
		if !success {
			os.Remove(tmp.Name())
		}
	}()

	srcFile, err := f.Open()
	if err != nil {
		return "", fmt.Errorf("open zip entry: %w", err)
	}
	defer srcFile.Close()

	normalized, err := bomenc.NormalizeToUTF8(srcFile)
	if err != nil {
		return "", fmt.Errorf("normalize encoding: %w", err)
	}

	if _, err := io.Copy(tmp, normalized); err != nil {
		return "", fmt.Errorf("copy zip entry: %w", err)
	}

	success = true
	return tmp.Name(), nil
}

func makeRegisterFn(ctx context.Context, graphdb graph.Database) func(graph.Kind) error {
	return func(kind graph.Kind) error {
		return graphdb.RefreshKinds(ctx)
	}
}

func runAnalysis(ctx context.Context, graphdb graph.Database, adcsEnabled, ntlmEnabled, citrix bool) error {
	slog.Info("Running post-processing analysis")
	start := time.Now()

	counter := analysis.NewCompositionCounter()
	if _, err := ad.Post(ctx, graphdb, adcsEnabled, citrix, ntlmEnabled, &counter); err != nil {
		return fmt.Errorf("AD post-processing: %w", err)
	}

	if _, err := azure.Post(ctx, graphdb); err != nil {
		return fmt.Errorf("Azure post-processing: %w", err)
	}

	slog.Info("Analysis complete", "duration", time.Since(start).Round(time.Millisecond))
	return nil
}
