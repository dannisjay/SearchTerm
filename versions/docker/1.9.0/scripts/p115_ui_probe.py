"""Open the admin p115 tab and report whether configured save paths render."""

import asyncio
import sys

from playwright.async_api import async_playwright

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8080"


async def main():
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=True, args=["--no-sandbox"])
        page = await browser.new_page()
        await page.goto(BASE + "/", timeout=30000)
        await page.fill("#username", "searchterm")
        await page.fill("#password", "searchterm")
        await page.click("#login-form button[type=submit]")
        await page.wait_for_selector("#admin-toggle", timeout=10000)
        await page.click("#admin-toggle")
        await page.click('.admin-sidebar button[data-tab="p115"]')
        await page.wait_for_timeout(2500)
        chips = await page.locator("#p115-save-paths .path-chip").count()
        print("path chips:", chips)
        texts = await page.locator("#p115-save-paths .path-chip span").all_text_contents()
        print("chip texts:", texts)
        status = await page.locator("#p115-status").text_content()
        print("status:", status.strip())
        await page.screenshot(path="/tmp/p115_admin.png")
        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())
