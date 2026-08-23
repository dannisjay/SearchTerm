"""Verify stacked site admin blocks and the appearance settings tab."""

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

        blocks = await page.locator("#site-blocks .site-block").count()
        print("site blocks:", blocks)
        names = await page.locator("#site-blocks .site-name").all_text_contents()
        print("block names:", [n.strip() for n in names])
        toggles = await page.locator("#site-blocks .switch-inline .checkbox").count()
        print("toggles:", toggles)
        for i in range(blocks):
            text = (await page.locator(f"#site-blocks .site-block").nth(i).text_content()).replace("\n", " ").strip()
            print(f"block {i}:", " ".join(text.split())[:160])

        await page.click('.admin-sidebar button[data-tab="appearance"]')
        await page.wait_for_timeout(300)
        appearance_text = await page.locator("#tab-appearance").text_content()
        print("appearance text:", " ".join(appearance_text.split())[:120])
        print("has bg input:", await page.locator("#bg-image-url").count() == 1)

        await page.screenshot(path="/tmp/searchterm_admin.png")
        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())
