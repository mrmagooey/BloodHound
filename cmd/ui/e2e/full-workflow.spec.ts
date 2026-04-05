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
import { loginViaUI } from './helpers';

/**
 * Minimal BloodHound v6 ingest data with a unique domain SID for this test.
 * Uses a SID that does not collide with ingest-analysis.spec.ts or graph-canvas.spec.ts.
 */
const DOMAIN_SID = 'S-1-5-21-1234509876-9876501234-5678012345';

const MINIMAL_DOMAIN_JSON = JSON.stringify({
    meta: { methods: 0, type: 'domains', count: 1, version: 6 },
    data: [
        {
            Properties: {
                domain: 'FULLWORKFLOW.LOCAL',
                name: 'FULLWORKFLOW.LOCAL',
                distinguishedname: 'DC=FULLWORKFLOW,DC=LOCAL',
                domainsid: DOMAIN_SID,
                collected: true,
                whencreated: 1700000000,
                functionallevel: '2016',
            },
            Trusts: [],
            ChildObjects: [],
            Links: [],
            ACEs: [],
            ObjectIdentifier: DOMAIN_SID,
            IsDeleted: false,
            IsACLProtected: false,
        },
    ],
});

const MINIMAL_COMPUTER_JSON = JSON.stringify({
    meta: { methods: 0, type: 'computers', count: 1, version: 6 },
    data: [
        {
            PrimaryGroupSID: `${DOMAIN_SID}-516`,
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
            ObjectIdentifier: `${DOMAIN_SID}-1000`,
            IsDeleted: false,
            IsACLProtected: false,
            Properties: {
                domain: 'FULLWORKFLOW.LOCAL',
                name: 'DC01.FULLWORKFLOW.LOCAL',
                domainsid: DOMAIN_SID,
                distinguishedname: 'CN=DC01,OU=Domain Controllers,DC=FULLWORKFLOW,DC=LOCAL',
                operatingsystem: 'Windows Server 2022',
                collected: true,
            },
        },
    ],
});

