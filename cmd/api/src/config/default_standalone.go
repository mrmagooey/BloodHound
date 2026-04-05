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

//go:build standalone

package config

import (
	"fmt"
	"path/filepath"

	"github.com/specterops/bloodhound/cmd/api/src/serde"
)

func NewDefaultAdminConfiguration() (DefaultAdminConfiguration, error) {
	if generatedPassword, err := GenerateSecureRandomString(32); err != nil {
		return DefaultAdminConfiguration{}, fmt.Errorf("failed to generate default password: %w", err)
	} else {
		return DefaultAdminConfiguration{
			PrincipalName: "admin",
			Password:      generatedPassword,
			EmailAddress:  "spam@example.com",
			FirstName:     "Admin",
			LastName:      "User",
			ExpireNow:     false,
		}, nil
	}
}

// NewDefaultConfiguration returns a Configuration with standalone-friendly defaults.
// All paths are relative to "./data" so the binary can run from any directory.
func NewDefaultConfiguration() (Configuration, error) {
	if jwtSigningKey, err := GenerateRandomBase64String(32); err != nil {
		return Configuration{}, fmt.Errorf("failed to generate JWT signing key: %w", err)
	} else {
		workDir := filepath.Join(".", "data")

		return Configuration{
			Version:                         0,
			BindAddress:                     "0.0.0.0:8080",
			SlowQueryThreshold:              100,
			MaxGraphQueryCacheSize:          100,
			MaxAPICacheSize:                 200,
			MetricsPort:                     ":2112",
			RootURL:                         serde.MustParseURL("http://127.0.0.1:8080/"),
			WorkDir:                         workDir,
			LogLevel:                        "INFO",
			CollectorsBasePath:              filepath.Join(workDir, "collectors"),
			CollectorsBucketURL:             serde.MustParseURL("https://bhe-hound-artifacts.s3.amazonaws.com/"),
			DatapipeInterval:                5,
			EnableStartupWaitPeriod:         true,
			EnableAPILogging:                true,
			DisableAnalysis:                 false,
			DisableCypherComplexityLimit:    false,
			DisableIngest:                   false,
			DisableMigrations:               false,
			EnableCypherMutations:           false,
			RecreateDefaultAdmin:            false,
			ForceDownloadEmbeddedCollectors: false,
			GraphQueryMemoryLimit:           2,
			EnableTextLogger:                false,
			TLS:                             TLSConfiguration{},
			SAML:                            SAMLConfiguration{},
			GraphDriver:                     "kglite",
			GraphPath:                       filepath.Join(workDir, "graph.db"),
			SQLitePath:                      filepath.Join(workDir, "bloodhound.db"),
			Database: DatabaseConfiguration{
				MaxConcurrentSessions: 10,
			},
			Neo4J: DatabaseConfiguration{
				MaxConcurrentSessions: 10,
			},
			Crypto: CryptoConfiguration{
				JWT: JWTConfiguration{
					SigningKey: jwtSigningKey,
				},
				Argon2: Argon2Configuration{
					MemoryKibibytes: 1024 * 1024 * 1,
					NumIterations:   1,
					NumThreads:      8,
				},
			},
			EnableUserAnalytics:  false,
			EnableAuditLogStdout: false,
		}, nil
	}
}
