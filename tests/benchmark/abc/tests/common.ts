import { Page } from "@playwright/test";

export async function printLogs(page: Page) {
    page.on('pageerror', (e) => console.error('[pageerror]', e));
    page.on('console', (m) => console.log('[browser]', m.text()));
}