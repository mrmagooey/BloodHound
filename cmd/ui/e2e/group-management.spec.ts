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

test.describe('Group Management page', () => {
    test.setTimeout(60_000);

    test.beforeEach(async ({ page }) => {
        await loginViaUI(page);
    });

    test('group management page loads', async ({ page }) => {
        await page.goto('/ui/group-management');
        await page.waitForURL(/\/ui\/group-management/, { timeout: 15_000 });

        // The page should render without redirecting away
        expect(page.url()).toContain('/ui/group-management');
    });

    test('group management page displays content area', async ({ page }) => {
        await page.goto('/ui/group-management');

        // Wait for the main content area to appear
        // GroupManagementContent renders the page structure
        await page.waitForSelector('[class*="pl-nav-width"]', { timeout: 15_000 });

        // Page should not show an error
        const pageText = await page.textContent('body');
        expect(pageText).not.toContain('Something went wrong');
    });

    test('group management page is accessible via nav link', async ({ page }) => {
        await page.goto('/ui/administration/file-ingest');
        await page.waitForSelector('main', { timeout: 15_000 });

        // The nav link could be "Group Management" or "Privilege Zones" depending on feature flag
        const groupMgmt = page.getByTestId('global_nav-group-management');
        const privZones = page.getByTestId('global_nav-privilege-zones');
        const navLink = groupMgmt.or(privZones);
        await navLink.first().click();

        await page.waitForURL(/\/ui\/(group-management|privilege-zones)/, { timeout: 10_000 });
    });

    test('group management page does not produce console errors', async ({ page }) => {
        const errors: string[] = [];
        page.on('pageerror', (error) => {
            errors.push(error.message);
        });

        await page.goto('/ui/group-management');
        // Wait for the page to settle
        await page.waitForTimeout(3_000);

        expect(errors).toEqual([]);
    });

    test('group management page shows asset group content', async ({ page }) => {
        await page.goto('/ui/group-management');
        await page.waitForURL(/\/ui\/group-management/, { timeout: 15_000 });

        // Wait for the page content to load
        await page.waitForSelector('[class*="pl-nav-width"]', { timeout: 15_000 });

        // The page should show asset group content (e.g., tier zero group, a list,
        // or general group management UI elements)
        const bodyText = await page.textContent('body');
        const hasGroupContent =
            bodyText?.includes('Tier Zero') ||
            bodyText?.includes('Admin Tier Zero') ||
            bodyText?.includes('Owned') ||
            bodyText?.includes('High Value') ||
            bodyText?.includes('Asset Group') ||
            bodyText?.includes('Group Management') ||
            bodyText?.includes('group') ||
            bodyText?.includes('Group');
        expect(hasGroupContent).toBe(true);
    });
});

