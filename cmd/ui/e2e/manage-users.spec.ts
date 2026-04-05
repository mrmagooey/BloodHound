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
import { E2E_ADMIN_USERNAME, E2E_ADMIN_PASSWORD } from './global-setup';
import { loginViaAPI, loginViaUI } from './helpers';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Create a unique handle for each test run so tests never collide. */
const uniqueSuffix = () => Date.now().toString(36);

interface TestUser {
    principal: string;
    email: string;
    firstName: string;
    lastName: string;
    password: string;
}

function makeTestUser(suffix?: string): TestUser {
    const tag = suffix ?? uniqueSuffix();
    return {
        principal: `e2e-user-${tag}`,
        email: `e2e-user-${tag}@test.local`,
        firstName: 'E2E',
        lastName: `User-${tag}`,
        password: 'E2eTestPw123!',
    };
}

/**
 * Delete a user by principal name via the API, silently ignoring 404s.
 * Useful for afterEach cleanup even when the UI test already deleted the user.
 */
async function deleteUserByPrincipal(
    request: import('@playwright/test').APIRequestContext,
    principal: string,
    token: string
): Promise<void> {
    const listResp = await request.get('/api/v2/bloodhound-users', {
        headers: { Authorization: `Bearer ${token}` },
    });
    if (!listResp.ok()) return;
    const body = await listResp.json();
    const users: Array<{ id: string; principal_name: string }> = body?.data?.users ?? [];
    const target = users.find((u) => u.principal_name === principal);
    if (!target) return;
    await request.delete(`/api/v2/bloodhound-users/${target.id}`, {
        headers: { Authorization: `Bearer ${token}` },
    });
}

/**
 * Navigate to Manage Users and wait for the table to be visible.
 */
async function gotoManageUsers(page: import('@playwright/test').Page): Promise<void> {
    await page.goto('/ui/administration/manage-users');
    await expect(page.getByTestId('manage-users_table')).toBeVisible({ timeout: 15_000 });
}

/**
 * Open the Create User dialog, fill the form, and submit it.
 * The dialog is expected to close on success.
 */
async function createUserViaUI(page: import('@playwright/test').Page, user: TestUser): Promise<void> {
    const createBtn = page.getByTestId('manage-users_button-create-user');
    await expect(createBtn).toBeVisible({ timeout: 10_000 });
    await createBtn.click();

    // Wait for the form to appear inside the dialog
    const form = page.getByTestId('create-user-dialog_form');
    await expect(form).toBeVisible({ timeout: 10_000 });

    await page.locator('#emailAddress').fill(user.email);
    await page.locator('#principal').fill(user.principal);
    await page.locator('#firstName').fill(user.firstName);
    await page.locator('#lastName').fill(user.lastName);
    await page.locator('#secret').fill(user.password);

    await page.getByTestId('create-user-dialog_button-save').click();

    // Dialog should close once the server responds
    await expect(form).not.toBeVisible({ timeout: 15_000 });
}

/**
 * Find the action-menu button for the table row whose first cell matches
 * the given principal name, click it, then click the named menu item.
 *
 * The DataTable renders rows as <tr> elements, with each cell in a <td>.
 * The first cell contains the principal_name.
 */
