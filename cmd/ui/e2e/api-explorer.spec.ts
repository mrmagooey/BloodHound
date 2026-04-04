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

test.describe('API Explorer page', () => {
    test.setTimeout(60_000);

    test('API Explorer page loads after login', async ({ page }) => {
        await loginViaUI(page);

        await page.goto('/ui/api-explorer');
        // Wait for the page to render content
        await page.waitForSelector('#app-root', { timeout: 15_000 });

        // The URL should stay on api-explorer (not redirect away)
        expect(page.url()).toContain('/ui/api-explorer');
    });

    test('API version endpoint returns expected data', async ({ request }) => {
        const token = await loginViaAPI(request);

        const versionResponse = await request.get('/api/version', {
            headers: { Authorization: `Bearer ${token}` },
        });
        expect(versionResponse.status()).toBe(200);

        const body = await versionResponse.json();
        expect(body.data).toBeTruthy();
        // The version response should contain API version info and server version
        expect(body.data.server_version).toBeTruthy();
    });

    test('page does not produce console errors', async ({ page }) => {
        const errors: string[] = [];
        page.on('pageerror', (error) => {
            errors.push(error.message);
        });

        await loginViaUI(page);
        await page.goto('/ui/api-explorer');
        await page.waitForSelector('#app-root', { timeout: 15_000 });

        expect(errors).toEqual([]);
    });
});
