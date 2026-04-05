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

test.describe('Download Collectors page', () => {
    test.setTimeout(60_000);

    test('page renders with SharpHound and AzureHound sections', async ({ page }) => {
        await loginViaUI(page);

        await page.goto('/ui/download-collectors');
        await page.waitForSelector('[data-testid="download-collectors"]', { timeout: 15_000 });

        const pageTitle = page.getByTestId('download-collectors');
        await expect(pageTitle).toBeVisible();

        // Page should mention both collector types
        const pageText = await page.textContent('body');
        expect(pageText).toContain('SharpHound');
        expect(pageText).toContain('AzureHound');
    });

    test('download collectors page shows collector content', async ({ page }) => {
        await loginViaUI(page);

        await page.goto('/ui/download-collectors');
        await page.waitForSelector('[data-testid="download-collectors"]', { timeout: 15_000 });

        // The page should show SharpHound and AzureHound content
        const pageText = await page.textContent('body');
        expect(pageText).toContain('SharpHound');
        expect(pageText).toContain('AzureHound');
    });

    test('page does not produce console errors', async ({ page }) => {
        const errors: string[] = [];
        page.on('pageerror', (error) => {
            errors.push(error.message);
        });

        await loginViaUI(page);
        await page.goto('/ui/download-collectors');
        await page.waitForSelector('[data-testid="download-collectors"]', { timeout: 15_000 });

        expect(errors).toEqual([]);
    });
});
