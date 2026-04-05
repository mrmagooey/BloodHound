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

import { mkdirSync, rmSync, writeFileSync } from 'fs';

/** Well-known admin credentials used by all E2E tests. */
export const E2E_ADMIN_USERNAME = 'admin';
export const E2E_ADMIN_PASSWORD = 'E2ETestPassword123!';

/**
 * Global setup for Playwright E2E tests.
 * Creates a fresh temporary data directory for the standalone binary and writes
 * a configuration file that seeds a default admin account with a known password.
 */
export default function globalSetup() {
    const dataDir = '/tmp/bhce-playwright-data';
    rmSync(dataDir, { recursive: true, force: true });
    mkdirSync(dataDir, { recursive: true });

    // Pre-create the data directories the server expects. When a server from a
    // previous run is reused (reuseExistingServer), the rmSync above removes its
    // working directories. Creating them here prevents "no such file or directory"
    // errors from the running server.
    const dataSubDir = `${dataDir}/data`;
    for (const sub of ['tmp', 'retained', 'client_logs', 'collectors']) {
        mkdirSync(`${dataSubDir}/${sub}`, { recursive: true });
    }

    // Write a config file so the standalone binary creates an admin user with
    // a deterministic password that tests can use to authenticate.
    const config = {
        work_dir: `${dataDir}/data`,
        sqlite_path: `${dataDir}/data/bloodhound.db`,
        graph_path: `${dataDir}/data/graph.db`,
        collectors_base_path: `${dataDir}/data/collectors`,
        // Use a short datapipe interval so ingest jobs are picked up quickly.
        // The default (60s) is too slow for E2E tests that upload data and then
        // poll for completion.
        datapipe_interval: 1,
        default_admin: {
            principal_name: E2E_ADMIN_USERNAME,
            password: E2E_ADMIN_PASSWORD,
            email_address: 'admin@e2e.test',
            first_name: 'E2E',
            last_name: 'Admin',
            expire_now: false,
        },
    };

    writeFileSync(`${dataDir}/bloodhound.config.json`, JSON.stringify(config, null, 2));
}
