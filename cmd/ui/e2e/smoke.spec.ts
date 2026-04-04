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
    test('API self endpoint returns 200', async ({ request }) => {
        const response = await request.get('/api/v2/self');
        expect(response.status()).toBe(200);
        const body = await response.json();
        // In standalone mode without auth, data is null
        expect(body).toHaveProperty('data');
    });

    test('root URL redirects to /ui', async ({ page }) => {
        const response = await page.goto('/');
        // After redirects, we should land on /ui or /ui/
        expect(page.url()).toContain('/ui');
        expect(response?.status()).toBeLessThan(400);
    });

    test('UI serves HTML at /ui/', async ({ request }) => {
        const response = await request.get('/ui/');
        expect(response.status()).toBe(200);
        const body = await response.text();
        expect(body).toContain('<!DOCTYPE html>');
        expect(body).toContain('BloodHound');
    });

    test('static assets are served', async ({ request }) => {
        // The main HTML references JS and CSS assets
        const htmlResponse = await request.get('/ui/');
        const html = await htmlResponse.text();

        // Extract a JS asset path from the HTML
        const jsMatch = html.match(/src="(\/ui\/assets\/[^"]+\.js)"/);
        expect(jsMatch).not.toBeNull();

        if (jsMatch) {
            const jsResponse = await request.get(jsMatch[1]);
            expect(jsResponse.status()).toBe(200);
        }
    });
});
