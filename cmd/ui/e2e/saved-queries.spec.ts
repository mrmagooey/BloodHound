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

// ─── Test data ───────────────────────────────────────────────────────────────
// Uses a SID prefix that does not collide with other spec files.

const DOMAIN_SID = 'S-1-5-21-9001900190019001-9002900290029002-9003900390039003';

const DOMAINS_JSON = JSON.stringify({
    meta: { methods: 0, type: 'domains', count: 1, version: 6 },
    data: [
        {
            Properties: {
                domain: 'SAVEDQTEST.LOCAL',
                name: 'SAVEDQTEST.LOCAL',
                distinguishedname: 'DC=SAVEDQTEST,DC=LOCAL',
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

const USERS_JSON = JSON.stringify({
    meta: { methods: 0, type: 'users', count: 2, version: 6 },
    data: [
        {
            Properties: {
                domain: 'SAVEDQTEST.LOCAL',
                name: 'ADMINISTRATOR@SAVEDQTEST.LOCAL',
                distinguishedname: 'CN=Administrator,CN=Users,DC=SAVEDQTEST,DC=LOCAL',
                domainsid: DOMAIN_SID,
                samaccountname: 'Administrator',
                enabled: true,
                admincount: true,
            },
            AllowedToDelegate: [],
            PrimaryGroupSID: `${DOMAIN_SID}-513`,
            HasSIDHistory: [],
            SPNTargets: [],
            Aces: [
                {
                    PrincipalSID: `${DOMAIN_SID}-512`,
                    PrincipalType: 'Group',
                    RightName: 'Owns',
                    IsInherited: false,
                },
            ],
            ObjectIdentifier: `${DOMAIN_SID}-500`,
            IsDeleted: false,
            IsACLProtected: false,
        },
        {
            Properties: {
                domain: 'SAVEDQTEST.LOCAL',
                name: 'TESTUSER@SAVEDQTEST.LOCAL',
                distinguishedname: 'CN=TestUser,CN=Users,DC=SAVEDQTEST,DC=LOCAL',
                domainsid: DOMAIN_SID,
                samaccountname: 'TestUser',
                enabled: true,
            },
            AllowedToDelegate: [],
            PrimaryGroupSID: `${DOMAIN_SID}-513`,
            HasSIDHistory: [],
            SPNTargets: [],
            Aces: [],
            ObjectIdentifier: `${DOMAIN_SID}-1001`,
            IsDeleted: false,
            IsACLProtected: false,
        },
    ],
});

const GROUPS_JSON = JSON.stringify({
    meta: { methods: 0, type: 'groups', count: 2, version: 6 },
    data: [
        {
            Properties: {
                domain: 'SAVEDQTEST.LOCAL',
                name: 'DOMAIN ADMINS@SAVEDQTEST.LOCAL',
                distinguishedname: 'CN=Domain Admins,CN=Users,DC=SAVEDQTEST,DC=LOCAL',
                domainsid: DOMAIN_SID,
                samaccountname: 'Domain Admins',
                admincount: true,
            },
            Members: [{ ObjectIdentifier: `${DOMAIN_SID}-500`, ObjectType: 'User' }],
            HasSIDHistory: [],
            Aces: [],
            ObjectIdentifier: `${DOMAIN_SID}-512`,
            IsDeleted: false,
            IsACLProtected: false,
        },
        {
            Properties: {
                domain: 'SAVEDQTEST.LOCAL',
                name: 'DOMAIN USERS@SAVEDQTEST.LOCAL',
                distinguishedname: 'CN=Domain Users,CN=Users,DC=SAVEDQTEST,DC=LOCAL',
                domainsid: DOMAIN_SID,
                samaccountname: 'Domain Users',
            },
            Members: [
                { ObjectIdentifier: `${DOMAIN_SID}-500`, ObjectType: 'User' },
                { ObjectIdentifier: `${DOMAIN_SID}-1001`, ObjectType: 'User' },
            ],
            HasSIDHistory: [],
            Aces: [],
            ObjectIdentifier: `${DOMAIN_SID}-513`,
            IsDeleted: false,
            IsACLProtected: false,
        },
    ],
});

const COMPUTERS_JSON = JSON.stringify({
    meta: { methods: 0, type: 'computers', count: 1, version: 6 },
    data: [
        {
            Properties: {
                domain: 'SAVEDQTEST.LOCAL',
                name: 'DC01.SAVEDQTEST.LOCAL',
                domainsid: DOMAIN_SID,
                distinguishedname: 'CN=DC01,OU=Domain Controllers,DC=SAVEDQTEST,DC=LOCAL',
                operatingsystem: 'Windows Server 2022',
                collected: true,
                enabled: true,
                unconstraineddelegation: false,
                isdc: true,
            },
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
        },
    ],
});

const AZURE_JSON = JSON.stringify({
    meta: { type: 'azure', version: 5, count: 6 },
    data: [
        {
            kind: 'AZTenant',
            data: {
                id: '/tenants/a1b2c3d4-e5f6-7890-abcd-ef1234567890',
                tenantId: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
                displayName: 'SAVEDQTEST Tenant',
                defaultDomain: 'savedqtest.onmicrosoft.com',
                tenantType: 'AAD',
                collected: true,
            },
        },
        {
            kind: 'AZSubscription',
            data: {
                id: '/subscriptions/b2c3d4e5-f6a7-8901-bcde-f12345678901',
                subscriptionId: 'b2c3d4e5-f6a7-8901-bcde-f12345678901',
                displayName: 'SAVEDQTEST Subscription',
                state: 'Enabled',
                tenantId: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
            },
        },
        {
            kind: 'AZUser',
            data: {
                id: 'c3d4e5f6-a7b8-9012-cdef-123456789012',
                displayName: 'Azure Test User',
                accountEnabled: true,
                userPrincipalName: 'azureuser@savedqtest.onmicrosoft.com',
                tenantId: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
            },
        },
        {
            kind: 'AZGroup',
            data: {
                id: 'd4e5f6a7-b8c9-0123-defa-234567890123',
                displayName: 'Azure Test Group',
                securityEnabled: true,
                mailNickname: 'azuretestgroup',
                tenantId: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
            },
        },
        {
            kind: 'AZServicePrincipal',
            data: {
                id: 'e5f6a7b8-c9d0-1234-efab-345678901234',
                displayName: 'Azure Test SP',
                appId: 'f6a7b8c9-d0e1-2345-fabc-456789012345',
                servicePrincipalType: 'Application',
                accountEnabled: true,
                tenantId: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
            },
        },
        {
            kind: 'AZApp',
            data: {
                id: 'f6a7b8c9-d0e1-2345-fabc-456789012345',
                displayName: 'Azure Test App',
                appId: 'f6a7b8c9-d0e1-2345-fabc-456789012345',
                tenantId: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
            },
        },
    ],
});

// ─── Helpers ─────────────────────────────────────────────────────────────────

/**
 * Upload a single JSON file via the File Ingest UI dialog and wait for the
 * "successfully uploaded" confirmation before closing the dialog.
 */
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

/**
 * Poll the ingest table until all jobs reach a terminal state (no more
 * Running/Ingesting/Ready/Analyzing rows) and at least one Complete row exists.
 */
async function waitForIngestComplete(page: Page): Promise<void> {
    await expect(async () => {
        const rows = page.locator('table tbody tr');
        expect(await rows.count(), 'Ingest table should have rows').toBeGreaterThan(0);

        const active = page.locator('table tbody tr').filter({ hasText: /Running|Ingesting|Ready|Analyzing/ });
        expect(await active.count(), 'No in-progress jobs should remain').toBe(0);

        const complete = page.locator('table tbody tr').filter({ hasText: 'Complete' });
        expect(await complete.count(), 'At least one Complete job should exist').toBeGreaterThan(0);
    }).toPass({ intervals: [1_000, 2_000, 2_000], timeout: 90_000 });
}

/**
 * Upload all five data files and wait for the datapipe to automatically ingest
 * + analyse. Runs in a dedicated browser context so it does not interfere with
 * the test page.
 */
async function ingestDataAndWait(browser: Browser): Promise<void> {
    const context = await browser.newContext();
    const page = await context.newPage();
    await loginViaUI(page);

    await page.goto('/ui/administration/file-ingest');
    await page.waitForSelector('[data-testid="manual-file-ingest"]', { timeout: 15_000 });

    await uploadFile(page, 'sq-domains.json', DOMAINS_JSON);
    await uploadFile(page, 'sq-users.json', USERS_JSON);
    await uploadFile(page, 'sq-groups.json', GROUPS_JSON);
    await uploadFile(page, 'sq-computers.json', COMPUTERS_JSON);
    await uploadFile(page, 'sq-azure.json', AZURE_JSON);

    // The datapipe is configured with interval=1 in the E2E server, so ingest
    // and analysis happen automatically. Wait until all jobs show Complete.
    await waitForIngestComplete(page);

    await page.close();
    await context.close();
}

// ─── Tests ───────────────────────────────────────────────────────────────────

test.describe('Saved queries: AD + Azure data', () => {
    test.setTimeout(300_000); // 5 minutes total

    test.beforeAll(async ({ browser }) => {
        await ingestDataAndWait(browser);
    });

    test('all pre-built saved queries execute without errors', async ({ page }) => {
        // Collect JS errors and API errors throughout the test.
        const jsErrors: string[] = [];
        page.on('pageerror', (err) => jsErrors.push(err.message));

        const cypherErrors: Array<{ status: number; url: string; body: string }> = [];
        page.on('response', async (response) => {
            if (
                response.url().includes('/api/v2/graphs/cypher') &&
                response.request().method() === 'POST' &&
                // 404 = empty result set — intentional server behavior, not an error.
                // Flag only unexpected client errors (400-403, 405+) and server errors (5xx).
                (response.status() >= 400 && response.status() !== 404)
            ) {
                let body = '';
                try {
                    body = await response.text();
                } catch {
                    // ignore body read errors
                }
                cypherErrors.push({ status: response.status(), url: response.url(), body });
            }
        });

        // ── 1. Navigate to Explore ──────────────────────────────────────────
        await loginViaUI(page);
        await page.goto('/ui/explore');
        await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });

        // Dismiss any blocking dialog (e.g. "upload data" prompt).
        const dialog = page.getByRole('dialog');
        if (await dialog.isVisible({ timeout: 3_000 }).catch(() => false)) {
            await page.keyboard.press('Escape');
            await dialog.waitFor({ state: 'hidden', timeout: 5_000 }).catch(() => {});
        }

        // ── 2. Switch to the Cypher tab ─────────────────────────────────────
        const cypherTab = page.getByTestId('explore_search-container_header_cypher-tab');
        await expect(cypherTab).toBeVisible({ timeout: 10_000 });
        await cypherTab.click();
        await expect(cypherTab).toHaveAttribute('aria-selected', 'true', { timeout: 5_000 });

        // ── 3. Expand the Saved Queries section if collapsed ────────────────
        // The toggle button controls visibility; the chevron icon indicates state.
        // We need showCommonQueries=true — click the toggle only if the list is hidden.
        const savedQueriesToggle = page.getByTestId('common-queries-toggle');
        await expect(savedQueriesToggle).toBeVisible({ timeout: 10_000 });

        const listSections = page.getByTestId('list-sections');

        // If not yet visible, click the toggle to expand.
        const isVisible = await listSections.isVisible({ timeout: 3_000 }).catch(() => false);
        if (!isVisible) {
            await savedQueriesToggle.click();
        }
        await expect(listSections).toBeVisible({ timeout: 5_000 });

        // ── 4. Collect all query row buttons ────────────────────────────────
        // Pre-built queries render as `div[role="button"]` inside each `<li>`.
        // User-saved queries also have a ListItemActionMenu trigger; we target
        // the inner row button specifically to fire the search directly.
        const queryRows = listSections.locator('li div[role="button"][aria-label="Run pre-built search query"]');
        await expect(queryRows.first()).toBeVisible({ timeout: 10_000 });
        const totalQueries = await queryRows.count();
        expect(totalQueries, 'Should have at least one pre-built query').toBeGreaterThan(0);

        // ── 5. Run every query, one at a time ───────────────────────────────
        // We snapshot the query names upfront so a re-render doesn't shift indices.
        const queryNames: string[] = [];
        for (let i = 0; i < totalQueries; i++) {
            const text = (await queryRows.nth(i).textContent()) ?? `query-${i}`;
            queryNames.push(text.trim());
        }

        for (let i = 0; i < totalQueries; i++) {
            const queryName = queryNames[i];

            // Wait for the response to this specific cypher POST.
            const cypherResponsePromise = page
                .waitForResponse(
                    (res) => res.url().includes('/api/v2/graphs/cypher') && res.request().method() === 'POST',
                    { timeout: 20_000 }
                )
                .catch(() => null);

            // Re-locate the row by index each time — the list may have
            // re-rendered after the previous query ran.
            const row = listSections
                .locator('li div[role="button"][aria-label="Run pre-built search query"]')
                .nth(i);

            // Scroll into view and click.
            await row.scrollIntoViewIfNeeded();
            await row.click();

            // Wait for the cypher API response (or time out gracefully).
            const cypherResponse = await cypherResponsePromise;
            if (cypherResponse !== null) {
                const status = cypherResponse.status();
                // 404 = empty result set — intentional server behavior for sparse data.
                if (status >= 400 && status !== 404) {
                    let body = '';
                    try {
                        body = await cypherResponse.text();
                    } catch {
                        // ignore
                    }
                    cypherErrors.push({ status, url: cypherResponse.url(), body });
                }
                // 200 with empty results is acceptable — not an error.
            }
            // If cypherResponsePromise timed out (null), the query may have been
            // a no-op (e.g. already selected / deselected). That is fine.
        }

        // ── 6. Assertions ───────────────────────────────────────────────────
        const cypherErrorSummary = cypherErrors
            .map((e) => `  [${e.status}] ${e.url}\n    ${e.body.slice(0, 200)}`)
            .join('\n');

        expect(
            cypherErrors,
            `${cypherErrors.length} cypher API call(s) returned error responses:\n${cypherErrorSummary}`
        ).toHaveLength(0);

        const jsErrorSummary = jsErrors.join('\n  ');
        expect(
            jsErrors,
            `JavaScript errors occurred during the test:\n  ${jsErrorSummary}`
        ).toHaveLength(0);
    });
});
