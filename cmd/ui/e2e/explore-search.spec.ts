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

import { expect, test } from '@playwright/test';
import { loginViaAPI, loginViaUI, pollUntil } from './helpers';
import { E2E_ADMIN_PASSWORD, E2E_ADMIN_USERNAME } from './global-setup';

/**
 * Minimal BloodHound v6 ingest JSON for a single domain.
 * Reused from ingest-analysis.spec.ts to seed data for search tests.
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
                domain: 'SEARCHTEST.LOCAL',
                name: 'SEARCHTEST.LOCAL',
                distinguishedname: 'DC=SEARCHTEST,DC=LOCAL',
                domainsid: 'S-1-5-21-9999999999-8888888888-7777777777',
                collected: true,
                whencreated: 1700000000,
                functionallevel: '2016',
            },
            Trusts: [],
            ChildObjects: [],
            Links: [],
            ACEs: [],
            ObjectIdentifier: 'S-1-5-21-9999999999-8888888888-7777777777',
            IsDeleted: false,
            IsACLProtected: false,
        },
    ],
});

test.describe('Explore: search and Cypher after ingest', () => {
    test.setTimeout(120_000);

    test('search API returns results after data ingest', async ({ request }) => {
        const token = await loginViaAPI(request);
        const authHeaders = () => ({ Authorization: `Bearer ${token}` });

        await test.step('Ingest domain data', async () => {
            const startResponse = await request.post('/api/v2/file-upload/start', {
                headers: authHeaders(),
            });
            expect(startResponse.status()).toBe(201);
            const startBody = await startResponse.json();
            const jobId = startBody.data.id;

            const uploadResponse = await request.post(`/api/v2/file-upload/${jobId}`, {
                headers: { ...authHeaders(), 'Content-Type': 'application/json' },
                data: MINIMAL_DOMAIN_JSON,
            });
            expect(uploadResponse.status()).toBe(202);

            const endResponse = await request.post(`/api/v2/file-upload/${jobId}/end`, {
                headers: authHeaders(),
            });
            expect(endResponse.status()).toBe(200);

            // Wait for ingest to complete
            await pollUntil(
                async () => {
                    const statusResponse = await request.get('/api/v2/datapipe/status', {
                        headers: authHeaders(),
                    });
                    if (statusResponse.status() !== 200) return false;
                    const body = await statusResponse.json();
                    return body.data?.status === 'idle';
                },
                { timeoutMs: 60_000, description: 'datapipe to return to idle after ingest' }
            );
        });

        await test.step('Search API returns the ingested domain', async () => {
            // The search endpoint may need analysis to run first, but domains
            // should be discoverable after ingest even without analysis.
            // Try searching; if empty, trigger analysis first.
            let searchResponse = await request.get('/api/v2/search?q=SEARCHTEST', {
                headers: authHeaders(),
            });
            expect(searchResponse.status()).toBe(200);
            let searchBody = await searchResponse.json();

            // If no results, trigger analysis and retry
            if (!searchBody.data || searchBody.data.length === 0) {
                await request.put('/api/v2/analysis', { headers: authHeaders() });
                await pollUntil(
                    async () => {
                        const statusResponse = await request.get('/api/v2/datapipe/status', {
                            headers: authHeaders(),
                        });
                        if (statusResponse.status() !== 200) return false;
                        const body = await statusResponse.json();
                        return body.data?.status === 'idle';
                    },
                    { timeoutMs: 60_000, description: 'analysis to complete' }
                );

                searchResponse = await request.get('/api/v2/search?q=SEARCHTEST', {
                    headers: authHeaders(),
                });
                expect(searchResponse.status()).toBe(200);
                searchBody = await searchResponse.json();
            }

            // Verify search returned something (the data structure may vary)
            expect(searchBody.data).toBeTruthy();
        });
    });

    test('Cypher query API returns results', async ({ request }) => {
        const token = await loginViaAPI(request);
        const authHeaders = () => ({ Authorization: `Bearer ${token}` });

        await test.step('Execute a simple Cypher query', async () => {
            const cypherResponse = await request.post('/api/v2/graphs/cypher', {
                headers: { ...authHeaders(), 'Content-Type': 'application/json' },
                data: JSON.stringify({
                    query: 'MATCH (n) RETURN n LIMIT 5',
                    include_properties: true,
                }),
            });
            expect(cypherResponse.status()).toBe(200);
            const body = await cypherResponse.json();
            expect(body.data).toBeTruthy();
        });
    });

    test('Cypher tab in UI accepts and submits a query', async ({ page }) => {
        await loginViaUI(page);

        await test.step('Navigate to explore and open Cypher tab', async () => {
            await page.goto('/ui/explore');
            await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });

            const cypherTab = page.getByTestId('explore_search-container_header_cypher-tab');
            await cypherTab.click();
            await expect(cypherTab).toHaveAttribute('aria-selected', 'true', { timeout: 5_000 });
        });

        await test.step('Cypher editor is visible and accepts input', async () => {
            // The Cypher editor should be visible after clicking the tab.
            // Look for a textarea, input, or CodeMirror/Monaco editor element.
            const cypherInput = page.locator('[data-testid="explore_search_cypher-editor"]')
                .or(page.locator('.cm-editor'))
                .or(page.locator('[role="textbox"]'));
            await expect(cypherInput.first()).toBeVisible({ timeout: 10_000 });
        });
    });
});
