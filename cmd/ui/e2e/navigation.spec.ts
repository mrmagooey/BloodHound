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

test.describe('Navigation', () => {
    test.beforeEach(async ({ request, page }) => {
        // Verify self endpoint returns a user before proceeding
        const response = await request.get('/api/v2/self');
        const body = await response.json();
        expect(body.data).not.toBeNull();

        // Use an admin page instead of /ui/explore to avoid the NoDataFileUpload
        // dialog that covers the explore page and intercepts pointer events when
        // the graph is empty.
        await page.goto('/ui/administration/file-ingest');
        await page.waitForSelector('main', { timeout: 15_000 });
    });

    test('nav bar is visible on authenticated pages', async ({ page }) => {
        // The nav bar should be rendered (MainNav component)
        const nav = page.locator('nav');
        await expect(nav.first()).toBeVisible({ timeout: 10_000 });
    });

    test('Explore nav link is present', async ({ page }) => {
        const exploreLink = page.getByTestId('global_nav-explore');
        await expect(exploreLink).toBeVisible({ timeout: 10_000 });
    });

    test('Group Management nav link is present', async ({ page }) => {
        // Could be "Group Management" or "Privilege Zones" depending on feature flag
        const groupMgmt = page.getByTestId('global_nav-group-management');
        const privZones = page.getByTestId('global_nav-privilege-zones');
        const either = groupMgmt.or(privZones);
        await expect(either.first()).toBeVisible({ timeout: 10_000 });
    });

    test('Administration nav link is present', async ({ page }) => {
        const adminLink = page.getByTestId('global_nav-administration');
        await expect(adminLink).toBeVisible({ timeout: 10_000 });
    });

    test('Profile nav link is present', async ({ page }) => {
        const profileLink = page.getByTestId('global_nav-my-profile');
        await expect(profileLink).toBeVisible({ timeout: 10_000 });
    });

    test('Download Collectors nav link is present', async ({ page }) => {
        const downloadLink = page.getByTestId('global_nav-download-collectors');
        await expect(downloadLink).toBeVisible({ timeout: 10_000 });
    });

    test('API Explorer nav link is present', async ({ page }) => {
        const apiLink = page.getByTestId('global_nav-api-explorer');
        await expect(apiLink).toBeVisible({ timeout: 10_000 });
    });

    test('clicking Explore nav link navigates to explore page', async ({ page }) => {
        // Navigate away first
        await page.goto('/ui/my-profile');
        await page.waitForSelector('#app-root', { timeout: 15_000 });

        const exploreLink = page.getByTestId('global_nav-explore');
        await exploreLink.click();
        await page.waitForURL(/\/ui\/explore/, { timeout: 10_000 });
        expect(page.url()).toContain('/ui/explore');
    });

    test('clicking Administration nav link navigates to admin page', async ({ page }) => {
        const adminLink = page.getByTestId('global_nav-administration');
        await adminLink.click();
        await page.waitForURL(/\/ui\/administration/, { timeout: 10_000 });
        expect(page.url()).toContain('/ui/administration');
    });

    test('Dark Mode toggle is present', async ({ page }) => {
        const darkModeToggle = page.getByTestId('global_nav-dark-mode');
        await expect(darkModeToggle).toBeVisible({ timeout: 10_000 });
    });

    test('Log Out button is present', async ({ page }) => {
        const logoutBtn = page.getByTestId('global_nav-logout');
        await expect(logoutBtn).toBeVisible({ timeout: 10_000 });
    });
});
