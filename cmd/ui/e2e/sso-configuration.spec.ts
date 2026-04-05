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

test.describe('SSO Configuration page', () => {
    test.setTimeout(60_000);

    test.beforeEach(async ({ page }) => {
        await loginViaUI(page);
    });

    test('SSO configuration page loads and shows title', async ({ page }) => {
        await page.goto('/ui/administration/sso-configuration');
        await page.waitForSelector('main', { timeout: 15_000 });

        expect(page.url()).toContain('/ui/administration/sso-configuration');

        const bodyText = await page.textContent('body');
        expect(bodyText).toContain('SSO Configuration');
    });

    test('SSO configuration page shows Providers list (likely empty)', async ({ page }) => {
        await page.goto('/ui/administration/sso-configuration');
        await page.waitForSelector('main', { timeout: 15_000 });

        // The page should display a "Providers" heading in the list panel
        const bodyText = await page.textContent('body');
        expect(bodyText).toContain('Providers');
    });

    test('SSO configuration page shows create provider button', async ({ page }) => {
        await page.goto('/ui/administration/sso-configuration');
        await page.waitForSelector('main', { timeout: 15_000 });

        // The page should have a "Create" button (either "Create Provider" or "Create SAML Provider")
        const bodyText = await page.textContent('body');
        expect(bodyText).toContain('Create');
    });

    test('SSO configuration page does not produce console errors', async ({ page }) => {
        const errors: string[] = [];
        page.on('pageerror', (error) => {
            errors.push(error.message);
        });

        await page.goto('/ui/administration/sso-configuration');
        await page.waitForSelector('main', { timeout: 15_000 });

        expect(errors).toEqual([]);
    });

    test('SSO providers API endpoint is accessible', async ({ request }) => {
        const token = await loginViaAPI(request);

        const response = await request.get('/api/v2/sso-providers', {
            headers: { Authorization: `Bearer ${token}` },
        });
        // Should return 200 with an empty or populated list
        expect(response.status()).toBe(200);
        const body = await response.json();
        // data may be null (empty list) or an array of providers
        expect(body.data === null || Array.isArray(body.data)).toBe(true);
    });
});
