// Copyright 2023 Specter Ops, Inc.
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

package services

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/specterops/bloodhound/cmd/api/src/api"
	"github.com/specterops/bloodhound/cmd/api/src/api/bolt"
	"github.com/specterops/bloodhound/cmd/api/src/api/registration"
	"github.com/specterops/bloodhound/cmd/api/src/api/router"
	"github.com/specterops/bloodhound/cmd/api/src/auth"
	"github.com/specterops/bloodhound/cmd/api/src/bootstrap"
	"github.com/specterops/bloodhound/cmd/api/src/config"
	"github.com/specterops/bloodhound/cmd/api/src/daemons"
	"github.com/specterops/bloodhound/cmd/api/src/daemons/api/bhapi"
	"github.com/specterops/bloodhound/cmd/api/src/daemons/api/toolapi"
	"github.com/specterops/bloodhound/cmd/api/src/daemons/changelog"
	"github.com/specterops/bloodhound/cmd/api/src/daemons/datapipe"
	"github.com/specterops/bloodhound/cmd/api/src/daemons/gc"
	"github.com/specterops/bloodhound/cmd/api/src/database"
	"github.com/specterops/bloodhound/cmd/api/src/migrations"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/bloodhound/cmd/api/src/model/appcfg"
	"github.com/specterops/bloodhound/cmd/api/src/queries"
	"github.com/specterops/bloodhound/cmd/api/src/services/dogtags"
	"github.com/specterops/bloodhound/cmd/api/src/services/opengraphschema"
	"github.com/specterops/bloodhound/cmd/api/src/services/upload"
	"github.com/specterops/bloodhound/cmd/api/src/database/types/null"
	"github.com/specterops/bloodhound/packages/go/cache"
	schema "github.com/specterops/bloodhound/packages/go/graphschema"
	"github.com/specterops/dawgs/graph"
	"gorm.io/gorm"
)

// ConnectPostgres initializes a connection to PG or SQLite, and returns errors if any.
// If cfg.SQLitePath is set, SQLite is used; otherwise PostgreSQL.
func ConnectPostgres(cfg config.Configuration) (*database.BloodhoundDB, error) {
	var (
		db  *gorm.DB
		err error
	)
	if cfg.SQLitePath != "" {
		slog.Info("Using SQLite database", slog.String("path", cfg.SQLitePath))
		db, err = database.OpenSQLiteDatabase(cfg.SQLitePath)
	} else {
		db, err = database.OpenDatabase(cfg.Database.PostgreSQLConnectionString())
	}
	if err != nil {
		return nil, fmt.Errorf("error while attempting to create database connection: %w", err)
	}
	return database.NewBloodhoundDB(db, auth.NewIdentityResolver(), cfg), nil
}

// ConnectDatabases initializes connections to PG and connection, and returns errors if any
func ConnectDatabases(ctx context.Context, cfg config.Configuration) (bootstrap.DatabaseConnections[*database.BloodhoundDB, *graph.DatabaseSwitch], error) {
	connections := bootstrap.DatabaseConnections[*database.BloodhoundDB, *graph.DatabaseSwitch]{}

	if db, err := ConnectPostgres(cfg); err != nil {
		return connections, err
	} else if graphDB, err := bootstrap.ConnectGraph(ctx, cfg); err != nil {
		return connections, err
	} else {
		connections.RDMS = db
		connections.Graph = graphDB

		return connections, nil
	}
}

// PreMigrationDaemons Word of caution: These daemons will be launched prior to any migration starting
func PreMigrationDaemons(ctx context.Context, cfg config.Configuration, connections bootstrap.DatabaseConnections[*database.BloodhoundDB, *graph.DatabaseSwitch]) ([]daemons.Daemon, error) {
	return []daemons.Daemon{
		toolapi.NewDaemon(ctx, connections, cfg, schema.DefaultGraphSchema()),
	}, nil
}

