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
import { E2E_ADMIN_USERNAME, E2E_ADMIN_PASSWORD } from './global-setup';

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

test.describe('Ingest and Analysis workflow', () => {
    // Increase the overall test timeout because ingest + analysis can take time.
    test.setTimeout(120_000);

    test('login, upload data, ingest, trigger analysis, and verify completion', async ({ page }) => {
        await test.step('Login via UI', async () => {
            await loginViaUI(page);
        });

        // Step 1: Upload domain data via the File Ingest page UI
        await test.step('Navigate to File Ingest page', async () => {
            await page.goto('/ui/administration/file-ingest');
            await page.waitForSelector('[data-testid="manual-file-ingest"]', { timeout: 15_000 });
        });

        await test.step('Upload domain JSON via UI', async () => {
            const uploadBtn = page.getByTestId('file-ingest_button-upload-files');
            await expect(uploadBtn).toBeVisible({ timeout: 10_000 });
            await expect(uploadBtn).toBeEnabled({ timeout: 10_000 });
            await uploadBtn.click();

            const dialog = page.locator('[role="dialog"]');
            await expect(dialog).toBeVisible({ timeout: 5_000 });

            const fileInput = page.getByTestId('ingest-file-upload');
            await fileInput.setInputFiles({
                name: 'ingest-domains.json',
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
        });

        await test.step('Upload computer JSON via UI', async () => {
            const uploadBtn = page.getByTestId('file-ingest_button-upload-files');
            await expect(uploadBtn).toBeVisible({ timeout: 10_000 });
            await expect(uploadBtn).toBeEnabled({ timeout: 10_000 });
            await uploadBtn.click();

            const dialog = page.locator('[role="dialog"]');
            await expect(dialog).toBeVisible({ timeout: 5_000 });

            const fileInput = page.getByTestId('ingest-file-upload');
            await fileInput.setInputFiles({
                name: 'ingest-computers.json',
                mimeType: 'application/json',
                buffer: Buffer.from(MINIMAL_COMPUTER_JSON),
            });

            const confirmBtn = page.getByTestId('confirmation-dialog_button-yes');
            await expect(confirmBtn).toBeEnabled({ timeout: 5_000 });
            await confirmBtn.click();

            await expect(dialog.locator('text=/successfully.*uploaded/i')).toBeVisible({ timeout: 30_000 });

            const closeBtn = page.getByTestId('confirmation-dialog_button-no');
            await closeBtn.click();
            await expect(dialog).not.toBeVisible({ timeout: 5_000 });
        });

        // Step 2: Wait for ingest to complete
        await test.step('Wait for ingest jobs to finish processing', async () => {
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
        });

        // Step 3: Trigger analysis via the BloodHound Configuration page UI
        await test.step('Navigate to BloodHound Configuration and trigger analysis', async () => {
            await page.goto('/ui/administration/bloodhound-configuration');

            const analyzeBtn = page.getByRole('button', { name: /Analyze Now/i });
            await expect(analyzeBtn).toBeVisible({ timeout: 15_000 });
            await expect(analyzeBtn).toBeEnabled({ timeout: 15_000 });

            const analysisResponsePromise = page.waitForResponse(
                (res) => res.url().includes('/api/v2/analysis') && res.request().method() === 'PUT',
                { timeout: 15_000 }
            );

            await analyzeBtn.click();

            const confirmBtn = page.getByRole('button', { name: /Confirm/i });
            if (await confirmBtn.isVisible({ timeout: 3_000 }).catch(() => false)) {
                await confirmBtn.click();
            }

            const analysisResponse = await analysisResponsePromise;
            expect(analysisResponse.status(), 'Request analysis should return 202').toBe(202);
        });

        await test.step('Wait for analysis to complete', async () => {
            const analyzeBtn = page.getByRole('button', { name: /Analyze Now/i });
            await expect(analyzeBtn).toBeVisible({ timeout: 60_000 });
            await expect(analyzeBtn).toBeEnabled({ timeout: 60_000 });
        });

        // Step 4: Verify via Cypher query in the UI
        await test.step('Navigate to Explore page', async () => {
            await page.goto('/ui/explore');
            await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });
        });

        const cypherResponsePromise = page.waitForResponse(
            (res) => res.url().includes('/api/v2/graphs/cypher') && res.request().method() === 'POST',
            { timeout: 30_000 }
        ).catch(() => null);

        await test.step('Run Cypher query via the UI', async () => {
            const dialog = page.getByRole('dialog');
            if (await dialog.isVisible({ timeout: 3_000 }).catch(() => false)) {
                await page.keyboard.press('Escape');
                await dialog.waitFor({ state: 'hidden', timeout: 5_000 }).catch(() => {});
            }

            const cypherTab = page.getByTestId('explore_search-container_header_cypher-tab');
            await cypherTab.click();
            await expect(cypherTab).toHaveAttribute('aria-selected', 'true', { timeout: 5_000 });

            const cmEditor = page.locator('.cm-editor');
            await expect(cmEditor.first()).toBeVisible({ timeout: 10_000 });

            const cmContent = page.locator('.cm-content');
            await cmContent.first().click();

            await page.keyboard.press('ControlOrMeta+a');
            await page.keyboard.type("MATCH (n) WHERE n.domain = 'E2ETEST.LOCAL' RETURN n LIMIT 10", { delay: 10 });

            await page.keyboard.press('Shift+Enter');
        });

        await test.step('Verify Cypher API returned nodes', async () => {
            const cypherResponse = await cypherResponsePromise;
            expect(cypherResponse, 'Cypher API response should have been received').not.toBeNull();
            const responseStatus = cypherResponse!.status();
            const responseBody = await cypherResponse!.json();
            const nodeCount = responseBody?.data?.nodes ? Object.keys(responseBody.data.nodes).length : 0;
            expect(responseStatus, `Cypher API should return 200, got ${responseStatus}`).toBe(200);
            expect(nodeCount, 'Cypher response should contain nodes from E2ETEST.LOCAL').toBeGreaterThan(0);
        });
    });

    test('File Ingest page is accessible after login via UI', async ({ page }) => {
        await test.step('Navigate to file ingest administration page', async () => {
            await page.goto('/ui/login');

            const loginForm = page.locator('#username');
            const redirected = page.waitForURL(/\/ui\/(?!login)/, { timeout: 15_000 });
            const formAppeared = loginForm.waitFor({ state: 'visible', timeout: 15_000 }).then(() => 'form' as const);

            const result = await Promise.race([
                redirected.then(() => 'redirected' as const),
                formAppeared,
            ]);

            if (result === 'form') {
                await loginForm.fill(E2E_ADMIN_USERNAME);
                await page.locator('#password').fill(E2E_ADMIN_PASSWORD);
                await page.getByRole('button', { name: 'LOGIN' }).click();
                await page.waitForURL(/\/ui\/(?!login)/, { timeout: 15_000 });
            }
        });

        await test.step('Navigate to Administration > File Ingest', async () => {
            await page.goto('/ui/administration/file-ingest');
            await page.waitForSelector('[data-testid="manual-file-ingest"]', { timeout: 15_000 });
            const fileIngestHeading = page.getByTestId('manual-file-ingest');
            await expect(fileIngestHeading).toBeVisible();
        });
    });
});
