"""Debug why configured 115 save paths do not render in the admin tab."""

import asyncio
import sys

from playwright.async_api import async_playwright

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8080"


async def main():
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=True, args=["--no-sandbox"])
        page = await browser.new_page()
        page.on("console", lambda msg: print("CONSOLE", msg.type, msg.text))
        page.on("pageerror", lambda exc: print("PAGEERROR", exc))

        await page.add_init_script("""
          const orig = window.renderSavePaths;
          window.__renderCalls = [];
          window.renderSavePaths = function () {
            try {
              const box = document.getElementById('p115-save-paths');
              __renderCalls.push({when: performance.now(), hasBox: !!box, paths: (window.p115State ? window.p115State.savePaths.length : 'n/a')});
            } catch (e) { __renderCalls.push({err: String(e)}); }
            return orig.apply(this, arguments);
          };
        """)

        async def js_eval(expr):
            try:
                return await page.evaluate(expr)
            except Exception as exc:
                return f"<eval error: {exc}>"

        await page.goto(BASE + "/", timeout=30000)
        await page.fill("#username", "searchterm")
        await page.fill("#password", "searchterm")
        await page.click("#login-form button[type=submit]")
        await page.wait_for_selector("#admin-toggle", timeout=10000)
        print("after login, state:", await js_eval("({savePaths: p115State.savePaths, configured: p115State.configured})"))
        print("render calls:", await js_eval("window.__renderCalls"))

        raw = await js_eval("fetch('/api/admin/p115').then(r => r.json())")
        print("raw /api/admin/p115:", raw)

        await page.click("#admin-toggle")
        await page.click('.admin-sidebar button[data-tab="p115"]')
        await page.wait_for_timeout(2500)
        chips = await page.locator("#p115-save-paths .path-chip").count()
        print("path chips after tab:", chips)
        print("save-paths html:", await js_eval("document.getElementById('p115-save-paths').innerHTML"))
        print("state after tab:", await js_eval("({savePaths: p115State.savePaths, configured: p115State.configured})"))
        print("render calls after tab:", await js_eval("window.__renderCalls"))
        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())
