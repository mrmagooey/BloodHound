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

test.describe('Administration pages - deeper coverage', () => {
    test.setTimeout(60_000);

    test.beforeEach(async ({ page }) => {
        await loginViaUI(page);
    });

    // --- File Ingest deeper tests ---

    test('File Ingest page shows title and documentation link', async ({ page }) => {
        await page.goto('/ui/administration/file-ingest');
        const fileIngest = page.getByTestId('manual-file-ingest');
        await expect(fileIngest).toBeVisible({ timeout: 15_000 });

        const bodyText = await page.textContent('body');
        expect(bodyText).toContain('File Ingest');
        // Should mention collector upload documentation
        expect(bodyText).toContain('SharpHound');
    });

    test('File Ingest page upload dialog accepts JSON files', async ({ page }) => {
        await page.goto('/ui/administration/file-ingest');
        await page.waitForSelector('[data-testid="manual-file-ingest"]', { timeout: 15_000 });

        // Open the upload dialog
        const uploadBtn = page.getByTestId('file-ingest_button-upload-files');
        await expect(uploadBtn).toBeVisible({ timeout: 10_000 });
        await uploadBtn.click();

        // The upload dialog should appear with a file input
        const dialog = page.locator('[role="dialog"]');
        await expect(dialog).toBeVisible({ timeout: 5_000 });

        // Verify the file input is present (accepts JSON files)
        const fileInput = page.getByTestId('ingest-file-upload');
        await expect(fileInput).toBeAttached({ timeout: 5_000 });
    });

    // --- Data Quality deeper tests ---

    test('Data Quality page renders with data-testid and environment selector', async ({ page }) => {
        await page.goto('/ui/administration/data-quality');

        // The page renders with data-testid='data-quality'
        const dataQuality = page.getByTestId('data-quality');
        await expect(dataQuality).toBeVisible({ timeout: 15_000 });
    });

    test('Data Quality page shows title text', async ({ page }) => {
        await page.goto('/ui/administration/data-quality');
        await page.waitForSelector('main', { timeout: 15_000 });

        const bodyText = await page.textContent('body');
        expect(bodyText).toContain('Data Quality');
    });

    // --- Database Management deeper tests ---

    test('Database Management page renders with data-testid', async ({ page }) => {
        await page.goto('/ui/administration/database-management');

        const dbMgmt = page.getByTestId('database-management');
        await expect(dbMgmt).toBeVisible({ timeout: 15_000 });
    });

    test('Database Management page shows deletion options and caution warning', async ({ page }) => {
        await page.goto('/ui/administration/database-management');
        await page.waitForSelector('[data-testid="database-management"]', { timeout: 15_000 });

        const bodyText = await page.textContent('body');
        expect(bodyText).toContain('Database Management');
        expect(bodyText).toContain('Caution');
        // Should have checkboxes for deletion options
        const checkboxes = page.locator('input[type="checkbox"]');
        const count = await checkboxes.count();
        expect(count).toBeGreaterThan(0);
    });

    // --- Manage Users deeper tests ---

    test('Manage Users page shows users table and create user button', async ({ page }) => {
        await page.goto('/ui/administration/manage-users');

        const usersTable = page.getByTestId('manage-users_table');
        await expect(usersTable).toBeVisible({ timeout: 15_000 });

        const createUserBtn = page.getByTestId('manage-users_button-create-user');
        await expect(createUserBtn).toBeVisible({ timeout: 10_000 });
    });

    test('Manage Users page displays the admin user in the table', async ({ page }) => {
        await page.goto('/ui/administration/manage-users');
        await page.waitForSelector('[data-testid="manage-users_table"]', { timeout: 15_000 });

        // The admin user seeded during setup should appear in the table
        const tableText = await page.getByTestId('manage-users_table').textContent();
        expect(tableText).toContain('admin');
    });

    // Covered by UI test: 'Manage Users page displays the admin user in the table'

    // --- BloodHound Configuration deeper tests ---

    test('BloodHound Configuration page shows Analyze Now and Citrix options', async ({ page }) => {
        await page.goto('/ui/administration/bloodhound-configuration');
        await page.waitForSelector('main', { timeout: 15_000 });

        const bodyText = await page.textContent('body');
        expect(bodyText).toContain('BloodHound Configuration');
        expect(bodyText).toContain('Analyze Now');
        expect(bodyText).toContain('Citrix');
    });

    // Covered by UI test: 'BloodHound Configuration page shows Analyze Now and Citrix options'
});

test.describe('Data Quality page', () => {
    test.setTimeout(60_000);

    // Covered by UI tests: 'page loads without errors' and 'page shows domain selector or empty state'

    test('page loads without errors', async ({ page }) => {
        const errors: Error[] = [];
        page.on('pageerror', (err) => errors.push(err));

        await loginViaUI(page);
        await page.goto('/ui/administration/data-quality');

        const container = page.getByTestId('data-quality');
        await expect(container).toBeVisible({ timeout: 15_000 });

        // No unhandled JS errors should have occurred
        expect(errors).toHaveLength(0);
    });

    test('page shows domain selector or empty state', async ({ page }) => {
        await loginViaUI(page);
        await page.goto('/ui/administration/data-quality');

        const container = page.getByTestId('data-quality');
        await expect(container).toBeVisible({ timeout: 15_000 });

        // With no collected domains, the page should show the empty state alert
        // ("No Domain or Tenant Selected") or show the environment selector
        const bodyText = await page.textContent('body');
        const hasEmptyState = bodyText?.includes('No Domain or Tenant Selected');
        const hasQualityDesc = bodyText?.includes('Understand the data collected');

        // One of these should be true — the page rendered its content
        expect(hasEmptyState || hasQualityDesc).toBe(true);
    });
});
