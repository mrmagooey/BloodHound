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

test.describe('Early Access Features interaction', () => {
    test.setTimeout(60_000);

    test.beforeEach(async ({ page }) => {
        await loginViaUI(page);
    });

    test('early access page shows warning dialog that can be dismissed', async ({ page }) => {
        await page.goto('/ui/administration/early-access-features');
        await page.waitForSelector('main', { timeout: 15_000 });

        // The warning dialog should appear automatically
        const warningDialog = page.getByTestId('early-access-features-warning-dialog');
        await expect(warningDialog).toBeVisible({ timeout: 10_000 });

        // It should have the expected warning text
        const dialogText = await warningDialog.textContent();
        expect(dialogText).toContain('under active development');

        // Click the confirm button to dismiss the dialog
        const confirmBtn = page.getByTestId('early-access-features-warning-dialog_button-confirm');
        await confirmBtn.click();

        // The warning dialog should now be hidden
        await expect(warningDialog).not.toBeVisible({ timeout: 5_000 });
    });

    test('early access page shows feature flags after dismissing warning', async ({ page }) => {
        await page.goto('/ui/administration/early-access-features');
        await page.waitForSelector('main', { timeout: 15_000 });

        // Dismiss the warning dialog
        const confirmBtn = page.getByTestId('early-access-features-warning-dialog_button-confirm');
        await expect(confirmBtn).toBeVisible({ timeout: 10_000 });
        await confirmBtn.click();

        // After dismissing, either feature flag toggles or "No Early Access Features Available" should appear
        const pageContainer = page.getByTestId('early-access-features');
        await expect(pageContainer).toBeVisible({ timeout: 10_000 });

        const bodyText = await page.textContent('body');
        const hasFlags = bodyText?.includes('Enabled') || bodyText?.includes('Disabled');
        const hasNoFlags = bodyText?.includes('No Early Access Features Available');

        // One of these states must be true
        expect(hasFlags || hasNoFlags).toBe(true);
    });

    test('clicking "Take me back" on warning dialog navigates away', async ({ page }) => {
        // First go to a known page so "navigate back" has somewhere to go
        await page.goto('/ui/administration/file-ingest');
        await page.waitForSelector('main', { timeout: 15_000 });

        // Now navigate to early access features
        await page.goto('/ui/administration/early-access-features');
        await page.waitForSelector('main', { timeout: 15_000 });

        const cancelBtn = page.getByTestId('early-access-features-warning-dialog_button-close');
        await expect(cancelBtn).toBeVisible({ timeout: 10_000 });
        await cancelBtn.click();

        // Should navigate back (away from early-access-features)
        await page.waitForTimeout(2_000);
        // The URL should no longer be the early-access-features page
        // (it navigates back via navigate(-1), so it goes to file-ingest)
        expect(page.url()).not.toContain('early-access-features');
    });

    // Covered by UI tests: 'early access page shows warning dialog' and 'early access page shows feature flags after dismissing warning'
});

test.describe('Dark Mode toggle', () => {
    test.setTimeout(60_000);

    test.beforeEach(async ({ page }) => {
        await loginViaUI(page);
    });

    test('clicking dark mode toggle changes the color scheme', async ({ page }) => {
        // Navigate to a page that does not fire expensive Cypher queries.
        // The dark mode toggle lives in the global nav and works on any page.
        await page.goto('/ui/administration/file-ingest');
        await page.waitForSelector('main', { timeout: 15_000 });

        // Get the initial background color of the body or root element
        const initialBg = await page.evaluate(() => {
            return window.getComputedStyle(document.body).backgroundColor;
        });

        // Click the dark mode toggle
        const darkModeToggle = page.getByTestId('global_nav-dark-mode');
        await darkModeToggle.click();

        // Wait for the theme transition
        await page.waitForTimeout(1_000);

        // Get the new background color
        const newBg = await page.evaluate(() => {
            return window.getComputedStyle(document.body).backgroundColor;
        });

        // The background color should have changed
        expect(newBg).not.toBe(initialBg);
    });

    test('dark mode toggle can be toggled back', async ({ page }) => {
        await page.goto('/ui/administration/file-ingest');
        await page.waitForSelector('main', { timeout: 15_000 });

        const initialBg = await page.evaluate(() => {
            return window.getComputedStyle(document.body).backgroundColor;
        });

        const darkModeToggle = page.getByTestId('global_nav-dark-mode');

        // Toggle on
        await darkModeToggle.click();
        await page.waitForTimeout(500);

        // Toggle off
        await darkModeToggle.click();
        await page.waitForTimeout(500);

        const restoredBg = await page.evaluate(() => {
            return window.getComputedStyle(document.body).backgroundColor;
        });

        // Background should be back to the initial color
        expect(restoredBg).toBe(initialBg);
    });
});