async function openActionMenuForUser(
    page: import('@playwright/test').Page,
    principal: string,
    menuItem: 'Update User' | 'Delete User' | 'Disable User' | 'Enable User'
): Promise<void> {
    const table = page.getByTestId('manage-users_table');

    // Locate the row that contains the principal name text
    const row = table.locator('tr').filter({ hasText: principal });
    await expect(row).toBeVisible({ timeout: 10_000 });

    // Click the action menu hamburger button within that row
    const menuBtn = row.getByTestId('manage-users_user-row-action-menu-button');
    await menuBtn.click();

    // The StyledMenu renders outside the table; locate by text
    const menuOption = page.getByRole('menuitem', { name: menuItem });
    await expect(menuOption).toBeVisible({ timeout: 5_000 });
    await menuOption.click();
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Manage Users CRUD', () => {
    test.setTimeout(60_000);

    test.beforeEach(async ({ page }) => {
        await loginViaUI(page);
    });

    // -----------------------------------------------------------------------
    // CREATE
    // -----------------------------------------------------------------------

    test('create a new user — user appears in the table', async ({ page, request }) => {
        const user = makeTestUser();
        const token = await loginViaAPI(request);

        try {
            await gotoManageUsers(page);
            await createUserViaUI(page, user);

            // After the dialog closes the table should reload; the new user
            // should appear in it.
            const table = page.getByTestId('manage-users_table');
            await expect(table).toContainText(user.principal, { timeout: 15_000 });
            await expect(table).toContainText(user.email);
        } finally {
            await deleteUserByPrincipal(request, user.principal, token);
        }
    });

    // -----------------------------------------------------------------------
    // EDIT
    // -----------------------------------------------------------------------

    test('edit a user — changed field persists in the table', async ({ page, request }) => {
        const user = makeTestUser();
        const token = await loginViaAPI(request);

        try {
            await gotoManageUsers(page);
            await createUserViaUI(page, user);

            // Confirm the user is visible before editing
            await expect(page.getByTestId('manage-users_table')).toContainText(user.principal, { timeout: 15_000 });

            // Open the Update User dialog via the action menu
            await openActionMenuForUser(page, user.principal, 'Update User');

            // The update form should appear
            const updateForm = page.getByTestId('update-user-dialog_dialog-content');
            await expect(updateForm).toBeVisible({ timeout: 10_000 });

            // Change the last name
            const newLastName = `Updated-${uniqueSuffix()}`;
            const lastNameInput = page.locator('#lastName');
            await lastNameInput.clear();
            await lastNameInput.fill(newLastName);

            await page.getByTestId('update-user-dialog_button-save').click();

            // Dialog should close
            await expect(updateForm).not.toBeVisible({ timeout: 15_000 });

            // The table should now show the updated name
            const expectedFullName = `${user.firstName} ${newLastName}`;
            await expect(page.getByTestId('manage-users_table')).toContainText(expectedFullName, { timeout: 15_000 });
        } finally {
            await deleteUserByPrincipal(request, user.principal, token);
        }
    });

    // -----------------------------------------------------------------------
    // DELETE
    // -----------------------------------------------------------------------

    test('delete a user — user disappears from the table', async ({ page, request }) => {
        const user = makeTestUser();
        const token = await loginViaAPI(request);

        // We create via API so the UI delete test is isolated
        const createResp = await request.post('/api/v2/bloodhound-users', {
            headers: { Authorization: `Bearer ${token}` },
            data: {
                email_address: user.email,
                principal: user.principal,
                first_name: user.firstName,
                last_name: user.lastName,
                secret: user.password,
                needs_password_reset: false,
                roles: [2], // User role
            },
        });
        expect(createResp.status(), 'API user creation should succeed').toBe(200);

        try {
            await gotoManageUsers(page);
            await expect(page.getByTestId('manage-users_table')).toContainText(user.principal, { timeout: 15_000 });

            // Open the Delete User menu item
            await openActionMenuForUser(page, user.principal, 'Delete User');

            // A confirmation dialog should appear.
            // The ConfirmationDialog component places data-testid on the Radix Dialog.Root
            // which is a context-only element and does not produce a DOM node.  Use
            // getByRole('dialog') to locate the rendered dialog content instead.
            const confirmDialog = page.getByRole('dialog');
            await expect(confirmDialog).toBeVisible({ timeout: 5_000 });
            await expect(confirmDialog).toContainText('Are you sure you want to delete this user?');

            // Confirm the deletion
            await page.getByTestId('confirmation-dialog_button-yes').click();

            // Dialog closes
            await expect(confirmDialog).not.toBeVisible({ timeout: 10_000 });

            // User should no longer appear in the table
            await expect(page.getByTestId('manage-users_table')).not.toContainText(user.principal, {
                timeout: 15_000,
            });
        } finally {
            // Best-effort cleanup in case the UI delete failed
            await deleteUserByPrincipal(request, user.principal, token);
        }
    });

    // -----------------------------------------------------------------------
    // DUPLICATE PRINCIPAL NAME
    // -----------------------------------------------------------------------

    test('duplicate principal name — form shows an error', async ({ page }) => {
        await gotoManageUsers(page);

        const createBtn = page.getByTestId('manage-users_button-create-user');
        await expect(createBtn).toBeVisible({ timeout: 10_000 });
        await createBtn.click();

        const form = page.getByTestId('create-user-dialog_form');
        await expect(form).toBeVisible({ timeout: 10_000 });

        // Use the well-known admin principal which is guaranteed to exist
        await page.locator('#emailAddress').fill('conflict@test.local');
        await page.locator('#principal').fill(E2E_ADMIN_USERNAME);
        await page.locator('#firstName').fill('Conflict');
        await page.locator('#lastName').fill('User');
        await page.locator('#secret').fill('ConflictPw123!');

        await page.getByTestId('create-user-dialog_button-save').click();

        // The form should stay open when the server rejects the duplicate principal.
        // The server returns a 409; the exact error surface (inline FormMessage vs toast)
        // may vary by implementation — we only assert the dialog did not close.
        await expect(form).toBeVisible({ timeout: 10_000 });

        // Dismiss the dialog
        await page.getByTestId('create-user-dialog_button-cancel').click();
        await expect(form).not.toBeVisible({ timeout: 5_000 });
    });

    // -----------------------------------------------------------------------
    // NO JS ERRORS
    // -----------------------------------------------------------------------

    test('full CRUD workflow produces no JS errors', async ({ page, request }) => {
        const errors: string[] = [];
        page.on('pageerror', (err) => errors.push(err.message));

        const user = makeTestUser();
        const token = await loginViaAPI(request);

        try {
            // CREATE
            await gotoManageUsers(page);
            await createUserViaUI(page, user);
            await expect(page.getByTestId('manage-users_table')).toContainText(user.principal, { timeout: 15_000 });

            // EDIT
            await openActionMenuForUser(page, user.principal, 'Update User');
            const updateForm = page.getByTestId('update-user-dialog_dialog-content');
            await expect(updateForm).toBeVisible({ timeout: 10_000 });
            const lastNameInput = page.locator('#lastName');
            await lastNameInput.clear();
            await lastNameInput.fill('UpdatedNoError');
            await page.getByTestId('update-user-dialog_button-save').click();
            await expect(updateForm).not.toBeVisible({ timeout: 15_000 });

            // DELETE
            await openActionMenuForUser(page, user.principal, 'Delete User');
            // Use getByRole('dialog') — the ConfirmationDialog places data-testid on
            // Radix Dialog.Root which renders no DOM element.
            const confirmDialog = page.getByRole('dialog');
            await expect(confirmDialog).toBeVisible({ timeout: 5_000 });
            await page.getByTestId('confirmation-dialog_button-yes').click();
            await expect(confirmDialog).not.toBeVisible({ timeout: 10_000 });
            await expect(page.getByTestId('manage-users_table')).not.toContainText(user.principal, {
                timeout: 15_000,
            });
        } finally {
            await deleteUserByPrincipal(request, user.principal, token);
        }

        expect(errors, 'No unhandled JS errors should occur').toHaveLength(0);
    });
});
