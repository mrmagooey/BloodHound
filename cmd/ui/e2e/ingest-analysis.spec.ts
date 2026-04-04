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

import { APIRequestContext, expect, test } from '@playwright/test';
import { E2E_ADMIN_PASSWORD, E2E_ADMIN_USERNAME } from './global-setup';

/**
 * Minimal BloodHound v6 ingest JSON for a single domain. This exercises the
 * full ingest pipeline without requiring a large collector zip.
 */
const MINIMAL_DOMAIN_JSON = JSON.stringify({
    meta: {
        methods: 0,
        type: 'domains',
        count: 1,
        version: 6,
    },
    data: [
        {
            Properties: {
                domain: 'E2ETEST.LOCAL',
                name: 'E2ETEST.LOCAL',
                distinguishedname: 'DC=E2ETEST,DC=LOCAL',
                domainsid: 'S-1-5-21-1111111111-2222222222-3333333333',
                collected: true,
                whencreated: 1700000000,
                functionallevel: '2016',
            },
            Trusts: [],
            ChildObjects: [],
            Links: [],
            ACEs: [],
            ObjectIdentifier: 'S-1-5-21-1111111111-2222222222-3333333333',
            IsDeleted: false,
            IsACLProtected: false,
        },
    ],
});

const MINIMAL_COMPUTER_JSON = JSON.stringify({
    meta: {
        methods: 0,
        type: 'computers',
        count: 1,
        version: 6,
    },
    data: [
        {
            PrimaryGroupSID: 'S-1-5-21-1111111111-2222222222-3333333333-516',
            AllowedToDelegate: [],
            AllowedToAct: [],
            HasSIDHistory: [],
            Sessions: { Results: [], Collected: false },
            PrivilegedSessions: { Results: [], Collected: false },
            RegistrySessions: { Results: [], Collected: false },
            LocalGroups: [],
            UserRights: [],
            Status: null,
            Aces: [],
            ObjectIdentifier: 'S-1-5-21-1111111111-2222222222-3333333333-1000',
            IsDeleted: false,
            IsACLProtected: false,
            Properties: {
                domain: 'E2ETEST.LOCAL',
                name: 'DC01.E2ETEST.LOCAL',
                domainsid: 'S-1-5-21-1111111111-2222222222-3333333333',
                distinguishedname: 'CN=DC01,OU=Domain Controllers,DC=E2ETEST,DC=LOCAL',
                operatingsystem: 'Windows Server 2022',
                collected: true,
            },
        },
    ],
});

/**
 * Helper: log in via the API and return the bearer session token.
 */
async function loginAndGetToken(request: APIRequestContext): Promise<string> {
    const loginResponse = await request.post('/api/v2/login', {
        data: {
            login_method: 'secret',
            username: E2E_ADMIN_USERNAME,
            secret: E2E_ADMIN_PASSWORD,
        },
    });

    expect(loginResponse.status(), 'Login should succeed').toBe(200);
    const loginBody = await loginResponse.json();
    const token = loginBody.data.session_token;
    expect(token, 'Session token should be present').toBeTruthy();
    return token;
}

/**
 * Helper: poll a condition function until it returns true, with a timeout.
 */
async function pollUntil(
    fn: () => Promise<boolean>,
    { intervalMs = 2000, timeoutMs = 60_000, description = 'condition' } = {}
): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
        if (await fn()) return;
        await new Promise((r) => setTimeout(r, intervalMs));
    }
    throw new Error(`Timed out waiting for ${description} after ${timeoutMs}ms`);
}

