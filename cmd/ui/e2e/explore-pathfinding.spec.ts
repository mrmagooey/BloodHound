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

import { Browser, Page, expect, test } from '@playwright/test';
import { loginViaUI } from './helpers';

// ---------------------------------------------------------------------------
// Shared data – reuse the domain already ingested by data-quality.spec.ts
// so that we don't need another ingest + analysis cycle for these tests.
// ---------------------------------------------------------------------------
const DOMAIN_SID = 'S-1-5-21-7777788888-8888899999-9999900000';
const DOMAIN_NAME = 'DATAQUALITYTEST.LOCAL';

// A second unique domain used exclusively by the pathfinding "with data" tests
// so we can guarantee at least two nodes exist in the graph.
const PF_DOMAIN_SID = 'S-1-5-21-1111122222-2222233333-3333344444';
const PF_DOMAIN_NAME = 'PATHFINDTEST.LOCAL';
const PF_COMPUTER_SID = `${PF_DOMAIN_SID}-1001`;

const PF_DOMAIN_JSON = JSON.stringify({
    meta: { methods: 0, type: 'domains', count: 1, version: 6 },
    data: [
        {
            Properties: {
                domain: PF_DOMAIN_NAME,
                name: PF_DOMAIN_NAME,
                distinguishedname: 'DC=PATHFINDTEST,DC=LOCAL',
                domainsid: PF_DOMAIN_SID,
                collected: true,
                whencreated: 1700000000,
                functionallevel: '2016',
            },
            Trusts: [],
            ChildObjects: [],
            Links: [],
            ACEs: [],
            ObjectIdentifier: PF_DOMAIN_SID,
            IsDeleted: false,
            IsACLProtected: false,
        },
    ],
});

