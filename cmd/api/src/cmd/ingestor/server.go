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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/specterops/bloodhound/cmd/api/src/services/upload"
	"github.com/specterops/dawgs/graph"
)

const (
	queueCapacity  = 100
	maxUploadBytes = 2 << 30 // 2 GiB
)

// serverEnv holds configuration read from environment variables for server mode.
type serverEnv struct {
	GraphPath string
	APIToken  string
	Port      string
	Config    Config
}

// runServer reads environment variables, opens the graph database, and
// starts the HTTP server with a background ingest queue.
func runServer(ctx context.Context) {
	env := readServerEnv()

	if env.APIToken == "" {
		slog.Error("INGESTOR_API_TOKEN environment variable is required in server mode")
		os.Exit(1)
	}

	graphdb, ingestSchema := mustOpen(ctx, env.GraphPath)
	defer graphdb.Close(ctx)

	srv := newServer(graphdb, ingestSchema, env.APIToken, env.Config)
	if err := srv.start(ctx, env.Port); err != nil {
		slog.Error("Server error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// readServerEnv reads all server configuration from environment variables.
func readServerEnv() serverEnv {
	return serverEnv{
		GraphPath: envString("INGESTOR_GRAPH_PATH", "bloodhound.kgl"),
		APIToken:  envString("INGESTOR_API_TOKEN", ""),
		Port:      envString("INGESTOR_PORT", "8080"),
		Config: Config{
			NoAnalysis:  envBool("INGESTOR_NO_ANALYSIS", false),
			ADCSEnabled: envBool("INGESTOR_ADCS", true),
			NTLMEnabled: envBool("INGESTOR_NTLM", true),
			Citrix:      envBool("INGESTOR_CITRIX", false),
		},
	}
}

// server handles HTTP requests and manages the sequential ingest queue.
type server struct {
	graphdb      graph.Database
	ingestSchema upload.IngestSchema
	token        string
	cfg          Config
	queue        chan ingestJob
	jobCounter   atomic.Uint64
}

type ingestJob struct {
	id      string
	tmpPath string
}

func newServer(graphdb graph.Database, ingestSchema upload.IngestSchema, token string, cfg Config) *server {
	return &server{
		graphdb:      graphdb,
		ingestSchema: ingestSchema,
		token:        token,
		cfg:          cfg,
		queue:        make(chan ingestJob, queueCapacity),
	}
}

// start launches the queue worker and the HTTP server. It blocks until the
// context is cancelled or the server fails.
func (s *server) start(ctx context.Context, port string) error {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.worker(ctx)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /ingest", s.handleIngest)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("Server shutdown error", slog.String("error", err.Error()))
		}
	}()

	slog.Info("Server listening", slog.String("port", port))
	err := srv.ListenAndServe()

	// Wait for the worker to finish the current job before returning.
	wg.Wait()

	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// handleIngest authenticates the request, saves the uploaded zip to a temp
// file, and enqueues it for processing.
func (s *server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if !s.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `missing "file" field`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "bh-ingestor-upload-*")
	if err != nil {
		slog.Error("Failed to create temp file", slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		slog.Error("Failed to write upload to disk", slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	tmp.Close()

	jobID := fmt.Sprintf("%d", s.jobCounter.Add(1))
	job := ingestJob{id: jobID, tmpPath: tmp.Name()}

	select {
	case s.queue <- job:
		slog.Info("Job queued",
			slog.String("job_id", jobID),
			slog.Int("queue_depth", len(s.queue)),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]any{
			"job_id":      jobID,
			"queue_depth": len(s.queue),
		})
	default:
		os.Remove(tmp.Name())
		http.Error(w, "queue full", http.StatusTooManyRequests)
	}
}

// worker processes ingest jobs sequentially from the queue.
func (s *server) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-s.queue:
			slog.Info("Processing job", slog.String("job_id", job.id))
			if err := run(ctx, s.graphdb, job.tmpPath, s.ingestSchema, s.cfg); err != nil {
				slog.Error("Job failed",
					slog.String("job_id", job.id),
					slog.String("error", err.Error()),
				)
			} else {
				slog.Info("Job complete", slog.String("job_id", job.id))
			}
			os.Remove(job.tmpPath)
		}
	}
}

// authenticate checks the Authorization Bearer token or X-Api-Token header.
func (s *server) authenticate(r *http.Request) bool {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ") == s.token
	}
	return r.Header.Get("X-Api-Token") == s.token
}

// envString returns the value of an environment variable, or a default.
func envString(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// envBool parses a boolean environment variable, returning defaultVal if unset or unparseable.
func envBool(key string, defaultVal bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		slog.Warn("Invalid boolean env var, using default",
			slog.String("key", key),
			slog.String("value", v),
			slog.Bool("default", defaultVal),
		)
		return defaultVal
	}
	return b
}