test.describe('Ingest and Analysis workflow', () => {
    // Increase the overall test timeout because ingest + analysis can take time.
    test.setTimeout(120_000);

    test('login, upload data, ingest, trigger analysis, and verify completion', async ({ request }) => {
        let token: string;

        await test.step('Authenticate via API login', async () => {
            token = await loginAndGetToken(request);
        });

        const authHeaders = () => ({ Authorization: `Bearer ${token!}` });

        await test.step('Verify self endpoint returns authenticated user', async () => {
            const selfResponse = await request.get('/api/v2/self', { headers: authHeaders() });
            expect(selfResponse.status()).toBe(200);
            const selfBody = await selfResponse.json();
            expect(selfBody.data).not.toBeNull();
            expect(selfBody.data.principal_name).toBe(E2E_ADMIN_USERNAME);
        });

        let jobId: number;

        await test.step('Start a file ingest job', async () => {
            const startResponse = await request.post('/api/v2/file-upload/start', {
                headers: authHeaders(),
            });
            expect(startResponse.status(), 'Start ingest job should return 201').toBe(201);
            const startBody = await startResponse.json();
            jobId = startBody.data.id;
            expect(jobId, 'Job ID should be a positive number').toBeGreaterThan(0);
        });

        await test.step('Upload domain JSON to the ingest job', async () => {
            const uploadResponse = await request.post(`/api/v2/file-upload/${jobId!}`, {
                headers: {
                    ...authHeaders(),
                    'Content-Type': 'application/json',
                },
                data: MINIMAL_DOMAIN_JSON,
            });
            expect(uploadResponse.status(), 'Domain JSON upload should return 202').toBe(202);
        });

        await test.step('Upload computer JSON to the ingest job', async () => {
            const uploadResponse = await request.post(`/api/v2/file-upload/${jobId!}`, {
                headers: {
                    ...authHeaders(),
                    'Content-Type': 'application/json',
                },
                data: MINIMAL_COMPUTER_JSON,
            });
            expect(uploadResponse.status(), 'Computer JSON upload should return 202').toBe(202);
        });

        await test.step('End the ingest job', async () => {
            const endResponse = await request.post(`/api/v2/file-upload/${jobId!}/end`, {
                headers: authHeaders(),
            });
            expect(endResponse.status(), 'End ingest job should return 200').toBe(200);
        });

        await test.step('Wait for the ingest job to finish processing', async () => {
            await pollUntil(
                async () => {
                    const statusResponse = await request.get('/api/v2/datapipe/status', {
                        headers: authHeaders(),
                    });
                    if (statusResponse.status() !== 200) return false;
                    const body = await statusResponse.json();
                    const status = body.data?.status;
                    // After ingest completes the datapipe returns to idle
                    return status === 'idle';
                },
                { timeoutMs: 60_000, description: 'datapipe to return to idle after ingest' }
            );
        });

        await test.step('Verify ingest job reached a terminal state', async () => {
            const jobsResponse = await request.get('/api/v2/file-upload', {
                headers: authHeaders(),
            });
            expect(jobsResponse.status()).toBe(200);
            const jobsBody = await jobsResponse.json();
            const jobs = jobsBody.data;
            expect(Array.isArray(jobs), 'Jobs list should be an array').toBe(true);

            const ourJob = jobs.find((j: any) => j.id === jobId);
            expect(ourJob, 'Our ingest job should appear in the list').toBeTruthy();
            // Terminal states: 2 = complete, 8 = partially_complete
            expect(
                [2, 8].includes(ourJob.status),
                `Job status should be complete (2) or partially_complete (8), got ${ourJob.status}`
            ).toBe(true);
        });

        await test.step('Request analysis', async () => {
            const analysisResponse = await request.put('/api/v2/analysis', {
                headers: authHeaders(),
            });
            expect(analysisResponse.status(), 'Request analysis should return 202').toBe(202);
        });

        await test.step('Wait for analysis to complete', async () => {
            await pollUntil(
                async () => {
                    const statusResponse = await request.get('/api/v2/datapipe/status', {
                        headers: authHeaders(),
                    });
                    if (statusResponse.status() !== 200) return false;
                    const body = await statusResponse.json();
                    const status = body.data?.status;
                    // Analysis is done when datapipe goes back to idle
                    return status === 'idle';
                },
                { timeoutMs: 60_000, description: 'analysis to complete (datapipe idle)' }
            );
        });

        await test.step('Verify datapipe has a last_complete_analysis_at timestamp', async () => {
            const statusResponse = await request.get('/api/v2/datapipe/status', {
                headers: authHeaders(),
            });
            expect(statusResponse.status()).toBe(200);
            const body = await statusResponse.json();
            const lastAnalysis = body.data?.last_complete_analysis_at;
            // The timestamp should be set (not zero/null) after a successful analysis
            expect(lastAnalysis, 'last_complete_analysis_at should be set').toBeTruthy();
            // Verify it is a real timestamp (not the Go zero time "0001-01-01T00:00:00Z")
            expect(lastAnalysis).not.toBe('0001-01-01T00:00:00Z');
        });
    });

    test('File Ingest page is accessible after login via UI', async ({ page, request }) => {
        const token = await loginAndGetToken(request);

        await test.step('Navigate to file ingest administration page', async () => {
            // Inject the session into the browser so the React app recognises us.
            // We do this by logging in through the UI form which sets Redux state.
            await page.goto('/ui/login');
            await page.waitForSelector('#username', { timeout: 15_000 });

            await page.locator('#username').fill(E2E_ADMIN_USERNAME);
            await page.locator('#password').fill(E2E_ADMIN_PASSWORD);
            await page.getByRole('button', { name: 'LOGIN' }).click();

            // After successful login the app redirects away from /login
            await page.waitForURL(/\/ui\/(?!login)/, { timeout: 15_000 });
        });

        await test.step('Navigate to Administration > File Ingest', async () => {
            await page.goto('/ui/administration/file-ingest');
            // Wait for the File Ingest page to render
            await page.waitForSelector('[data-testid="manual-file-ingest"]', { timeout: 15_000 });
            const fileIngestHeading = page.getByTestId('manual-file-ingest');
            await expect(fileIngestHeading).toBeVisible();
        });
    });
});