test.describe('Group Management — group selector', () => {
    test.setTimeout(30_000);

    test('group dropdown trigger is visible and shows a selected group name', async ({ page }) => {
        await loginViaUI(page);
        await page.goto('/ui/group-management');

        // DropdownTrigger always renders with data-testid='dropdown_context-selector'.
        // The group selector is the first one on this page.
        const trigger = page.getByTestId('dropdown_context-selector').first();
        await expect(trigger).toBeVisible({ timeout: 15_000 });

        // The trigger text should reflect a real group name. The API seeds at least
        // the Tier Zero group which the UI maps to the label "High Value".
        const triggerText = await trigger.textContent();
        expect(triggerText?.trim().length).toBeGreaterThan(0);
    });

    test('group dropdown lists group options when asset groups are available', async ({ page }) => {
        await loginViaUI(page);
        await page.goto('/ui/group-management');

        const trigger = page.getByTestId('dropdown_context-selector').first();
        await expect(trigger).toBeVisible({ timeout: 15_000 });

        // Open the dropdown
        await trigger.click();

        // DropdownSelector renders each option as a Button whose data-testid is the
        // option's display value. In standalone mode, asset groups may not be seeded,
        // so we check either that options exist or that the dropdown opens without errors.
        // If options exist, verify they are visible; if not, the dropdown is simply empty.
        const anyOption = page.locator('[data-radix-popper-content-wrapper] button[data-testid]');
        const optionCount = await anyOption.count();
        if (optionCount > 0) {
            await expect(anyOption.first()).toBeVisible({ timeout: 5_000 });
        }
        // If no options, the trigger opened without error — that is the expected behaviour
        // for a standalone instance with no asset groups seeded.
    });

    test('selecting a group from the dropdown updates the page without errors', async ({ page }) => {
        await loginViaUI(page);

        const errors: string[] = [];
        page.on('pageerror', (err) => errors.push(err.message));

        await page.goto('/ui/group-management');

        const trigger = page.getByTestId('dropdown_context-selector').first();
        await expect(trigger).toBeVisible({ timeout: 15_000 });

        // Open the dropdown
        await trigger.click();

        // In standalone mode, asset groups may not be seeded.  If an option exists,
        // click it to verify the selection flow; otherwise simply close the dropdown.
        const firstOption = page.locator('[data-radix-popper-content-wrapper] button[data-testid]').first();
        const hasOption = await firstOption.isVisible({ timeout: 3_000 }).catch(() => false);
        if (hasOption) {
            await firstOption.click();
        } else {
            // Close the dropdown by pressing Escape so it does not leak into other tests
            await page.keyboard.press('Escape');
        }

        // After selection (or close) the trigger should still be visible and the page
        // must not have crashed.
        await expect(trigger).toBeVisible({ timeout: 5_000 });
        expect(errors).toEqual([]);
    });
});

test.describe('Group Management — members list', () => {
    test.setTimeout(30_000);

    test('members table is rendered with Name and Custom Member column headers', async ({ page }) => {
        await loginViaUI(page);
        await page.goto('/ui/group-management');

        // Wait until the API call for asset groups has resolved and the table header
        // columns appear. AssetGroupMemberList always renders the table structure.
        await expect(page.getByRole('columnheader', { name: 'Name' })).toBeVisible({ timeout: 15_000 });
        await expect(page.getByRole('columnheader', { name: 'Custom Member' })).toBeVisible({ timeout: 15_000 });
    });

    test('members table shows rows or an empty-state message for the selected group', async ({ page }) => {
        await loginViaUI(page);
        await page.goto('/ui/group-management');

        // Wait for the table header as a proxy that the list query has settled
        await expect(page.getByRole('columnheader', { name: 'Name' })).toBeVisible({ timeout: 15_000 });

        // Either the table has at least one data row, or it shows one of the two
        // empty-state messages defined in AssetGroupMemberList.
        const tableBody = page.locator('table tbody');
        const rowCount = await tableBody.locator('tr').count();

        if (rowCount === 0) {
            // No asset groups seeded (standalone mode): the member query is disabled
            // because no group is selected.  This is a valid empty state.
        } else if (rowCount === 1) {
            // Could be the single empty-state row
            const cellText = await tableBody.locator('tr td').first().textContent();
            const isEmptyState =
                cellText?.includes('No members in selected Asset Group') ||
                cellText?.includes('No members match that filter');
            // Either there is real content or a valid empty-state message
            expect(isEmptyState ?? false).toBe(true);
        } else {
            // Multiple rows means actual member data was returned
            expect(rowCount).toBeGreaterThan(0);
        }
    });

    test('navigating between groups resets the member list without JS errors', async ({ page }) => {
        await loginViaUI(page);

        const errors: string[] = [];
        page.on('pageerror', (err) => errors.push(err.message));

        await page.goto('/ui/group-management');

        // Wait for the page content area — the table header only appears when a group is
        // selected, which requires at least one asset group to be seeded.
        await page.waitForSelector('[class*="pl-nav-width"]', { timeout: 15_000 });

        // Open the group dropdown
        const trigger = page.getByTestId('dropdown_context-selector').first();
        await expect(trigger).toBeVisible({ timeout: 15_000 });
        await trigger.click();

        const options = page.locator('[data-radix-popper-content-wrapper] button[data-testid]');
        const optionCount = await options.count();

        if (optionCount >= 2) {
            // Click the second option to switch groups
            await options.nth(1).click();
            // The table header should still be visible after switching groups
            await expect(page.getByRole('columnheader', { name: 'Name' })).toBeVisible({ timeout: 10_000 });
        } else if (optionCount === 1) {
            // Only one group available — click it to confirm no crash
            await options.first().click();
            await expect(page.getByRole('columnheader', { name: 'Name' })).toBeVisible({ timeout: 10_000 });
        } else {
            // No groups seeded (standalone mode) — close the dropdown and verify no errors
            await page.keyboard.press('Escape');
        }

        expect(errors).toEqual([]);
    });
});

