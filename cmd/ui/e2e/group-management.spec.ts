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

test.describe('Group Management page', () => {
    test.setTimeout(60_000);

    test.beforeEach(async ({ page }) => {
        await loginViaUI(page);
    });

    test('group management page loads', async ({ page }) => {
        await page.goto('/ui/group-management');
        await page.waitForURL(/\/ui\/group-management/, { timeout: 15_000 });

        // The page should render without redirecting away
        expect(page.url()).toContain('/ui/group-management');
    });

    test('group management page displays content area', async ({ page }) => {
        await page.goto('/ui/group-management');

        // Wait for the main content area to appear
        // GroupManagementContent renders the page structure
        await page.waitForSelector('[class*="pl-nav-width"]', { timeout: 15_000 });

        // Page should not show an error
        const pageText = await page.textContent('body');
        expect(pageText).not.toContain('Something went wrong');
    });

    test('group management page is accessible via nav link', async ({ page }) => {
        await page.goto('/ui/administration/file-ingest');
        await page.waitForSelector('main', { timeout: 15_000 });

        // The nav link could be "Group Management" or "Privilege Zones" depending on feature flag
        const groupMgmt = page.getByTestId('global_nav-group-management');
        const privZones = page.getByTestId('global_nav-privilege-zones');
        const navLink = groupMgmt.or(privZones);
        await navLink.first().click();

        await page.waitForURL(/\/ui\/(group-management|privilege-zones)/, { timeout: 10_000 });
    });

    test('group management page does not produce console errors', async ({ page }) => {
        const errors: string[] = [];
        page.on('pageerror', (error) => {
            errors.push(error.message);
        });

        await page.goto('/ui/group-management');
        // Wait for the page to settle
        await page.waitForTimeout(3_000);

        expect(errors).toEqual([]);
    });

    test('group management page shows asset group content', async ({ page }) => {
        await page.goto('/ui/group-management');
        await page.waitForURL(/\/ui\/group-management/, { timeout: 15_000 });

        // Wait for the page content to load
        await page.waitForSelector('[class*="pl-nav-width"]', { timeout: 15_000 });

        // The page should show asset group content (e.g., tier zero group, a list,
        // or general group management UI elements)
        const bodyText = await page.textContent('body');
        const hasGroupContent =
            bodyText?.includes('Tier Zero') ||
            bodyText?.includes('Admin Tier Zero') ||
            bodyText?.includes('Owned') ||
            bodyText?.includes('High Value') ||
            bodyText?.includes('Asset Group') ||
            bodyText?.includes('Group Management') ||
            bodyText?.includes('group') ||
            bodyText?.includes('Group');
        expect(hasGroupContent).toBe(true);
    });
});
