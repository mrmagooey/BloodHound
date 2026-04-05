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

test.describe('My Profile page', () => {
    test.setTimeout(60_000);

    test.beforeEach(async ({ page }) => {
        await loginViaUI(page);
    });

    test('profile page loads and displays user information', async ({ page }) => {
        await page.goto('/ui/my-profile');
        const profileContainer = page.getByTestId('my-profile');
        await expect(profileContainer).toBeVisible({ timeout: 15_000 });

        // Page title should be visible
        const pageText = await page.textContent('body');
        expect(pageText).toContain('My Profile');
        expect(pageText).toContain('User Information');
    });

    test('profile page shows admin user email', async ({ page }) => {
        await page.goto('/ui/my-profile');
        await page.getByTestId('my-profile').waitFor({ timeout: 15_000 });

        // The seeded admin has email admin@e2e.test (from global-setup config)
        const pageText = await page.textContent('body');
        expect(pageText).toContain('admin@e2e.test');
    });

    test('profile page shows admin user name', async ({ page }) => {
        await page.goto('/ui/my-profile');
        await page.getByTestId('my-profile').waitFor({ timeout: 15_000 });

        // The seeded admin has first_name "E2E" and last_name "Admin"
        const pageText = await page.textContent('body');
        expect(pageText).toContain('E2E');
        expect(pageText).toContain('Admin');
    });

    test('profile page shows Authentication section', async ({ page }) => {
        await page.goto('/ui/my-profile');
        await page.getByTestId('my-profile').waitFor({ timeout: 15_000 });

        const pageText = await page.textContent('body');
        expect(pageText).toContain('Authentication');
    });

    test('Reset Password button is present', async ({ page }) => {
        await page.goto('/ui/my-profile');
        await page.getByTestId('my-profile').waitFor({ timeout: 15_000 });

        const resetPasswordBtn = page.getByTestId('my-profile_button-reset-password');
        await expect(resetPasswordBtn).toBeVisible({ timeout: 10_000 });
    });

    test('clicking Reset Password opens password dialog', async ({ page }) => {
        await page.goto('/ui/my-profile');
        await page.getByTestId('my-profile').waitFor({ timeout: 15_000 });

        const resetPasswordBtn = page.getByTestId('my-profile_button-reset-password');
        await resetPasswordBtn.click();

        // A dialog should appear with password fields
        const dialog = page.getByRole('dialog');
        await expect(dialog).toBeVisible({ timeout: 10_000 });
    });

    test('profile page is accessible via nav link', async ({ page }) => {
        await page.goto('/ui/administration/file-ingest');
        await page.waitForSelector('main', { timeout: 15_000 });

        const profileLink = page.getByTestId('global_nav-my-profile');
        await profileLink.click();

        await page.waitForURL(/\/ui\/my-profile/, { timeout: 10_000 });
        const profileContainer = page.getByTestId('my-profile');
        await expect(profileContainer).toBeVisible({ timeout: 15_000 });
    });

    // Covered by UI tests: 'profile page shows admin user email' and 'profile page shows admin user name'
});
