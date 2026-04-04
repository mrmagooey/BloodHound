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

test.describe('Explore / Graph Explorer', () => {
    test.beforeEach(async ({ request, page }) => {
        // Verify self endpoint returns a user before proceeding
        const response = await request.get('/api/v2/self');
        const body = await response.json();
        expect(body.data).not.toBeNull();

        await page.goto('/ui/explore');
        await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });
    });

    test('explore page loads with graph container', async ({ page }) => {
        const explore = page.getByTestId('explore');
        await expect(explore).toBeVisible();
    });

    test('search widget is present', async ({ page }) => {
        const searchContainer = page.getByTestId('explore_search-container');
        await expect(searchContainer).toBeVisible({ timeout: 10_000 });
    });

    test('search widget has Search tab', async ({ page }) => {
        const searchTab = page.getByTestId('explore_search-container_header_search-tab');
        await expect(searchTab).toBeVisible({ timeout: 10_000 });
    });

    test('search widget has Pathfinding tab', async ({ page }) => {
        const pathfindingTab = page.getByTestId('explore_search-container_header_pathfinding-tab');
        await expect(pathfindingTab).toBeVisible({ timeout: 10_000 });
    });

    test('search widget has Cypher tab', async ({ page }) => {
        const cypherTab = page.getByTestId('explore_search-container_header_cypher-tab');
        await expect(cypherTab).toBeVisible({ timeout: 10_000 });
    });

    test('search widget can be collapsed and expanded', async ({ page }) => {
        const toggleBtn = page.getByTestId('explore_search-container_header_expand-collapse-button');
        await expect(toggleBtn).toBeVisible({ timeout: 10_000 });

        // Click to collapse
        await toggleBtn.click();
        // Click again to expand
        await toggleBtn.click();
        // The search container header should still be visible
        const header = page.getByTestId('explore_search-container_header');
        await expect(header).toBeVisible();
    });

    test('clicking Cypher tab switches to cypher search', async ({ page }) => {
        const cypherTab = page.getByTestId('explore_search-container_header_cypher-tab');
        await cypherTab.click();

        // The URL should now contain a cypher-related query parameter or the tab should be active
        // Check that the tab is selected (aria-selected attribute)
        await expect(cypherTab).toHaveAttribute('aria-selected', 'true', { timeout: 5_000 });
    });

    test('explore page does not produce console errors', async ({ page }) => {
        const errors: string[] = [];
        page.on('pageerror', (error) => {
            errors.push(error.message);
        });

        // Reload to catch errors from initial load
        await page.reload();
        await page.waitForSelector('[data-testid="explore"]', { timeout: 15_000 });

        expect(errors).toEqual([]);
    });
});