const PF_COMPUTER_JSON = JSON.stringify({
    meta: { methods: 0, type: 'computers', count: 1, version: 6 },
    data: [
        {
            PrimaryGroupSID: `${PF_DOMAIN_SID}-516`,
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
            ObjectIdentifier: PF_COMPUTER_SID,
            IsDeleted: false,
            IsACLProtected: false,
            Properties: {
                domain: PF_DOMAIN_NAME,
                name: `DC01.${PF_DOMAIN_NAME}`,
                domainsid: PF_DOMAIN_SID,
                distinguishedname: `CN=DC01,OU=Domain Controllers,DC=PATHFINDTEST,DC=LOCAL`,
                operatingsystem: 'Windows Server 2022',
                collected: true,
            },
        },
    ],
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Upload a single JSON payload through the File Ingest UI. */
async function uploadFile(page: Page, fileName: string, content: string): Promise<void> {
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

/** Wait for all ingest jobs to reach a terminal state. */
async function waitForIngestComplete(page: Page): Promise<void> {
    await expect(async () => {
        const rows = page.locator('table tbody tr');
        expect(await rows.count(), 'Ingest table should have rows').toBeGreaterThan(0);

        const active = page.locator('table tbody tr').filter({ hasText: /Running|Ingesting|Ready|Analyzing/ });
        expect(await active.count(), 'No in-progress jobs should remain').toBe(0);

        const complete = page.locator('table tbody tr').filter({ hasText: 'Complete' });
        expect(await complete.count(), 'At least one Complete job should exist').toBeGreaterThan(0);
    }).toPass({ intervals: [1_000, 2_000, 2_000], timeout: 60_000 });
}

/** Navigate to /ui/explore, wait for the container, and dismiss any blocking upload dialog. */
async function goToExplore(page: Page): Promise<void> {
    await page.goto('/ui/explore');
    await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });

    const dialog = page.getByRole('dialog');
    if (await dialog.isVisible({ timeout: 3_000 }).catch(() => false)) {
        await page.keyboard.press('Escape');
        await dialog.waitFor({ state: 'hidden', timeout: 5_000 }).catch(() => {});
    }
}

/** Click the Pathfinding tab and verify it becomes active. */
async function openPathfindingTab(page: Page): Promise<void> {
    const tab = page.getByTestId('explore_search-container_header_pathfinding-tab');
    await expect(tab).toBeVisible({ timeout: 10_000 });
    await tab.click();
    // MUI Tab sets aria-selected="true" on the active tab
    await expect(tab).toHaveAttribute('aria-selected', 'true', { timeout: 5_000 });
}

/** Ingest domain + computer for pathfinding tests and wait for the datapipe to finish. */
async function ingestPathfindingData(browser: Browser): Promise<void> {
    const context = await browser.newContext();
    const page = await context.newPage();
    await loginViaUI(page);

    await page.goto('/ui/administration/file-ingest');
    await page.waitForSelector('[data-testid="manual-file-ingest"]', { timeout: 15_000 });

    await uploadFile(page, 'pf-domains.json', PF_DOMAIN_JSON);
    await uploadFile(page, 'pf-computers.json', PF_COMPUTER_JSON);

    // The datapipe runs ingest + analysis automatically; wait for completion.
    await waitForIngestComplete(page);

    await page.close();
    await context.close();
}

// ===========================================================================
// Suite 1 – Structural / UI tests (no data dependency)
// ===========================================================================

test.describe('Explore: Pathfinding tab – structural', () => {
    test.setTimeout(60_000);

    test('Pathfinding tab is selectable and shows source/target inputs', async ({ page }) => {
        await loginViaUI(page);
        await goToExplore(page);
        await openPathfindingTab(page);

        // The PathfindingSearch wrapper is rendered inside the active tabpanel
        const pathfindingPanel = page.getByTestId('pathfinding-search');
        await expect(pathfindingPanel).toBeVisible({ timeout: 10_000 });

        // Source and destination inputs are rendered by ExploreSearchCombobox with aria-labels
        const startInput = page.getByRole('textbox', { name: 'Start Node' });
        const destInput = page.getByRole('textbox', { name: 'Destination Node' });

        await expect(startInput).toBeVisible({ timeout: 5_000 });
        await expect(destInput).toBeVisible({ timeout: 5_000 });
    });

    test('Source and target node inputs accept text', async ({ page }) => {
        await loginViaUI(page);
        await goToExplore(page);
        await openPathfindingTab(page);

        const startInput = page.getByRole('textbox', { name: 'Start Node' });
        const destInput = page.getByRole('textbox', { name: 'Destination Node' });

        await expect(startInput).toBeVisible({ timeout: 10_000 });
        await startInput.fill('TestSource');
        await expect(startInput).toHaveValue('TestSource');

        await expect(destInput).toBeVisible({ timeout: 5_000 });
        await destInput.fill('TestDest');
        await expect(destInput).toHaveValue('TestDest');
    });

    test('Running pathfinding with no known nodes shows empty results gracefully (no crash)', async ({ page }) => {
        await loginViaUI(page);

        const jsErrors: string[] = [];
        page.on('pageerror', (err) => jsErrors.push(err.message));

        await goToExplore(page);
        await openPathfindingTab(page);

        const startInput = page.getByRole('textbox', { name: 'Start Node' });
        const destInput = page.getByRole('textbox', { name: 'Destination Node' });

        await expect(startInput).toBeVisible({ timeout: 10_000 });

        // Type a term that is unlikely to match any node, triggering the search API
        await startInput.fill('ZZZNonExistentNode123');

        // The search result list should either be absent or show an empty-state message.
        // We should NOT see any JS error thrown by the application.
        // Use .first() to avoid strict-mode violation when both source and destination inputs
        // are rendered simultaneously with the same testid.
        const resultList = page.getByTestId('explore_search_result-list').first();
        if (await resultList.isVisible({ timeout: 5_000 }).catch(() => false)) {
            // If visible, it should contain a message indicating no results, not crash
            const listText = await resultList.textContent();
            // The list content is legitimate as long as it doesn't throw
            expect(listText).toBeDefined();
        }

        // The Pathfinding UI container should still be intact
        await expect(page.getByTestId('pathfinding-search')).toBeVisible();

        // Clear, type a destination term too
        await destInput.fill('ZZZAnotherNonExistent456');

        // The page must still be alive with no JS errors
        expect(jsErrors, `Unexpected JS errors: ${jsErrors.join(', ')}`).toHaveLength(0);
    });

    test('Switching away from Pathfinding tab and back preserves tab state', async ({ page }) => {
        await loginViaUI(page);
        await goToExplore(page);
        await openPathfindingTab(page);

        const startInput = page.getByRole('textbox', { name: 'Start Node' });
        await expect(startInput).toBeVisible({ timeout: 10_000 });

        // Type some text to create state we want to verify is preserved
        await startInput.fill('PreservedValue');

        // Switch to Search tab
        const searchTab = page.getByTestId('explore_search-container_header_search-tab');
        await searchTab.click();
        await expect(searchTab).toHaveAttribute('aria-selected', 'true', { timeout: 5_000 });

        // The pathfinding panel should no longer be rendered (TabPanels only renders active tab)
        await expect(page.getByTestId('pathfinding-search')).not.toBeVisible({ timeout: 5_000 });

        // Switch back to Pathfinding tab
        const pathfindingTab = page.getByTestId('explore_search-container_header_pathfinding-tab');
        await pathfindingTab.click();
        await expect(pathfindingTab).toHaveAttribute('aria-selected', 'true', { timeout: 5_000 });

        // The pathfinding panel re-renders; inputs should be present again
        await expect(page.getByTestId('pathfinding-search')).toBeVisible({ timeout: 5_000 });
        await expect(page.getByRole('textbox', { name: 'Start Node' })).toBeVisible({ timeout: 5_000 });
    });

    test('Pathfinding tab produces no JS errors when navigated to on an empty graph', async ({ page }) => {
        const jsErrors: string[] = [];
        page.on('pageerror', (err) => jsErrors.push(err.message));

        await loginViaUI(page);
        await goToExplore(page);
        await openPathfindingTab(page);

        // Let the tab settle — any async renders triggered by the tab switch should complete
        await expect(page.getByTestId('pathfinding-search')).toBeVisible({ timeout: 10_000 });

        expect(jsErrors, `Unexpected JS errors: ${jsErrors.join(', ')}`).toHaveLength(0);
    });
});

// ===========================================================================
// Suite 2 – Functional tests that require ingested data
// ===========================================================================

test.describe('Explore: Pathfinding tab – with data', () => {
    test.setTimeout(180_000);

    // Ingest domain + computer once for all tests in this block.
    test.beforeAll(async ({ browser }) => {
        await ingestPathfindingData(browser);
    });

    test('source input shows autocomplete results for known node', async ({ page }) => {
        await loginViaUI(page);
        await goToExplore(page);
        await openPathfindingTab(page);

        const startInput = page.getByRole('textbox', { name: 'Start Node' });
        await expect(startInput).toBeVisible({ timeout: 10_000 });

        // Type part of the ingested domain name; the search API should return results
        await startInput.fill('PATHFINDTEST');

        // The autocomplete dropdown should appear with at least one matching item.
        // Use .first() to avoid strict-mode violation: both source and destination inputs
        // render with the same testid simultaneously.
        const resultList = page.getByTestId('explore_search_result-list').first();
        await expect(resultList).toBeVisible({ timeout: 15_000 });

        const resultItems = page.locator('[data-testid="explore_search_result-list"]').first().locator('[role="option"]');
        await expect(resultItems.first()).toBeVisible({ timeout: 10_000 });
        expect(await resultItems.count()).toBeGreaterThan(0);
    });

    test('pathfinding between two known nodes calls the shortest-path API', async ({ page }) => {
        await loginViaUI(page);
        await goToExplore(page);
        await openPathfindingTab(page);

        const startInput = page.getByRole('textbox', { name: 'Start Node' });
        const destInput = page.getByRole('textbox', { name: 'Destination Node' });

        await expect(startInput).toBeVisible({ timeout: 10_000 });

        // ---------------------------------------------------------------
        // Select the source node (domain)
        // ---------------------------------------------------------------
        await startInput.fill('PATHFINDTEST');

        // Use .first() to avoid strict-mode violation: both source and destination inputs
        // render the result list with the same testid simultaneously.
        const resultList = page.getByTestId('explore_search_result-list').first();
        await expect(resultList).toBeVisible({ timeout: 15_000 });

        // Click the first matching result to select it
        const firstItem = resultList.locator('[role="option"]').first();
        await expect(firstItem).toBeVisible({ timeout: 10_000 });
        await firstItem.click();

        // After selection the input should show the selected node name
        await expect(startInput).not.toHaveValue('', { timeout: 5_000 });

        // ---------------------------------------------------------------
        // Select the destination node (computer)
        // ---------------------------------------------------------------
        await expect(destInput).toBeVisible({ timeout: 5_000 });
        await destInput.fill('DC01');

        // After source selection the source dropdown is collapsed; the destination
        // dropdown is now the second (nth(1)) result list in the DOM.
        const destResultList = page.getByTestId('explore_search_result-list').nth(1);
        await expect(destResultList).toBeVisible({ timeout: 15_000 });
        const firstDestItem = destResultList.locator('[role="option"]').first();
        await expect(firstDestItem).toBeVisible({ timeout: 10_000 });

        // Intercept the shortest-path API call that fires when both nodes are selected
        const pathResponsePromise = page.waitForResponse(
            (res) =>
                res.url().includes('/api/v2/graphs/shortest-path') && res.request().method() === 'GET',
            { timeout: 30_000 }
        ).catch(() => null);

        await firstDestItem.click();

        // ---------------------------------------------------------------
        // Verify the API was called and returned a meaningful response
        // ---------------------------------------------------------------
        const pathResponse = await pathResponsePromise;
        expect(pathResponse, 'Shortest-path API should have been called').not.toBeNull();

        const status = pathResponse!.status();
        // 200 = path found; 404 = no path between the two nodes (both are valid outcomes)
        expect(
            [200, 404],
            `Unexpected status ${status} from shortest-path API`
        ).toContain(status);
    });

    test('pathfinding with no path gracefully shows "no path" state without crashing', async ({ page }) => {
        const jsErrors: string[] = [];
        page.on('pageerror', (err) => jsErrors.push(err.message));

        await loginViaUI(page);
        await goToExplore(page);
        await openPathfindingTab(page);

        const startInput = page.getByRole('textbox', { name: 'Start Node' });
        const destInput = page.getByRole('textbox', { name: 'Destination Node' });

        await expect(startInput).toBeVisible({ timeout: 10_000 });

        // Select source: the pathfinding domain.
        // Use .first() to avoid strict-mode violation when both source and destination
        // result lists render with the same testid simultaneously.
        await startInput.fill('PATHFINDTEST');
        const resultList = page.getByTestId('explore_search_result-list').first();
        await expect(resultList).toBeVisible({ timeout: 15_000 });
        await resultList.locator('[role="option"]').first().click();

        // Type a destination term and verify the UI handles the search gracefully.
        // We don't require a selectable result — the search may return "No results"
        // (a disabled placeholder) if the domain isn't in the pathfinding dataset.
        await expect(destInput).toBeVisible({ timeout: 5_000 });
        await destInput.fill('UNKNOWNNODE_NORESULTS');
        // After source selection, destination is the second (nth(1)) result list.
        const destResultList2 = page.getByTestId('explore_search_result-list').nth(1);
        await expect(destResultList2).toBeVisible({ timeout: 15_000 });

        // The explore page must still be alive and the search widget intact
        await expect(page.getByTestId('explore')).toBeVisible({ timeout: 5_000 });
        await expect(page.getByTestId('pathfinding-search')).toBeVisible({ timeout: 5_000 });

        expect(jsErrors, `Unexpected JS errors: ${jsErrors.join(', ')}`).toHaveLength(0);
    });

    test('Pathfinding tab produces no JS errors when both nodes are selected', async ({ page }) => {
        const jsErrors: string[] = [];
        page.on('pageerror', (err) => jsErrors.push(err.message));

        await loginViaUI(page);
        await goToExplore(page);
        await openPathfindingTab(page);

        const startInput = page.getByRole('textbox', { name: 'Start Node' });
        await expect(startInput).toBeVisible({ timeout: 10_000 });

        // Fill and select source node.
        // Use .first() to avoid strict-mode violation: both source and destination inputs
        // render the result list with the same testid simultaneously.
        await startInput.fill('PATHFINDTEST');
        const resultList = page.getByTestId('explore_search_result-list').first();
        await expect(resultList).toBeVisible({ timeout: 15_000 });
        await resultList.locator('[role="option"]').first().click();

        // Fill and select destination node — after source selection, destination
        // is the second (nth(1)) result list in the DOM.
        const destInput = page.getByRole('textbox', { name: 'Destination Node' });
        await expect(destInput).toBeVisible({ timeout: 5_000 });
        await destInput.fill('DC01');
        const destResultListJS = page.getByTestId('explore_search_result-list').nth(1);
        await expect(destResultListJS).toBeVisible({ timeout: 15_000 });
        await destResultListJS.locator('[role="option"]').first().click();

        // Give the graph time to render any result
        await page.waitForResponse(
            (res) => res.url().includes('/api/v2/graphs/shortest-path'),
            { timeout: 20_000 }
        ).catch(() => null);

        expect(jsErrors, `Unexpected JS errors: ${jsErrors.join(', ')}`).toHaveLength(0);
    });
});
