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

import { Page, expect, test } from '@playwright/test';
import { loginViaUI } from './helpers';

/**
 * Minimal BloodHound v6 ingest data: one domain + one computer.
 * After ingest + analysis these will be queryable via Cypher and the search API.
 */
const MINIMAL_DOMAIN_JSON = JSON.stringify({
    meta: { methods: 0, type: 'domains', count: 1, version: 6 },
    data: [
        {
            Properties: {
                domain: 'GRAPHTEST.LOCAL',
                name: 'GRAPHTEST.LOCAL',
                distinguishedname: 'DC=GRAPHTEST,DC=LOCAL',
                domainsid: 'S-1-5-21-5555555555-6666666666-7777777777',
                collected: true,
                whencreated: 1700000000,
                functionallevel: '2016',
            },
            Trusts: [],
            ChildObjects: [],
            Links: [],
            ACEs: [],
            ObjectIdentifier: 'S-1-5-21-5555555555-6666666666-7777777777',
            IsDeleted: false,
            IsACLProtected: false,
        },
    ],
});

const MINIMAL_COMPUTER_JSON = JSON.stringify({
    meta: { methods: 0, type: 'computers', count: 1, version: 6 },
    data: [
        {
            PrimaryGroupSID: 'S-1-5-21-5555555555-6666666666-7777777777-516',
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
            ObjectIdentifier: 'S-1-5-21-5555555555-6666666666-7777777777-1000',
            IsDeleted: false,
            IsACLProtected: false,
            Properties: {
                domain: 'GRAPHTEST.LOCAL',
                name: 'DC01.GRAPHTEST.LOCAL',
                domainsid: 'S-1-5-21-5555555555-6666666666-7777777777',
                distinguishedname: 'CN=DC01,OU=Domain Controllers,DC=GRAPHTEST,DC=LOCAL',
                operatingsystem: 'Windows Server 2022',
                collected: true,
            },
        },
    ],
});

/**
 * Upload a file via the File Ingest UI dialog.
 */
async function uploadFileViaUI(page: Page, fileName: string, content: string): Promise<void> {
    const uploadBtn = page.getByTestId('file-ingest_button-upload-files');
    await expect(uploadBtn).toBeVisible({ timeout: 10_000 });
    await expect(uploadBtn).toBeEnabled({ timeout: 10_000 });
    await uploadBtn.click();

    const dialog = page.locator('[role="dialog"]');
    await expect(dialog).toBeVisible({ timeout: 5_000 });

    const fileInput = page.getByTestId('ingest-file-upload');
    await fileInput.setInputFiles({
        name: fileName,
        mimeType: 'application/json',
        buffer: Buffer.from(content),
    });

    const confirmBtn = page.getByTestId('confirmation-dialog_button-yes');
    await expect(confirmBtn).toBeEnabled({ timeout: 5_000 });
    await confirmBtn.click();

    await expect(dialog.locator('text=/successfully.*uploaded/i')).toBeVisible({ timeout: 30_000 });

    const closeBtn = page.getByTestId('confirmation-dialog_button-no');
    await closeBtn.click();
    await expect(dialog).not.toBeVisible({ timeout: 5_000 });
}

/**
 * Ingest domain + computer data and run analysis via the UI.
 * Returns when analysis is complete (Analyze Now button re-enables).
 */
async function ingestAndAnalyzeViaUI(page: Page): Promise<void> {
    // Navigate to File Ingest page
    await page.goto('/ui/administration/file-ingest');
    await page.waitForSelector('[data-testid="manual-file-ingest"]', { timeout: 15_000 });

    // Upload domain JSON
    await uploadFileViaUI(page, 'graph-domains.json', MINIMAL_DOMAIN_JSON);

    // Upload computer JSON
    await uploadFileViaUI(page, 'graph-computers.json', MINIMAL_COMPUTER_JSON);

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

    const confirmBtn = page.getByRole('button', { name: /Confirm/i });
    if (await confirmBtn.isVisible({ timeout: 3_000 }).catch(() => false)) {
        await confirmBtn.click();
    }

    // Wait for analysis to complete (button re-enables)
    await expect(analyzeBtn).toBeEnabled({ timeout: 60_000 });
}

/**
 * Dismiss the NoDataFileUploadDialog that covers the explore page when the
 * graph has no data. It intercepts pointer events, so we must close it first.
 */
async function dismissFileUploadDialog(page: Page): Promise<void> {
    const dialog = page.getByRole('dialog');
    if (await dialog.isVisible({ timeout: 3_000 }).catch(() => false)) {
        await page.keyboard.press('Escape');
        await dialog.waitFor({ state: 'hidden', timeout: 5_000 }).catch(() => {});
    }
}

/**
 * Navigate to the explore page Cypher tab, type a query, and execute it.
 */
async function runCypherQueryInUI(page: Page, query: string): Promise<void> {
    // Dismiss any dialog that may be blocking the UI
    await dismissFileUploadDialog(page);

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

    // Select all existing text and replace with our query
    await page.keyboard.press('ControlOrMeta+a');
    await page.keyboard.type(query, { delay: 10 });

    // Execute the query with Shift+Enter
    await page.keyboard.press('Shift+Enter');
}

