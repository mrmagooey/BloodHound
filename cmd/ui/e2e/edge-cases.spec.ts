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

test.describe('404 Not Found page', () => {
    test.setTimeout(60_000);

    test('navigating to an unknown route shows the 404 page', async ({ page }) => {
        await loginViaUI(page);

        await page.goto('/ui/this-route-does-not-exist-at-all');

        // The NotFound component renders a 404 alert and a "Go to Explore" button
        const goToExplore = page.getByTestId('page-not-found-go-to-explore');
        await expect(goToExplore).toBeVisible({ timeout: 15_000 });

        const bodyText = await page.textContent('body');
        expect(bodyText).toContain('404');
        expect(bodyText).toContain('Page not found');
    });

    test('"Go to Explore" button on 404 page navigates to explore', async ({ page }) => {
        await loginViaUI(page);

        await page.goto('/ui/this-route-does-not-exist-at-all');

        const goToExplore = page.getByTestId('page-not-found-go-to-explore');
        await expect(goToExplore).toBeVisible({ timeout: 15_000 });
        await goToExplore.click();

        await page.waitForURL(/\/ui\/explore/, { timeout: 15_000 });
        expect(page.url()).toContain('/ui/explore');
    });
});

test.describe('Disabled User page', () => {
    test.setTimeout(60_000);

    test('disabled user page renders with warning and back-to-login button', async ({ page }) => {
        await loginViaUI(page);

        await page.goto('/ui/user-disabled');

        // The DisabledUser component shows a warning alert and a "Back to Login" button
        const backToLogin = page.getByTestId('disabled-user-back-to-login');
        await expect(backToLogin).toBeVisible({ timeout: 15_000 });

        const bodyText = await page.textContent('body');
        expect(bodyText).toContain('Your Account Has Been Disabled');
        expect(bodyText).toContain('system administrator');
    });
});

test.describe('Expired Password page', () => {
    test.setTimeout(60_000);

    test('expired password page redirects when auth is not expired', async ({ page }) => {
        await loginViaUI(page);

        // In standalone mode the admin password is not expired, so navigating
        // to /expired-password should redirect away (to home / explore)
        await page.goto('/ui/expired-password');

        // Should redirect away from expired-password since auth is not expired
        await page.waitForURL(/\/ui\/(?!expired-password)/, { timeout: 15_000 });
        expect(page.url()).not.toContain('/ui/expired-password');
    });
});