func Entrypoint(ctx context.Context, cfg config.Configuration, connections bootstrap.DatabaseConnections[*database.BloodhoundDB, *graph.DatabaseSwitch]) ([]daemons.Daemon, error) {

	dogtagsService := dogtags.NewDefaultService()

	slog.InfoContext(ctx, "DogTags provider initialized",
		slog.String("namespace", "dogtags"),
		slog.String("provider", dogtagsService.ProviderName()))

	flags := dogtagsService.GetAllDogTags()
	slog.InfoContext(ctx, "DogTags Configuration",
		slog.String("namespace", "dogtags"),
		slog.Any("flags", flags))

	standaloneMode := cfg.SQLitePath != ""

	if !cfg.DisableMigrations {
		if standaloneMode {
			// Standalone (SQLite) mode: run the SQLite schema migration and create the
			// default admin account on first startup.
			if err := bootstrap.MigrateDB(ctx, cfg, connections.RDMS, config.NewDefaultAdminConfiguration); err != nil {
				return nil, fmt.Errorf("rdms migration error: %w", err)
			}
		} else {
			if err := bootstrap.MigrateDB(ctx, cfg, connections.RDMS, config.NewDefaultAdminConfiguration); err != nil {
				return nil, fmt.Errorf("rdms migration error: %w", err)
			} else if err := migrations.NewGraphMigrator(connections.Graph).Migrate(ctx); err != nil {
				return nil, fmt.Errorf("graph migration error: %w", err)
			} else if err := bootstrap.PopulateExtensionData(ctx, connections.RDMS); err != nil {
				return nil, fmt.Errorf("extensions data population error: %w", err)
			}
		}
	} else if err := connections.Graph.SetDefaultGraph(ctx, schema.DefaultGraph()); err != nil {
		return nil, fmt.Errorf("no default graph found but migrations are disabled per configuration: %w", err)
	} else {
		slog.InfoContext(ctx, "Database migrations are disabled per configuration")
	}

	// Allow recreating the default admin account to help with lockouts/loading database dumps
	if !standaloneMode && cfg.RecreateDefaultAdmin {
		slog.InfoContext(ctx, "Recreating default admin user")
		if err := bootstrap.CreateDefaultAdmin(ctx, cfg, connections.RDMS, config.NewDefaultAdminConfiguration); err != nil {
			return nil, err
		}
	}

	// Remove authentication tokens if the APITokens parameter is disabled (PostgreSQL mode only)
	if !standaloneMode && !appcfg.GetAPITokensParameter(ctx, connections.RDMS) {
		slog.WarnContext(ctx, "APITokens parameter is disabled")
		if dErr := connections.RDMS.DeleteAllAuthTokens(ctx); dErr != nil {
			return nil, fmt.Errorf("failed to delete all auth tokens at startup: %w", dErr)
		}
	}

	if apiCache, err := cache.NewCache(cache.Config{MaxSize: cfg.MaxAPICacheSize}); err != nil {
		return nil, fmt.Errorf("failed to create in-memory cache for API: %w", err)
	} else if graphQueryCache, err := cache.NewCache(cache.Config{MaxSize: cfg.MaxAPICacheSize}); err != nil {
		return nil, fmt.Errorf("failed to create in-memory cache for graph queries: %w", err)
	} else if collectorManifests, err := cfg.SaveCollectorManifests(); err != nil {
		return nil, fmt.Errorf("failed to save collector manifests: %w", err)
	} else if ingestSchema, err := upload.LoadIngestSchema(); err != nil {
		return nil, fmt.Errorf("failed to load OpenGraph schema: %w", err)
	} else {
		startDelay := 0 * time.Second

		var (
			cl                     = changelog.NewChangelog(connections.Graph, connections.RDMS, changelog.DefaultOptions())
			pipeline               = datapipe.NewPipeline(ctx, cfg, connections.RDMS, connections.Graph, graphQueryCache, ingestSchema, cl)
			graphQuery             = queries.NewGraphQuery(connections.Graph, graphQueryCache, cfg)
			authorizer             = auth.NewAuthorizer(connections.RDMS)
			datapipeDaemon         = datapipe.NewDaemon(pipeline, startDelay, time.Duration(cfg.DatapipeInterval)*time.Second, connections.RDMS)
			routerInst             = router.NewRouter(cfg, authorizer, fmt.Sprintf(bootstrap.ContentSecurityPolicy, "", "", "", "", "", ""))
			authenticator          = api.NewAuthenticator(cfg, connections.RDMS, api.NewAuthExtensions(cfg, connections.RDMS))
			openGraphSchemaService = opengraphschema.NewOpenGraphSchemaService(connections.RDMS, connections.Graph)
		)

		registration.RegisterFossGlobalMiddleware(&routerInst, cfg, auth.NewIdentityResolver(), authenticator, connections.RDMS)
		registration.RegisterFossRoutes(&routerInst, cfg, connections.RDMS, connections.Graph, graphQuery, apiCache, collectorManifests, authenticator, authorizer, ingestSchema, dogtagsService, openGraphSchemaService)

		if standaloneMode {
			// In standalone mode, kglite does not persist node properties (other than "name")
			// across save/load cycles. If retained ingest files exist and the graph has no
			// node properties, schedule re-ingest tasks so the datapipe can rebuild the graph
			// on its first tick (which fires immediately at startup with startDelay=0).
			if err := scheduleRehydrationIfNeeded(ctx, cfg, connections); err != nil {
				slog.WarnContext(ctx, fmt.Sprintf("startup graph rehydration check failed: %v", err))
			}
		} else {
			// Set neo4j batch and flush sizes from database parameters
			neo4jParameters := appcfg.GetNeo4jParameters(ctx, connections.RDMS)
			connections.Graph.SetBatchWriteSize(neo4jParameters.BatchWriteSize)
			connections.Graph.SetWriteFlushSize(neo4jParameters.WriteFlushSize)

			// Trigger analysis on first start
			if err := connections.RDMS.RequestAnalysis(ctx, "init"); err != nil {
				slog.WarnContext(ctx, fmt.Sprintf("failed to request init analysis: %v", err))
			}
		}

		boltAddr := fmt.Sprintf(":%d", bolt.DefaultBoltPort)
		boltDaemon := bolt.NewDaemon(boltAddr, graphQuery, connections.RDMS)

		return []daemons.Daemon{
			bhapi.NewDaemon(cfg, routerInst.Handler()),
			boltDaemon,
			gc.NewDataPruningDaemon(connections.RDMS),
			cl,
			datapipeDaemon,
		}, nil
	}
}

