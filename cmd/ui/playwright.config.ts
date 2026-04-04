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

import { defineConfig, devices } from '@playwright/test';

const BHCE_BINARY = process.env.BHCE_BINARY || '/tmp/bhce';
const BASE_URL = process.env.BASE_URL || 'http://localhost:8080';

export default defineConfig({
    testDir: './e2e',
    globalSetup: './e2e/global-setup.ts',
    fullyParallel: false,
    forbidOnly: !!process.env.CI,
    retries: process.env.CI ? 2 : 0,
    workers: 1,
    reporter: 'html',
    timeout: 30_000,

    use: {
        baseURL: BASE_URL,
        trace: 'on-first-retry',
    },

    projects: [
        {
            name: 'chromium',
            use: { ...devices['Desktop Chrome'] },
        },
    ],

    webServer: {
        command: `cd /tmp/bhce-playwright-data && ${BHCE_BINARY}`,
        url: `${BASE_URL}/api/v2/self`,
        reuseExistingServer: !process.env.CI,
        timeout: 15_000,
        stdout: 'pipe',
        stderr: 'pipe',
    },
});
