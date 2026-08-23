#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Minimal gying-site probe: challenge solve -> login -> search -> details.
Ported from fish2018/pansou plugin/gying (MIT), for feasibility testing only.
"""
import argparse
import http.cookiejar
import json
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from urllib.parse import urlparse

CHALLENGE_JSON_RE = re.compile(r"const\s+json\s*=\s*(\{.*?\})\s*;\s*const\s+jss\s*=", re.S)
SEARCH_DATA_RE = re.compile(r"_obj\s*\.\s*search\s*=\s*(\{.*?\})\s*;", re.S)
MAGNET_HASH_RE = re.compile(r"^[a-fA-F0-9]{40}$")

BASE_HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
        "(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
    ),
    "Accept": (
        "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,"
        "image/webp,*/*;q=0.8"
    ),
    "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
    "Cache-Control": "no-cache",
    "Pragma": "no-cache",
    "Upgrade-Insecure-Requests": "1",
}


def build_opener():
    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(jar),
        urllib.request.HTTPRedirectHandler(),
    )
    return opener, jar


def raw_request(opener, method, url, data=None, headers=None):
    req_headers = dict(BASE_HEADERS)
    if headers:
        req_headers.update(headers)
    body = None
    if data is not None:
        if isinstance(data, dict):
            body = urllib.parse.urlencode(data).encode("utf-8")
        elif isinstance(data, str):
            body = data.encode("utf-8")
        req_headers.setdefault("Content-Type", "application/x-www-form-urlencoded")
    req = urllib.request.Request(url, data=body, headers=req_headers, method=method)
    try:
        resp = opener.open(req, timeout=60)
        return resp.status, resp.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()
    except Exception as e:
        return -1, str(e).encode("utf-8")


def is_challenge_page(body):
    text = body.decode("utf-8", errors="ignore")
    markers = (
        "powSolve-",
        "pow.worker-",
        "/res/pow",
        "安全验证",
        "正在确认你是不是机器人",
        "正在执行浏览器计算验证",
    )
    return any(m in text for m in markers)


def is_login_shell(body):
    text = body.decode("utf-8", errors="ignore")
    return "_BT.PC.HTML('login')" in text or "_BT.PC.HTML('nologin')" in text


def compute_pow(data):
    n = int(data["N"], 16)
    y = int(data["x"], 16)
    t = int(data["t"])
    start = time.time()
    for _ in range(t):
        y = (y * y) % n
    return format(y, "x"), time.time() - start


def request_with_challenge_retry(opener, method, url, data=None, headers=None, base_url=None):
    for attempt in range(2):
        status, body = raw_request(opener, method, url, data=data, headers=headers)
        if status < 0:
            return status, body
        if not is_challenge_page(body):
            return status, body
        print(f"[probe] challenge on {method} {url} (attempt {attempt + 1})", flush=True)
        if attempt == 1:
            return status, body
        text = body.decode("utf-8", errors="ignore")
        m = CHALLENGE_JSON_RE.search(text)
        if m:
            challenge = json.loads(m.group(1))
            y, elapsed = compute_pow(challenge)
            print(f"[probe] inline PoW solved in {elapsed:.1f}s", flush=True)
            form = {"action": "verify", "id": challenge["id"], "y": y}
            verify_status, verify_body = raw_request(
                opener, "POST", url, data=form, headers={"Referer": url}
            )
        else:
            parsed = urlparse(url)
            pow_url = parsed._replace(path="/res/pow", query="", fragment="").geturl()
            pow_status, pow_body = raw_request(opener, "GET", pow_url, headers={"Referer": url})
            print(f"[probe] remote PoW GET {pow_status}", flush=True)
            try:
                challenge = json.loads(pow_body.decode("utf-8", errors="ignore"))
            except Exception as e:
                print(f"[probe] remote PoW parse failed: {e} {pow_body[:200]!r}", flush=True)
                return status, body
            y, elapsed = compute_pow(challenge)
            print(
                f"[probe] remote PoW solved in {elapsed:.1f}s "
                f"(t={challenge.get('t')}, N={len(challenge.get('N', ''))} hex)",
                flush=True,
            )
            form = {"y": y}
            verify_status, verify_body = raw_request(
                opener, "POST", pow_url, data=form, headers={"Referer": url}
            )
        print(
            f"[probe] challenge verify status={verify_status} body={verify_body[:200]!r}",
            flush=True,
        )
    return status, body


def collect_cookies(jar):
    return {c.name: c.value for c in jar}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", required=True, help="gying base url, e.g. https://www.hgeme.com")
    parser.add_argument("--user", required=True)
    parser.add_argument("--pass", dest="password", required=True)
    parser.add_argument("--keyword", default="钢铁侠")
    parser.add_argument("--limit", type=int, default=5)
    parser.add_argument("--cookie-out", default="/opt/115bot_analysis/gying_cookies.txt")
    args = parser.parse_args()

    base = args.base.rstrip("/")
    opener, jar = build_opener()

    print("[probe] step 1: GET login page", flush=True)
    status, body = request_with_challenge_retry(opener, "GET", base, base_url=base)
    print(f"[probe] login page status={status} bytes={len(body)} challenge={is_challenge_page(body)}", flush=True)

    print("[probe] step 2: POST login", flush=True)
    login_url = base + "/user/login"
    form = {
        "code": "",
        "siteid": "1",
        "dosubmit": "1",
        "cookietime": "10506240",
        "username": args.user,
        "password": args.password,
    }
    status, body = request_with_challenge_retry(
        opener,
        "POST",
        login_url,
        data=form,
        headers={"Referer": base, "Origin": base},
        base_url=base,
    )
    print(f"[probe] login status={status} bytes={len(body)}", flush=True)
    try:
        login_json = json.loads(body.decode("utf-8", errors="ignore"))
        print(f"[probe] login response code={login_json.get('code')} msg={login_json.get('msg') or login_json.get('info')}", flush=True)
        if login_json.get("code") != 200:
            print("[probe] LOGIN FAILED", flush=True)
            sys.exit(2)
    except Exception as e:
        print(f"[probe] login response not JSON: {e} {body[:300]!r}", flush=True)
        sys.exit(2)

    print("[probe] step 3: warmup detail page", flush=True)
    warm_url = base + "/mv/wkMn"
    status, body = request_with_challenge_retry(opener, "GET", warm_url, base_url=base)
    print(f"[probe] warmup status={status} bytes={len(body)}", flush=True)

    cookies = collect_cookies(jar)
    print(f"[probe] cookies: {len(cookies)} -> {sorted(cookies.keys())}", flush=True)
    if args.cookie_out:
        with open(args.cookie_out, "w", encoding="utf-8") as f:
            f.write("; ".join(f"{k}={v}" for k, v in cookies.items()))
        print(f"[probe] cookies saved to {args.cookie_out}", flush=True)

    print(f"[probe] step 4: search {args.keyword}", flush=True)
    search_url = base + "/search?q=" + urllib.parse.quote(args.keyword) + "&type=0&mode=2"
    status, body = request_with_challenge_retry(opener, "GET", search_url, base_url=base)
    text = body.decode("utf-8", errors="ignore")
    print(f"[probe] search status={status} bytes={len(body)} login_shell={is_login_shell(body)}", flush=True)
    m = SEARCH_DATA_RE.search(text)
    if not m:
        print("[probe] no _obj.search JSON found", flush=True)
        sys.exit(3)
    search_data = json.loads(m.group(1))
    items = search_data.get("l", {})
    ids = items.get("i") or []
    titles = items.get("title") or []
    types = items.get("d") or []
    print(
        f"[probe] search q={search_data.get('q')} n={search_data.get('n')} "
        f"items={len(ids)}",
        flush=True,
    )

    for idx in range(min(args.limit, len(ids))):
        title = titles[idx] if idx < len(titles) else "?"
        year = items.get("year") or []
        if idx < len(year) and year[idx]:
            title = f"{title} ({year[idx]})"
        detail_url = f"{base}/res/downurl/{types[idx]}/{ids[idx]}"
        status, body = request_with_challenge_retry(opener, "GET", detail_url, base_url=base)
        print(f"[probe] detail {idx + 1}: {title} status={status}", flush=True)
        try:
            detail = json.loads(body.decode("utf-8", errors="ignore"))
        except Exception as e:
            print(f"[probe] detail parse failed: {e}", flush=True)
            continue
        if detail.get("code") == 403:
            print("[probe] detail code=403, need re-login", flush=True)
            continue
        dl = detail.get("downlist", {}).get("list", {})
        hashes = dl.get("m") or []
        names = dl.get("t") or []
        sizes = dl.get("s") or []
        for j, h in enumerate(hashes):
            if not MAGNET_HASH_RE.match(h or ""):
                continue
            magnet = "magnet:?xt=urn:btih:" + h.lower()
            res_name = names[j] if j < len(names) else title
            size = sizes[j] if j < len(sizes) else "?"
            print(f"  - {magnet}", flush=True)
            print(f"    name={res_name} size={size}", flush=True)
            if j >= 2:
                break


if __name__ == "__main__":
    main()
