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

test.describe('Application loading', () => {
    test('page renders without JavaScript errors', async ({ page }) => {
        const errors: string[] = [];
        page.on('pageerror', (error) => {
            errors.push(error.message);
        });

        await page.goto('/ui');
        // Wait for the React app to mount
        await page.waitForSelector('#root', { timeout: 10_000 });

        // The app should have rendered something inside #root
        const rootContent = await page.locator('#root').innerHTML();
        expect(rootContent.length).toBeGreaterThan(0);

        // No uncaught JS errors
        expect(errors).toEqual([]);
    });

    test('page has correct title', async ({ page }) => {
        await page.goto('/ui');
        await expect(page).toHaveTitle('BloodHound');
    });

    test('page has a favicon', async ({ page }) => {
        await page.goto('/ui');
        const favicon = page.locator('link[rel*="icon"]');
        await expect(favicon.first()).toBeAttached();
    });
});
