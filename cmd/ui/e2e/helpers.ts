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

import { APIRequestContext, Page, expect } from '@playwright/test';
import { E2E_ADMIN_PASSWORD, E2E_ADMIN_USERNAME } from './global-setup';

/**
 * Log in via the API and return the bearer session token.
 */
export async function loginViaAPI(request: APIRequestContext): Promise<string> {
    const loginResponse = await request.post('/api/v2/login', {
        data: {
            login_method: 'secret',
            username: E2E_ADMIN_USERNAME,
            secret: E2E_ADMIN_PASSWORD,
        },
    });

    expect(loginResponse.status(), 'Login should succeed').toBe(200);
    const loginBody = await loginResponse.json();
    const token = loginBody.data.session_token;
    expect(token, 'Session token should be present').toBeTruthy();
    return token;
}

/**
 * Log in via the UI login form. After this function returns the browser
 * session is authenticated and the page has navigated away from /login.
 */
export async function loginViaUI(page: Page): Promise<void> {
    await page.goto('/ui/login');
    await page.waitForSelector('#username', { timeout: 15_000 });

    await page.locator('#username').fill(E2E_ADMIN_USERNAME);
    await page.locator('#password').fill(E2E_ADMIN_PASSWORD);
    await page.getByRole('button', { name: 'LOGIN' }).click();

    // After successful login the app redirects away from /login
    await page.waitForURL(/\/ui\/(?!login)/, { timeout: 15_000 });
}

/**
 * Poll a condition function until it returns true, with a timeout.
 */
export async function pollUntil(
    fn: () => Promise<boolean>,
    { intervalMs = 2000, timeoutMs = 60_000, description = 'condition' } = {}
): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
        if (await fn()) return;
        await new Promise((r) => setTimeout(r, intervalMs));
    }
    throw new Error(`Timed out waiting for ${description} after ${timeoutMs}ms`);
}
