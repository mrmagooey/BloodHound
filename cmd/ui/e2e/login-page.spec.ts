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

test.describe('Login page', () => {
    test.beforeEach(async ({ page }) => {
        await page.goto('/ui/login');
        // Wait for the login form to render
        await page.waitForSelector('#username', { timeout: 10_000 });
    });

    test('displays the login form with email and password fields', async ({ page }) => {
        const emailInput = page.locator('#username');
        const passwordInput = page.locator('#password');

        await expect(emailInput).toBeVisible();
        await expect(passwordInput).toBeVisible();
    });

    test('displays the LOGIN button', async ({ page }) => {
        const loginButton = page.getByRole('button', { name: 'LOGIN' });
        await expect(loginButton).toBeVisible();
    });

    test('email field accepts input', async ({ page }) => {
        const emailInput = page.locator('#username');
        await emailInput.fill('testuser@example.com');
        await expect(emailInput).toHaveValue('testuser@example.com');
    });

    test('password field accepts input and is masked', async ({ page }) => {
        const passwordInput = page.locator('#password');
        await passwordInput.fill('secretpassword');
        await expect(passwordInput).toHaveValue('secretpassword');
        await expect(passwordInput).toHaveAttribute('type', 'password');
    });

    test('navigating to an authenticated route redirects to login', async ({ page }) => {
        await page.goto('/ui/explore');
        // Should redirect back to login since we are not authenticated
        await page.waitForURL(/\/ui\/login/, { timeout: 10_000 });
        expect(page.url()).toContain('/ui/login');
    });
});
