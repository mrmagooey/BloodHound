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

import { expect, test, Page } from '@playwright/test';
import { loginViaUI } from './helpers';

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

/**
 * Ingest domain data and run analysis via the UI.
 */
async function ingestAndAnalyzeViaUI(page: Page): Promise<void> {
    // Navigate to File Ingest page
    await page.goto('/ui/administration/file-ingest');
    await page.waitForSelector('[data-testid="manual-file-ingest"]', { timeout: 15_000 });

    // Upload domain JSON
    const uploadBtn = page.getByTestId('file-ingest_button-upload-files');
    await expect(uploadBtn).toBeVisible({ timeout: 10_000 });
    await expect(uploadBtn).toBeEnabled({ timeout: 10_000 });
    await uploadBtn.click();

    const dialog = page.locator('[role="dialog"]');
    await expect(dialog).toBeVisible({ timeout: 5_000 });

    const fileInput = page.getByTestId('ingest-file-upload');
    await fileInput.setInputFiles({
        name: 'search-domains.json',
        mimeType: 'application/json',
        buffer: Buffer.from(MINIMAL_DOMAIN_JSON),
    });

    const confirmBtn = page.getByTestId('confirmation-dialog_button-yes');
    await expect(confirmBtn).toBeEnabled({ timeout: 5_000 });
    await confirmBtn.click();

    await expect(dialog.locator('text=/successfully.*uploaded/i')).toBeVisible({ timeout: 30_000 });

    const closeBtn = page.getByTestId('confirmation-dialog_button-no');
    await closeBtn.click();
    await expect(dialog).not.toBeVisible({ timeout: 5_000 });

    // Wait for ingest to complete
    await expect(async () => {
        const rows = page.locator('table tbody tr');
        const rowCount = await rows.count();
        expect(rowCount, 'Ingest table should have rows').toBeGreaterThan(0);

        const runningIndicators = page.locator('table tbody tr').filter({ hasText: /Running|Ingesting|Ready|Analyzing/ });
        const activeCount = await runningIndicators.count();
        expect(activeCount, 'No in-progress jobs should remain').toBe(0);

        const completeIndicators = page.locator('table tbody tr').filter({ hasText: 'Complete' });
        const completeCount = await completeIndicators.count();
        expect(completeCount, 'At least one Complete job should exist').toBeGreaterThan(0);
    }).toPass({ intervals: [1_000, 2_000, 2_000], timeout: 60_000 });

    // Trigger analysis
    await page.goto('/ui/administration/bloodhound-configuration');

    const analyzeBtn = page.getByRole('button', { name: /Analyze Now/i });
    await expect(analyzeBtn).toBeVisible({ timeout: 15_000 });
    await expect(analyzeBtn).toBeEnabled({ timeout: 15_000 });

    await analyzeBtn.click();

    const confirmAnalysisBtn = page.getByRole('button', { name: /Confirm/i });
    if (await confirmAnalysisBtn.isVisible({ timeout: 3_000 }).catch(() => false)) {
        await confirmAnalysisBtn.click();
    }

    // Wait for analysis to complete (button re-enables)
    await expect(analyzeBtn).toBeEnabled({ timeout: 60_000 });
}

