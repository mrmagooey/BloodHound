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

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/specterops/bloodhound/cmd/api/src/bootstrap"
	"github.com/specterops/bloodhound/cmd/api/src/config"
	"github.com/specterops/bloodhound/cmd/api/src/database"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/bloodhound/cmd/api/src/model/appcfg"
	"github.com/specterops/bloodhound/cmd/api/src/services"
	"github.com/specterops/bloodhound/cmd/api/src/version"
	"github.com/specterops/bloodhound/packages/go/bhlog"
	"github.com/specterops/bloodhound/packages/go/bhlog/attr"
	"github.com/specterops/bloodhound/packages/go/bhlog/level"
	"github.com/specterops/dawgs/graph"
)

func printVersion() {
	fmt.Printf("Bloodhound API Version: %s\n", version.GetVersion())
	os.Exit(0)
}

// resetPassword opens the SQLite database, looks up the admin user, generates a new
// random password, hashes it with Argon2, stores it, then prints the new password.
func resetPassword(cfg config.Configuration) {
	ctx := context.Background()

	principalName := cfg.DefaultAdmin.PrincipalName
	if principalName == "" {
		principalName = "admin"
	}

	db, err := services.ConnectPostgres(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close(ctx)

	user, err := db.LookupUser(ctx, principalName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to look up user %q: %v\n", principalName, err)
		os.Exit(1)
	}

	newPassword, err := config.GenerateSecureRandomString(32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to generate password: %v\n", err)
		os.Exit(1)
	}

	secretDigester := cfg.Crypto.Argon2.NewDigester()
	secretDigest, err := secretDigester.Digest(newPassword)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to hash password: %v\n", err)
		os.Exit(1)
	}

	if user.AuthSecret == nil {
		// No existing secret — create one
		newSecret := model.AuthSecret{
			UserID:       user.ID,
			Digest:       secretDigest.String(),
			DigestMethod: secretDigester.Method(),
			ExpiresAt:    time.Now().Add(appcfg.GetPasswordExpiration(ctx, db)),
		}
		if _, err := db.CreateAuthSecret(ctx, newSecret); err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to create auth secret: %v\n", err)
			os.Exit(1)
		}
	} else {
		user.AuthSecret.Digest = secretDigest.String()
		user.AuthSecret.DigestMethod = secretDigester.Method()
		user.AuthSecret.ExpiresAt = time.Now().Add(appcfg.GetPasswordExpiration(ctx, db))
		if err := db.UpdateAuthSecret(ctx, *user.AuthSecret); err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to update auth secret: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("Password reset for user %q\nNew password: %s\n", principalName, newPassword)
}

func main() {
	var (
		configFilePath string
		versionFlag    bool
		resetpwFlag    bool
		sqlitePath     string
	)

	// Eagerly set logging format if valid environment variable is set
	bhlog.ConfigureDefaultJSON(os.Stdout)
	if config.GetTextLoggerEnabled() {
		bhlog.ConfigureDefaultText(os.Stdout)
	}

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "BloodHound Community Edition API Server\n\nUsage of %s\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.BoolVar(&versionFlag, "version", false, "Get binary version.")
	flag.BoolVar(&resetpwFlag, "resetpw", false, "Reset the admin user password, print the new password to stdout, then exit.")
	flag.StringVar(&configFilePath, "configfile", bootstrap.DefaultConfigFilePath(), "Configuration file to load.")
	flag.StringVar(&sqlitePath, "sqlite-path", "", "Path to SQLite database file. Enables standalone mode (overrides config file).")
	flag.Parse()

	if versionFlag {
		printVersion()
	}

	cfg, err := config.GetConfiguration(configFilePath, config.NewDefaultConfiguration)
	if err != nil {
		slog.Error(fmt.Sprintf("Unable to read configuration %s: %v", configFilePath, err))
		os.Exit(1)
	}

	// Override SQLite path if provided via CLI flag.
	if sqlitePath != "" {
		cfg.SQLitePath = sqlitePath
	}

	// Handle --resetpw: reset the admin password and exit without starting the server.
	if resetpwFlag {
		resetPassword(cfg)
		os.Exit(0)
	}

	// Initialize logging
	var (
		logFile *os.File

		logLevel            = slog.LevelInfo
		logWriter io.Writer = os.Stdout
	)

	if cfg.LogPath != "" {
		logFile, err = os.OpenFile(cfg.LogPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			slog.Error(
				"Failed to configure logging to file",
				slog.String("path", cfg.LogPath),
				attr.Error(err),
			)
		} else {
			defer logFile.Close()
			slog.Info("Additionally logging to file", slog.String("log_file", cfg.LogPath))
			logWriter = io.MultiWriter(logWriter, logFile)
		}
	}

	if cfg.LogLevel != "" {
		if parsedLevel, err := bhlog.ParseLevel(cfg.LogLevel); err != nil {
			slog.Warn("Configured log level is invalid. Ignoring.", slog.String("requested_log_level", cfg.LogLevel))
		} else {
			logLevel = parsedLevel
		}
	}

	if cfg.EnableTextLogger {
		bhlog.ConfigureDefaultText(logWriter)
	} else {
		bhlog.ConfigureDefaultJSON(logWriter)
	}

	level.SetGlobalLevel(logLevel)
	slog.Info("Logging configured", slog.String("log_level", logLevel.String()))

	initializer := bootstrap.Initializer[*database.BloodhoundDB, *graph.DatabaseSwitch]{
		Configuration:       cfg,
		DBConnector:         services.ConnectDatabases,
		PreMigrationDaemons: services.PreMigrationDaemons,
		Entrypoint:          services.Entrypoint,
	}

	if err := initializer.Launch(context.Background(), true); err != nil {
		slog.Error(fmt.Sprintf("Failed starting the server: %v", err))
		os.Exit(1)
	}
}
