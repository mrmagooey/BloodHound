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

/**
 * Detect standalone mode by checking if navigating to /ui/login results in an
 * automatic redirect (the StandaloneAuthMiddleware authenticates every request,
 * so the app redirects away from /login immediately).
 */
async function isStandaloneMode(page: import('@playwright/test').Page): Promise<boolean> {
    await page.goto('/ui/login');
    // Race between the login form appearing and an auto-redirect.
    const loginForm = page.locator('#username');
    const redirected = page.waitForURL(/\/ui\/(?!login)/, { timeout: 5_000 }).then(() => true).catch(() => false);
    const formAppeared = loginForm.waitFor({ state: 'visible', timeout: 5_000 }).then(() => false).catch(() => true);
    return Promise.race([redirected, formAppeared]);
}

test.describe('Login page', () => {
    let standalone: boolean;

    test.beforeAll(async ({ browser }) => {
        const page = await browser.newPage();
        standalone = await isStandaloneMode(page);
        await page.close();
    });

    test.beforeEach(async ({ page }) => {
        if (standalone) return; // Skip setup in standalone mode
        await page.goto('/ui/login');
        await page.waitForSelector('#username', { timeout: 10_000 });
    });

    test('displays the login form with email and password fields', async ({ page }) => {
        if (standalone) {
            // In standalone mode, visiting /ui/login auto-redirects because the user
            // is always authenticated. Verify that we end up on an authenticated page.
            await page.goto('/ui/login');
            await page.waitForURL(/\/ui\/(?!login)/, { timeout: 10_000 });
            expect(page.url()).not.toContain('/ui/login');
            return;
        }
        const emailInput = page.locator('#username');
        const passwordInput = page.locator('#password');

        await expect(emailInput).toBeVisible();
        await expect(passwordInput).toBeVisible();
    });

    test('displays the LOGIN button', async ({ page }) => {
        if (standalone) {
            // Auto-authenticated; login button not shown
            await page.goto('/ui/login');
            await page.waitForURL(/\/ui\/(?!login)/, { timeout: 10_000 });
            return;
        }
        const loginButton = page.getByRole('button', { name: 'LOGIN' });
        await expect(loginButton).toBeVisible();
    });

    test('email field accepts input', async ({ page }) => {
        if (standalone) {
            await page.goto('/ui/login');
            await page.waitForURL(/\/ui\/(?!login)/, { timeout: 10_000 });
            return;
        }
        const emailInput = page.locator('#username');
        await emailInput.fill('testuser@example.com');
        await expect(emailInput).toHaveValue('testuser@example.com');
    });

    test('password field accepts input and is masked', async ({ page }) => {
        if (standalone) {
            await page.goto('/ui/login');
            await page.waitForURL(/\/ui\/(?!login)/, { timeout: 10_000 });
            return;
        }
        const passwordInput = page.locator('#password');
        await passwordInput.fill('secretpassword');
        await expect(passwordInput).toHaveValue('secretpassword');
        await expect(passwordInput).toHaveAttribute('type', 'password');
    });

    test('navigating to an authenticated route redirects to login', async ({ page }) => {
        if (standalone) {
            // In standalone mode, navigating to /ui/explore should stay there
            // because the user is always authenticated.
            await page.goto('/ui/explore');
            await page.waitForSelector('[data-testid="explore"]', { timeout: 10_000 });
            expect(page.url()).toContain('/ui/explore');
            return;
        }
        await page.goto('/ui/explore');
        // Should redirect back to login since we are not authenticated
        await page.waitForURL(/\/ui\/login/, { timeout: 10_000 });
        expect(page.url()).toContain('/ui/login');
    });
});