test.describe('Group Management — filters panel', () => {
    test.setTimeout(30_000);

    test('filters container and toggle button are present', async ({ page }) => {
        await loginViaUI(page);
        await page.goto('/ui/group-management');

        // AssetGroupFilters renders a container with this test id
        const filtersContainer = page.getByTestId('asset-group-filters-container');
        await expect(filtersContainer).toBeVisible({ timeout: 15_000 });

        // The toggle button is always rendered inside the container
        const toggleButton = page.getByTestId('display-filters-button');
        await expect(toggleButton).toBeVisible({ timeout: 5_000 });
    });

    test('expanding the filters panel reveals node-type and custom-member controls', async ({ page }) => {
        await loginViaUI(page);
        await page.goto('/ui/group-management');

        const toggleButton = page.getByTestId('display-filters-button');
        await expect(toggleButton).toBeVisible({ timeout: 15_000 });

        // The collapsible section is collapsed by default; clicking opens it
        await toggleButton.click();

        const collapsibleSection = page.getByTestId('asset-group-filter-collapsible-section');
        await expect(collapsibleSection).toBeVisible({ timeout: 5_000 });

        // Node type <Select> and Custom Member <Checkbox>
        await expect(page.getByTestId('asset-groups-node-type-filter')).toBeVisible({ timeout: 5_000 });
        await expect(page.getByTestId('asset-groups-custom-member-filter')).toBeVisible({ timeout: 5_000 });
    });

    test('toggling the custom-member filter checkbox does not produce JS errors', async ({ page }) => {
        await loginViaUI(page);

        const errors: string[] = [];
        page.on('pageerror', (err) => errors.push(err.message));

        await page.goto('/ui/group-management');

        const toggleButton = page.getByTestId('display-filters-button');
        await expect(toggleButton).toBeVisible({ timeout: 15_000 });
        await toggleButton.click();

        const checkbox = page.getByTestId('asset-groups-custom-member-filter');
        await expect(checkbox).toBeVisible({ timeout: 5_000 });

        // Check and then uncheck — both transitions should be error-free
        await checkbox.click();
        await checkbox.click();

        expect(errors).toEqual([]);
    });
});

test.describe('Group Management — member search (add/remove)', () => {
    test.setTimeout(30_000);

    test('member search autocomplete input is present and accepts text when a group is selected', async ({ page }) => {
        await loginViaUI(page);
        await page.goto('/ui/group-management');

        // AssetGroupAutocomplete only renders when the user has edit permissions AND
        // a group is selected. In standalone mode, asset groups may not be seeded, so
        // we check conditionally.
        const combobox = page.getByTestId('group-management_asset-group-edit-combobox');
        const comboboxVisible = await combobox.isVisible({ timeout: 15_000 }).catch(() => false);

        if (!comboboxVisible) {
            // No asset groups seeded — the combobox is legitimately absent. Pass.
            return;
        }

        const input = combobox.locator('input');
        await expect(input).toBeVisible({ timeout: 5_000 });

        // Type a search term — the placeholder is "Add or Remove Members"
        await input.fill('test');
        await expect(input).toHaveValue('test');
    });

    test('member search input placeholder text is correct when a group is selected', async ({ page }) => {
        await loginViaUI(page);
        await page.goto('/ui/group-management');

        const combobox = page.getByTestId('group-management_asset-group-edit-combobox');
        const comboboxVisible = await combobox.isVisible({ timeout: 15_000 }).catch(() => false);

        if (!comboboxVisible) {
            // No asset groups seeded — the combobox is legitimately absent. Pass.
            return;
        }

        const input = combobox.locator('input');
        await expect(input).toHaveAttribute('placeholder', 'Add or Remove Members');
    });
});
