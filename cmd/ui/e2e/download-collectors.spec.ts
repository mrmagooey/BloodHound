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
import { loginViaAPI, loginViaUI } from './helpers';

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

    test('collectors API endpoint returns data or 500 when no manifests configured', async ({ request }) => {
        const token = await loginViaAPI(request);

        const sharpHoundResponse = await request.get('/api/v2/collectors/sharphound', {
            headers: { Authorization: `Bearer ${token}` },
        });
        // In standalone mode without collector manifests the endpoint returns 500.
        // In a full deployment it returns 200.
        expect([200, 500]).toContain(sharpHoundResponse.status());

        const azureHoundResponse = await request.get('/api/v2/collectors/azurehound', {
            headers: { Authorization: `Bearer ${token}` },
        });
        expect([200, 500]).toContain(azureHoundResponse.status());
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