test.describe('Full workflow: login, upload, analyse, query, verify', () => {
    test.setTimeout(120_000);

    test('login, upload via UI, analyse, query via UI, verify results', async ({ page }) => {
        // Step 1: Login via the UI login form
        await test.step('Login via UI', async () => {
            await loginViaUI(page);
        });

        // Step 2: Upload data via the File Ingest page UI
        await test.step('Navigate to File Ingest page', async () => {
            await page.goto('/ui/administration/file-ingest');
            await page.waitForSelector('[data-testid="manual-file-ingest"]', { timeout: 15_000 });
        });

        await test.step('Open the upload dialog', async () => {
            const uploadBtn = page.getByTestId('file-ingest_button-upload-files');
            await expect(uploadBtn).toBeVisible({ timeout: 10_000 });
            await expect(uploadBtn).toBeEnabled({ timeout: 10_000 });
            await uploadBtn.click();
        });

        await test.step('Select domain file for upload', async () => {
            const dialog = page.locator('[role="dialog"]');
            await expect(dialog).toBeVisible({ timeout: 5_000 });

            const fileInput = page.getByTestId('ingest-file-upload');
            await fileInput.setInputFiles({
                name: 'workflow-domains.json',
                mimeType: 'application/json',
                buffer: Buffer.from(MINIMAL_DOMAIN_JSON),
            });

            await expect(dialog.locator('text=workflow-domains.json')).toBeVisible({ timeout: 5_000 });
        });

        await test.step('Click Upload and wait for completion', async () => {
            const uploadConfirmBtn = page.getByTestId('confirmation-dialog_button-yes');
            await expect(uploadConfirmBtn).toBeEnabled({ timeout: 5_000 });
            await uploadConfirmBtn.click();

            const dialog = page.locator('[role="dialog"]');
            await expect(dialog.locator('text=/successfully.*uploaded/i')).toBeVisible({ timeout: 30_000 });
        });

        await test.step('Close the upload dialog', async () => {
            const closeBtn = page.getByTestId('confirmation-dialog_button-no');
            await closeBtn.click();
            const dialog = page.locator('[role="dialog"]');
            await expect(dialog).not.toBeVisible({ timeout: 5_000 });
        });

        // Upload computer data via a second upload cycle
        await test.step('Open the upload dialog for computer data', async () => {
            const uploadBtn = page.getByTestId('file-ingest_button-upload-files');
            await expect(uploadBtn).toBeVisible({ timeout: 10_000 });
            await expect(uploadBtn).toBeEnabled({ timeout: 10_000 });
            await uploadBtn.click();
        });

        await test.step('Select computer file for upload', async () => {
            const dialog = page.locator('[role="dialog"]');
            await expect(dialog).toBeVisible({ timeout: 5_000 });

            const fileInput = page.getByTestId('ingest-file-upload');
            await fileInput.setInputFiles({
                name: 'workflow-computers.json',
                mimeType: 'application/json',
                buffer: Buffer.from(MINIMAL_COMPUTER_JSON),
            });

            await expect(dialog.locator('text=workflow-computers.json')).toBeVisible({ timeout: 5_000 });
        });

        await test.step('Click Upload and wait for computer upload completion', async () => {
            const uploadConfirmBtn = page.getByTestId('confirmation-dialog_button-yes');
            await expect(uploadConfirmBtn).toBeEnabled({ timeout: 5_000 });
            await uploadConfirmBtn.click();

            const dialog = page.locator('[role="dialog"]');
            await expect(dialog.locator('text=/successfully.*uploaded/i')).toBeVisible({ timeout: 30_000 });
        });

        await test.step('Close the second upload dialog', async () => {
            const closeBtn = page.getByTestId('confirmation-dialog_button-no');
            await closeBtn.click();
            const dialog = page.locator('[role="dialog"]');
            await expect(dialog).not.toBeVisible({ timeout: 5_000 });
        });

        // Wait for all ingest jobs to finish processing by watching the ingest table UI
        await test.step('Wait for ingest jobs to finish processing', async () => {
            // We are already on /ui/administration/file-ingest after closing the upload dialog.
            // The table auto-refreshes. Wait until at least one "Complete" status appears
            // and no "Running" or "Ingesting" statuses remain (terminal states: Complete, Failed, Partially Completed).
            await expect(async () => {
                // Reload to get fresh table data
                const rows = page.locator('table tbody tr');
                const rowCount = await rows.count();
                expect(rowCount, 'Ingest table should have rows').toBeGreaterThan(0);

                // Check that no rows contain active/in-progress statuses
                const runningIndicators = page.locator('table tbody tr').filter({ hasText: /Running|Ingesting|Ready|Analyzing/ });
                const activeCount = await runningIndicators.count();
                expect(activeCount, 'No in-progress jobs should remain').toBe(0);

                // At least one Complete job should exist
                const completeIndicators = page.locator('table tbody tr').filter({ hasText: 'Complete' });
                const completeCount = await completeIndicators.count();
                expect(completeCount, 'At least one Complete job should exist').toBeGreaterThan(0);
            }).toPass({ intervals: [1_000, 2_000, 2_000], timeout: 60_000 });
        });

        // Step 3: Trigger analysis via the BloodHound Configuration page UI
        await test.step('Navigate to BloodHound Configuration and trigger analysis', async () => {
            await page.goto('/ui/administration/bloodhound-configuration');

            // Wait for the "Analyze Now" button to appear and be enabled
            const analyzeBtn = page.getByRole('button', { name: 'Analyze Now' });
            await expect(analyzeBtn).toBeVisible({ timeout: 15_000 });
            await expect(analyzeBtn).toBeEnabled({ timeout: 15_000 });

            // Set up response interception before clicking
            const analysisResponsePromise = page.waitForResponse(
                (res) => res.url().includes('/api/v2/analysis') && res.request().method() === 'PUT',
                { timeout: 15_000 }
            );

            // Click the button to open the confirmation dialog
            await analyzeBtn.click();

            // Click "Confirm" in the confirmation dialog
            const confirmBtn = page.getByRole('button', { name: 'Confirm' });
            await expect(confirmBtn).toBeVisible({ timeout: 5_000 });
            await confirmBtn.click();

            // Wait for the analysis API call to complete
            const analysisResponse = await analysisResponsePromise;
            expect(analysisResponse.status(), 'Request analysis should return 202').toBe(202);
        });

        await test.step('Wait for analysis to complete', async () => {
            // The "Analyze Now" button shows "Analyzing" while analysis is running,
            // and returns to "Analyze Now" (enabled) when datapipe status is idle.
            // Wait for the button to show "Analyze Now" text again (not "Analyzing").
            const analyzeBtn = page.getByRole('button', { name: 'Analyze Now' });
            await expect(analyzeBtn).toBeVisible({ timeout: 60_000 });
            await expect(analyzeBtn).toBeEnabled({ timeout: 60_000 });
        });

        // Step 4: Navigate to Explore and run a Cypher query via the UI
        await test.step('Navigate to Explore page', async () => {
            await page.goto('/ui/explore');
            await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });
        });

        // Intercept the Cypher API response to verify data is returned
        const cypherResponsePromise = page.waitForResponse(
            (res) => res.url().includes('/api/v2/graphs/cypher') && res.request().method() === 'POST',
            { timeout: 30_000 }
        ).catch(() => null);

        await test.step('Run Cypher query via the UI', async () => {
            // Dismiss any dialog that may be blocking the UI
            const dialog = page.getByRole('dialog');
            if (await dialog.isVisible({ timeout: 3_000 }).catch(() => false)) {
                await page.keyboard.press('Escape');
                await dialog.waitFor({ state: 'hidden', timeout: 5_000 }).catch(() => {});
            }

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
            await page.keyboard.type("MATCH (n) WHERE n.domain = 'FULLWORKFLOW.LOCAL' RETURN n LIMIT 10", { delay: 10 });

            // Execute the query with Shift+Enter
            await page.keyboard.press('Shift+Enter');
        });

        // Step 5: Verify that results appear in the graph
        await test.step('Verify Cypher API returned nodes', async () => {
            const cypherResponse = await cypherResponsePromise;
            expect(cypherResponse, 'Cypher API response should have been received').not.toBeNull();
            const responseStatus = cypherResponse!.status();
            const responseBody = await cypherResponse!.json();
            const nodeCount = responseBody?.data?.nodes ? Object.keys(responseBody.data.nodes).length : 0;
            expect(responseStatus, `Cypher API should return 200, got ${responseStatus}`).toBe(200);
            expect(nodeCount, 'Cypher response should contain nodes from FULLWORKFLOW.LOCAL').toBeGreaterThan(0);
        });

        await test.step('Verify sigma graph container is rendering', async () => {
            const sigmaContainer = page.locator('#sigma-container .sigma-container');
            await expect(sigmaContainer).toBeVisible({ timeout: 10_000 });
            const canvases = sigmaContainer.locator('canvas');
            await expect(canvases).toHaveCount(7, { timeout: 10_000 });
        });
    });
});