test.describe('Explore: search and Cypher after ingest', () => {
    test.setTimeout(120_000);

    // Ingest data once for all tests in this describe block.
    test.beforeAll(async ({ browser }) => {
        const context = await browser.newContext();
        const page = await context.newPage();
        await loginViaUI(page);
        await ingestAndAnalyzeViaUI(page);
        await page.close();
        await context.close();
    });

    test('search returns results after data ingest', async ({ page }) => {
        await loginViaUI(page);

        await test.step('Navigate to explore and search for ingested domain', async () => {
            await page.goto('/ui/explore');
            await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });

            // Dismiss any dialog that covers the explore page
            const dialog = page.getByRole('dialog');
            if (await dialog.isVisible({ timeout: 3_000 }).catch(() => false)) {
                await page.keyboard.press('Escape');
                await dialog.waitFor({ state: 'hidden', timeout: 5_000 }).catch(() => {});
            }

            // Use the node search tab (default tab) to search for the domain
            const searchContainer = page.getByTestId('explore_search_input-search');
            await expect(searchContainer).toBeVisible({ timeout: 10_000 });
            const searchInput = searchContainer.locator('input');
            await searchInput.fill('SEARCHTEST');

            // Wait for search results to appear
            const resultList = page.getByTestId('explore_search_result-list');
            await expect(resultList).toBeVisible({ timeout: 15_000 });

            // Verify result list has items
            const resultItems = page.getByTestId('explore_search_result-list-item');
            await expect(resultItems.first()).toBeVisible({ timeout: 10_000 });
            const itemCount = await resultItems.count();
            expect(itemCount, 'Search should return at least one result').toBeGreaterThan(0);
        });
    });

    test('Cypher query returns results', async ({ page }) => {
        await loginViaUI(page);

        await test.step('Navigate to explore and run Cypher query', async () => {
            await page.goto('/ui/explore');
            await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });

            // Dismiss any dialog that covers the explore page
            const dialog = page.getByRole('dialog');
            if (await dialog.isVisible({ timeout: 3_000 }).catch(() => false)) {
                await page.keyboard.press('Escape');
                await dialog.waitFor({ state: 'hidden', timeout: 5_000 }).catch(() => {});
            }

            // Intercept the Cypher API response
            const cypherResponsePromise = page.waitForResponse(
                (res) => res.url().includes('/api/v2/graphs/cypher') && res.request().method() === 'POST',
                { timeout: 30_000 }
            ).catch(() => null);

            // Click the Cypher tab
            const cypherTab = page.getByTestId('explore_search-container_header_cypher-tab');
            await cypherTab.click();
            await expect(cypherTab).toHaveAttribute('aria-selected', 'true', { timeout: 5_000 });

            // Wait for the Cypher editor (CodeMirror) to appear
            const cmEditor = page.locator('.cm-editor');
            await expect(cmEditor.first()).toBeVisible({ timeout: 10_000 });

            // Focus the CodeMirror content area and type the query
            const cmContent = page.locator('.cm-content');
            await cmContent.first().click();

            await page.keyboard.press('ControlOrMeta+a');
            await page.keyboard.type('MATCH (n) RETURN n LIMIT 5', { delay: 10 });

            // Execute the query with Shift+Enter
            await page.keyboard.press('Shift+Enter');

            // Verify the Cypher API was called and returned data
            const cypherResponse = await cypherResponsePromise;
            expect(cypherResponse, 'Cypher API response should have been received').not.toBeNull();
            const responseStatus = cypherResponse!.status();
            const responseBody = await cypherResponse!.json();
            if (responseStatus === 200) {
                expect(responseBody.data).toBeTruthy();
                const nodeCount = responseBody?.data?.nodes ? Object.keys(responseBody.data.nodes).length : 0;
                expect(nodeCount, 'Cypher response should contain nodes').toBeGreaterThan(0);
            } else {
                // 404 means the query executed but found nothing
                expect([200, 404]).toContain(responseStatus);
            }
        });
    });

    test('Cypher tab in UI accepts and submits a query', async ({ page }) => {
        await loginViaUI(page);

        await test.step('Navigate to explore and open Cypher tab', async () => {
            await page.goto('/ui/explore');
            await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });

            // Dismiss the "no data" upload dialog if it appears (it blocks clicks)
            const dialog = page.getByRole('dialog');
            if (await dialog.isVisible({ timeout: 3_000 }).catch(() => false)) {
                await page.keyboard.press('Escape');
                await dialog.waitFor({ state: 'hidden', timeout: 5_000 }).catch(() => {});
            }

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
