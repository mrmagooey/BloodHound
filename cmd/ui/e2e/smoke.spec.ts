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

test.describe('Smoke tests', () => {
    test('app loads and auth works', async ({ page }) => {
        await page.goto('/ui/explore');
        await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });
        expect(page.url()).toContain('/ui/explore');
    });

    test('root URL redirects to /ui', async ({ page }) => {
        const response = await page.goto('/');
        // After redirects, we should land on /ui or /ui/
        expect(page.url()).toContain('/ui');
        expect(response?.status()).toBeLessThan(400);
    });

    test('UI serves HTML at /ui/', async ({ page }) => {
        await page.goto('/ui/');
        await page.waitForURL(/\/ui\//, { timeout: 10_000 });
        const html = await page.content();
        expect(html).toContain('<!DOCTYPE html>');
    });

    test('static assets are served', async ({ page }) => {
        await page.goto('/ui/');
        await page.waitForURL(/\/ui\//, { timeout: 10_000 });

        // The page should contain a script tag pointing to a JS bundle
        const jsScript = page.locator('script[src*=".js"]');
        const count = await jsScript.count();
        expect(count).toBeGreaterThan(0);
    });
});
