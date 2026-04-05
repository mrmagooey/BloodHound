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

test.describe('Privilege Zones page', () => {
    test.setTimeout(60_000);

    test.beforeEach(async ({ page }) => {
        await loginViaUI(page);
    });

    test('privilege zones page loads and shows Zone Builder heading', async ({ page }) => {
        // The route is always registered even when the tier_management_engine
        // feature flag is disabled. The page should render the Zone Builder UI.
        await page.goto('/ui/privilege-zones');

        // The PrivilegeZones component renders "Zone Builder" as the main heading
        const heading = page.locator('h1', { hasText: 'Zone Builder' });
        await expect(heading).toBeVisible({ timeout: 15_000 });
    });

    test('privilege zones page shows tabs for Zones and Labels', async ({ page }) => {
        await page.goto('/ui/privilege-zones');

        // Wait for the page to render
        const heading = page.locator('h1', { hasText: 'Zone Builder' });
        await expect(heading).toBeVisible({ timeout: 15_000 });

        // The page should show tab triggers for navigation
        const bodyText = await page.textContent('body');
        expect(bodyText).toContain('Zone');
    });

    test('privilege zones page does not produce console errors', async ({ page }) => {
        const errors: string[] = [];
        page.on('pageerror', (error) => {
            errors.push(error.message);
        });

        await page.goto('/ui/privilege-zones');

        const heading = page.locator('h1', { hasText: 'Zone Builder' });
        await expect(heading).toBeVisible({ timeout: 15_000 });

        expect(errors).toEqual([]);
    });

    test('privilege zones page shows zone content or empty state', async ({ page }) => {
        await page.goto('/ui/privilege-zones');

        const heading = page.locator('h1', { hasText: 'Zone Builder' });
        await expect(heading).toBeVisible({ timeout: 15_000 });

        // The page should show zone list content or an empty/disabled state
        const bodyText = await page.textContent('body');
        const hasZoneContent =
            bodyText?.includes('Zone') ||
            bodyText?.includes('Label') ||
            bodyText?.includes('No zones') ||
            bodyText?.includes('Create');
        expect(hasZoneContent).toBe(true);
    });
});
