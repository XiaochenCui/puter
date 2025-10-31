import { Page } from "@playwright/test";

export async function streamBrowserLogs(page: Page) {
    page.on('pageerror', (e) => console.error('[pageerror]', e));
    page.on('console', (m) => console.log('[browser]', m.text()));
}

export async function getLocalStorage(page: Page) {
    return await page.evaluate(() => {
        const data: Record<string, string> = {};
        for (let i = 0; i < localStorage.length; i++) {
            const key = localStorage.key(i);
            if (key) data[key] = localStorage.getItem(key) || '';
        }
        return data;
    });
}