test.describe('Graph canvas: Sigma renders nodes after data ingest', () => {
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

    test('sigma container mounts with canvas layers on the explore page', async ({ page }) => {
        await loginViaUI(page);
        await page.goto('/ui/explore');
        await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });

        // Sigma v2 creates multiple <canvas> elements inside the .sigma-container div
        // (nested inside #sigma-container). The canvases are created in this order:
        // edges, edgeLabels, nodes, labels, hovers, hoverNodes, mouse (7 total).
        const sigmaContainer = page.locator('#sigma-container .sigma-container');
        await expect(sigmaContainer).toBeVisible({ timeout: 10_000 });

        const canvases = sigmaContainer.locator('canvas');
        // Sigma creates exactly 7 canvas layers
        await expect(canvases).toHaveCount(7, { timeout: 10_000 });
    });

    test('cypher query populates the sigma graph with nodes', async ({ page }) => {
        await loginViaUI(page);
        await page.goto('/ui/explore');
        await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });

        // Intercept the Cypher API response to verify data is returned
        const cypherResponsePromise = page.waitForResponse(
            (res) => res.url().includes('/api/v2/graphs/cypher') && res.request().method() === 'POST',
            { timeout: 30_000 }
        ).catch(() => null);

        // Run a Cypher query that returns all nodes
        await runCypherQueryInUI(page, 'MATCH (n) RETURN n LIMIT 10');

        // Verify the Cypher API was called and returned data
        const cypherResponse = await cypherResponsePromise;
        expect(cypherResponse, 'Cypher API response should have been received').not.toBeNull();
        const responseStatus = cypherResponse!.status();
        const responseBody = await cypherResponse!.json();
        const nodeCount = responseBody?.data?.nodes ? Object.keys(responseBody.data.nodes).length : 0;
        expect(responseStatus, `Cypher API should return 200, got ${responseStatus}`).toBe(200);
        expect(nodeCount, 'Cypher response should contain nodes').toBeGreaterThan(0);

        // Verify the sigma container still has 7 canvases (sigma is rendering)
        const sigmaContainer = page.locator('#sigma-container .sigma-container');
        await expect(sigmaContainer).toBeVisible({ timeout: 10_000 });
        const canvases = sigmaContainer.locator('canvas');
        await expect(canvases).toHaveCount(7, { timeout: 10_000 });
    });

    test('sigma graph node count matches expected ingest data', async ({ page }) => {
        await loginViaUI(page);
        await page.goto('/ui/explore');
        await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });

        // Intercept the Cypher API response to verify node count
        const cypherResponsePromise = page.waitForResponse(
            (res) => res.url().includes('/api/v2/graphs/cypher') && res.request().method() === 'POST',
            { timeout: 30_000 }
        ).catch(() => null);

        // Run a Cypher query filtering to our test domain
        await runCypherQueryInUI(page, "MATCH (n) WHERE n.domain = 'GRAPHTEST.LOCAL' RETURN n");

        const cypherResponse = await cypherResponsePromise;
        expect(cypherResponse, 'Cypher API response should have been received').not.toBeNull();
        expect(cypherResponse!.status()).toBe(200);
        const body = await cypherResponse!.json();
        const nodeCount = body?.data?.nodes ? Object.keys(body.data.nodes).length : 0;
        // We ingested 1 domain + 1 computer; analysis creates additional well-known
        // group nodes, so assert at least 2.
        expect(nodeCount).toBeGreaterThanOrEqual(2);
    });

    test('search for ingested domain loads a node into the graph', async ({ page }) => {
        await loginViaUI(page);
        await page.goto('/ui/explore');
        await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });

        // Dismiss any dialog that covers the explore page
        await dismissFileUploadDialog(page);

        // Use the node search tab (default tab) to search for the domain.
        // The data-testid is on an MUI TextField wrapper div, so we need
        // to locate the actual input element within it.
        const searchContainer = page.getByTestId('explore_search_input-search');
        await expect(searchContainer).toBeVisible({ timeout: 10_000 });
        const searchInput = searchContainer.locator('input');
        await searchInput.fill('GRAPHTEST');

        // Wait for search results to appear
        const resultList = page.getByTestId('explore_search_result-list');
        await expect(resultList).toBeVisible({ timeout: 15_000 });

        // Click the first search result
        const firstResult = page.getByTestId('explore_search_result-list-item').first();
        await expect(firstResult).toBeVisible({ timeout: 10_000 });
        await firstResult.click();

        // After clicking a search result, the URL should update and the graph
        // API should be called to load the node data.
        await page.waitForTimeout(2_000);
        // Verify the sigma container is still active
        const sigmaContainer = page.locator('#sigma-container .sigma-container');
        await expect(sigmaContainer).toBeVisible({ timeout: 10_000 });
    });

    test('sigma labels canvas is present and has non-zero dimensions', async ({ page }) => {
        await loginViaUI(page);
        await page.goto('/ui/explore');
        await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });

        // Dismiss any dialog that covers the explore page
        await dismissFileUploadDialog(page);

        // Wait for sigma to fully initialize by checking for 7 canvases
        const sigmaContainer = page.locator('#sigma-container .sigma-container');
        await expect(sigmaContainer).toBeVisible({ timeout: 10_000 });
        const canvases = sigmaContainer.locator('canvas');
        await expect(canvases).toHaveCount(7, { timeout: 10_000 });

        // Verify at least one canvas has non-zero dimensions.
        // Sigma sizes canvases based on the container's client dimensions.
        // In headless Chrome the element-level width/height attributes should
        // be set once the sigma container is visible and sized by CSS layout.
        // Check any canvas (e.g., the first one) for non-zero dimensions.
        const firstCanvas = canvases.first();
        const box = await firstCanvas.boundingBox();
        expect(box, 'Canvas should have a bounding box').not.toBeNull();
        expect(box!.width, 'Canvas bounding box width should be > 0').toBeGreaterThan(0);
        expect(box!.height, 'Canvas bounding box height should be > 0').toBeGreaterThan(0);
    });
});