// scheduleRehydrationIfNeeded checks whether graph node properties were lost when kglite loaded
// from disk (a known kglite limitation: only the "name" property survives save/load). If retained
// ingest files exist and the graph has no objectid properties, it creates synthetic ingest tasks
// so the datapipe will re-ingest the files on its first tick and restore the graph.
func scheduleRehydrationIfNeeded(
	ctx context.Context,
	cfg config.Configuration,
	connections bootstrap.DatabaseConnections[*database.BloodhoundDB, *graph.DatabaseSwitch],
) error {
	// Check whether any retained ingest files exist.
	retainedDir := cfg.RetainedFilesDirectory()
	entries, err := os.ReadDir(retainedDir)
	if err != nil || len(entries) == 0 {
		return nil // nothing retained — either first run or retention not yet active
	}

	// If there are already pending ingest tasks (from a previous incomplete startup
	// re-ingest or an in-progress upload), skip to avoid duplicating work.
	if taskCount, err := connections.RDMS.CountAllIngestTasks(ctx); err == nil && taskCount > 0 {
		return nil
	}

	// Check whether the graph already has nodes with node properties populated.
	// After a clean in-session ingest, objectid is set. After a kglite save/load it is nil.
	hasProps, err := graphHasNodeProperties(ctx, connections.Graph)
	if err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("could not check graph properties, skipping rehydration: %v", err))
		return nil
	}
	if hasProps {
		return nil // graph properties are intact — no re-ingest needed
	}

	slog.InfoContext(ctx, "Graph properties missing after load — scheduling startup re-ingest from retained files",
		slog.Int("file_count", len(entries)))

	// Create a synthetic ingest job to hold the re-ingest tasks.
	job, err := connections.RDMS.CreateIngestJob(ctx, model.IngestJob{
		Status:     model.JobStatusIngesting,
		StartTime:  time.Now().UTC(),
		LastIngest: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("creating rehydration ingest job: %w", err)
	}

	// Create one task per retained file. Retained files are individual JSON files
	// (ZIPs are extracted before retention, so all retained files are JSON).
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filePath := filepath.Join(retainedDir, entry.Name())
		if _, err := connections.RDMS.CreateIngestTask(ctx, model.IngestTask{
			StoredFileName:   filePath,
			OriginalFileName: entry.Name(),
			FileType:         model.FileTypeJson,
			JobId:            null.Int64From(job.ID),
		}); err != nil {
			slog.WarnContext(ctx, "Failed to create rehydration ingest task",
				slog.String("file", filePath),
				slog.String("error", err.Error()))
		}
	}

	return nil
}

// graphHasNodeProperties returns true if the graph contains at least one node whose
// objectid property is non-nil. After a kglite save/load cycle, node properties
// (other than name) are nil, so this returns false until after a fresh ingest.
func graphHasNodeProperties(ctx context.Context, graphDB graph.Database) (bool, error) {
	var hasProps bool

	err := graphDB.ReadTransaction(ctx, func(tx graph.Transaction) error {
		result := tx.Raw("MATCH (n) WHERE n.objectid IS NOT NULL RETURN count(n) AS c LIMIT 1", nil)
		defer result.Close()
		if result.Error() != nil {
			return result.Error()
		}
		if result.Next() {
			if v, ok := result.Values()[0].(int64); ok && v > 0 {
				hasProps = true
			}
		}
		return result.Error()
	})

	return hasProps, err
}
