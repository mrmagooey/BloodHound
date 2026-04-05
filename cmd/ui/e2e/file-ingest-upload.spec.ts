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
import { loginViaUI, pollUntil } from './helpers';

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
                domain: 'UPLOAD-UI-TEST.LOCAL',
                name: 'UPLOAD-UI-TEST.LOCAL',
                distinguishedname: 'DC=UPLOAD-UI-TEST,DC=LOCAL',
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

test.describe('File Ingest Upload via UI', () => {
    test.setTimeout(120_000);

    test('upload a file through the file ingest dialog and verify the job appears', async ({ page, request }) => {
        await loginViaUI(page);

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

        await test.step('Select a file for upload', async () => {
            const dialog = page.locator('[role="dialog"]');
            await expect(dialog).toBeVisible({ timeout: 5_000 });

            // The file input is hidden; use setInputFiles on it directly.
            const fileInput = page.getByTestId('ingest-file-upload');
            await fileInput.setInputFiles({
                name: 'test-domains.json',
                mimeType: 'application/json',
                buffer: Buffer.from(MINIMAL_DOMAIN_JSON),
            });

            // After selecting a file, it should appear in the dialog file list
            await expect(dialog.locator('text=test-domains.json')).toBeVisible({ timeout: 5_000 });
        });

        await test.step('Click Upload and wait for completion', async () => {
            const uploadConfirmBtn = page.getByTestId('confirmation-dialog_button-yes');
            await expect(uploadConfirmBtn).toBeEnabled({ timeout: 5_000 });
            await uploadConfirmBtn.click();

            // Wait for the upload to finish -- the dialog should show a success message
            const dialog = page.locator('[role="dialog"]');
            await expect(dialog.locator('text=/successfully.*uploaded/i')).toBeVisible({ timeout: 30_000 });
        });

        await test.step('Close the dialog', async () => {
            const closeBtn = page.getByTestId('confirmation-dialog_button-no');
            await closeBtn.click();

            // Dialog should close
            const dialog = page.locator('[role="dialog"]');
            await expect(dialog).not.toBeVisible({ timeout: 5_000 });
        });

        await test.step('Verify the ingest job appears in the table', async () => {
            // The file ingest table has a 5s polling interval; wait for the job to appear.
            // Look for a row referencing the admin user or the job ID.
            await pollUntil(
                async () => {
                    const bodyText = await page.textContent('[data-testid="manual-file-ingest"]');
                    // The ingest table should show at least one job row with the admin user email
                    return bodyText !== null && bodyText.includes('admin');
                },
                { timeoutMs: 30_000, intervalMs: 2_000, description: 'ingest job to appear in the table' }
            );
        });
    });
});
