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

test.describe('Administration pages', () => {
    test.setTimeout(60_000);

    test.beforeEach(async ({ page }) => {
        await loginViaUI(page);
    });

    test('administration page loads and defaults to File Ingest', async ({ page }) => {
        await page.goto('/ui/administration');
        // The default administration route is file-ingest
        await page.waitForURL(/\/ui\/administration\/file-ingest/, { timeout: 15_000 });
        expect(page.url()).toContain('/ui/administration/file-ingest');
    });

    test('File Ingest sub-page renders', async ({ page }) => {
        await page.goto('/ui/administration/file-ingest');
        const fileIngest = page.getByTestId('manual-file-ingest');
        await expect(fileIngest).toBeVisible({ timeout: 15_000 });
    });

    test('Data Quality sub-page is accessible', async ({ page }) => {
        await page.goto('/ui/administration/data-quality');
        // Wait for the page to load - look for heading or content area
        await page.waitForSelector('main', { timeout: 15_000 });
        // The page should render without errors and the URL should remain correct
        expect(page.url()).toContain('/ui/administration/data-quality');
    });

    test('Database Management sub-page is accessible', async ({ page }) => {
        await page.goto('/ui/administration/database-management');
        await page.waitForSelector('main', { timeout: 15_000 });
        expect(page.url()).toContain('/ui/administration/database-management');
    });

    test('Manage Users sub-page is accessible', async ({ page }) => {
        await page.goto('/ui/administration/manage-users');
        await page.waitForSelector('main', { timeout: 15_000 });
        expect(page.url()).toContain('/ui/administration/manage-users');
        // Should show at least the admin user
        const userContent = page.locator('main');
        await expect(userContent).toBeVisible({ timeout: 10_000 });
    });

    test('BloodHound Configuration sub-page is accessible', async ({ page }) => {
        await page.goto('/ui/administration/bloodhound-configuration');
        await page.waitForSelector('main', { timeout: 15_000 });
        expect(page.url()).toContain('/ui/administration/bloodhound-configuration');
    });

    test('Early Access Features sub-page is accessible', async ({ page }) => {
        await page.goto('/ui/administration/early-access-features');
        await page.waitForSelector('main', { timeout: 15_000 });
        expect(page.url()).toContain('/ui/administration/early-access-features');
    });

    test('administration sub-nav contains expected links', async ({ page }) => {
        await page.goto('/ui/administration/file-ingest');
        await page.waitForSelector('main', { timeout: 15_000 });

        // The sub-navigation should contain links to each admin section
        const subNav = page.locator('nav').or(page.locator('[class*="SubNav"]')).or(page.locator('[role="navigation"]'));
        const firstNav = subNav.first();
        await expect(firstNav).toBeVisible({ timeout: 10_000 });

        // Check for key section text in the page
        const pageText = await page.textContent('body');
        expect(pageText).toContain('File Ingest');
        expect(pageText).toContain('Data Quality');
        expect(pageText).toContain('Manage Users');
    });
});
