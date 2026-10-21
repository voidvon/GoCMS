#!/usr/bin/env python3
"""Seed realistic access-log rows for local dashboard testing.

This script only touches gocms_access_log / gocms_access_log_ignore for one
site. It never changes content, categories, templates, or site settings.

Examples:
  python3 scripts/seed-access-logs.py --reset
  python3 scripts/seed-access-logs.py --site 2 --hosts test.local --reset
"""

from __future__ import annotations

import argparse
import json
import posixpath
import random
import sqlite3
import sys
from collections import Counter
from datetime import datetime, timedelta, timezone
from pathlib import Path
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parent.parent
DEFAULT_DB = ROOT / "data" / "site.db"
ACCESS_LOG = "gocms_access_log"
IGNORE_TABLE = "gocms_access_log_ignore"
SEED = 20261021

CHROME = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
EDGE = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0"
IPHONE = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
GOOGLEBOT = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
BINGBOT = "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)"
GPTBOT = "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.2; +https://openai.com/gptbot)"
CURL = "curl/8.4.0"

AGENTS = [
    ("Chrome 141", "browser", "desktop", CHROME),
    ("Chrome 141", "browser", "desktop", CHROME),
    ("Edge 141", "browser", "desktop", EDGE),
    ("Safari", "browser", "mobile", IPHONE),
    ("Googlebot", "bot", "other", GOOGLEBOT),
    ("Bingbot", "bot", "other", BINGBOT),
    ("GPTBot", "bot", "other", GPTBOT),
    ("curl", "tool", "other", CURL),
    ("Unknown Bot", "bot", "other", "Mozilla/5.0 (compatible; scanner/1.0)"),
]

PAGE_PATHS = [
    "/", "/Products/", "/about/", "/news/", "/contact.html", "/service/", "/valve/",
    "/en/", "/en/Products/", "/en/about/", "/en/news/", "/en/service/", "/en/valve/",
    "/Products/340.html", "/Products/27-2.html", "/Products/489-1.html", "/Products/205.html",
    "/Products/74.html", "/Products/112-1.html", "/Products/439.html", "/Products/53-1.html",
    "/Products/497-1.html", "/Products/171-1.html", "/Products/317.html", "/Products/30-1.html",
    "/Products/252.html", "/Products/481.html", "/Products/194.html", "/Products/301.html",
    "/Products/244.html", "/Products/35.html", "/Products/182.html",
]

SCANNER_ERRORS = [
    ("/wp-admin/install.php", 404),
    ("/wp-login.php", 404),
    ("/xmlrpc.php", 404),
    ("/.env", 404),
    ("/.aws/credentials", 403),
    ("/config.json", 404),
    ("/phpinfo.php", 404),
    ("/wp-content/debug.log", 404),
    ("/vendor/phpunit/phpunit/src/Util/PHP/eval-stdin.php", 404),
    ("/ipfs/bafkreicyqcbhpicbos7ev4mrxofwqx6hvvge7pahpta6xuspr44crai5by", 404),
    ("/officialsite", 404),
    ("/Alvin9999/https/fanfan1.net/daohang/", 404),
    ("/app/.env", 404),
    ("/backup.zip", 404),
    ("/database.sql", 404),
    ("/.git/config", 403),
    ("/server-status", 403),
    ("/actuator/env", 404),
]

BROKEN_PAGE_ERRORS = [
    "/products/steam-traps/missing-model/",
    "/products/control-valves/old-series/",
    "/learn-about-steam/old-article/",
    "/news/2025/old-news/",
    "/downloads/catalog.pdf",
    "/uploads/pdfs/202609/missing-datasheet.pdf",
    "/images/global/learn-about-steam/fig-missing.png",
    "/Products/9999.html",
    "/Products/old-product.html",
    "/about/team/old-member/",
    "/contact-us/old-form/",
    "/en/products/missing-model/",
    "/en/learn-about-steam/old-article/",
    "/en/news/2024/old-news/",
    "/en/downloads/catalog.pdf",
    "/zh-CN/products/missing-model/",
    "/zh-CN/old-page/",
    "/service/old-service/",
    "/valve/old-valve/",
    "/customer-stories/missing-case/",
    "/industries/missing-industry/",
    "/sitemap-1.xml",
]

SERVER_ERRORS = [
    ("/api/checkout", 500),
    ("/api/orders", 500),
    ("/search", 500),
    ("/en/checkout", 502),
    ("/contact.html", 500),
    ("/Products/340.html", 502),
]


def fetch_site_hosts(conn: sqlite3.Connection, site_id: int) -> list[str]:
    row = conn.execute(
        'SELECT "domain", "aliases" FROM "gocms_site" WHERE "id" = ?', (site_id,)
    ).fetchone()
    if row is None:
        raise SystemExit(f"site #{site_id} does not exist in {DEFAULT_DB}")
    hosts: list[str] = []
    domain = (row[0] or "").strip().lower()
    if domain:
        hosts.append(domain)
        if not domain.startswith("www."):
            hosts.append("www." + domain)
        else:
            hosts.append(domain[4:])
    try:
        aliases = json.loads(row[1] or "[]")
    except json.JSONDecodeError:
        aliases = []
    for alias in aliases:
        value = str(alias).strip().lower()
        if value:
            hosts.append(value)
    # Make the local fixture useful for the multi-domain dashboard without
    # requiring a language domain to be configured.
    if domain:
        bare = domain[4:] if domain.startswith("www.") else domain
        hosts.append("en." + bare)
    result: list[str] = []
    for host in hosts:
        if host and host not in result:
            result.append(host)
    return result or ["localhost"]


def normalize_ignore_path(value: str) -> str:
    parsed = urlparse(value.strip())
    path = parsed.path or "/"
    if not path.startswith("/"):
        path = "/" + path
    clean = posixpath.normpath(path)
    return "/" if clean in ("", ".") else clean


def referer_host(value: str) -> str:
    if not value:
        return ""
    return (urlparse(value).netloc or "").split(":")[0].lower()


def make_visited(now_utc: datetime, minutes_ago: int) -> tuple[str, str]:
    visited = now_utc - timedelta(minutes=minutes_ago)
    local = visited.astimezone()
    return visited.strftime("%Y-%m-%d %H:%M:%S"), local.strftime("%Y-%m-%d")


def seed(
    conn: sqlite3.Connection,
    site_id: int,
    hosts: list[str],
    reset: bool,
    ignores: list[str],
) -> tuple[int, int, int, int]:
    rng = random.Random(SEED)
    now_utc = datetime.now(timezone.utc).replace(second=0, microsecond=0)
    rows: list[tuple] = []

    # Successful page views give the "热门页面" card enough data to paginate.
    for path in PAGE_PATHS:
        visits = rng.randint(2, 9)
        for index in range(visits):
            host = rng.choices(hosts, weights=[6] + [3] * (len(hosts) - 1), k=1)[0]
            agent = rng.choice(AGENTS[:4] + AGENTS[:4] + [AGENTS[4], AGENTS[7]])
            agent_label, agent_kind, device_kind, ua = agent
            roll = rng.random()
            if roll < 0.55:
                referer = f"https://{hosts[0]}/"
                kind = "internal"
            elif roll < 0.75:
                referer = "https://www.google.com/"
                kind = "external"
            else:
                referer = ""
                kind = "none"
            visited_at, day_key = make_visited(now_utc, rng.randint(10, 2200))
            rows.append((
                site_id, host, day_key, path, "", "GET", 200, "page",
                "text/html; charset=utf-8", f"203.0.113.{rng.randint(10, 240)}",
                referer, referer_host(referer), kind, ua, agent_kind, agent_label,
                device_kind, visited_at,
            ))

    def add_error(
        path: str,
        status: int,
        count: int,
        *,
        scanner: bool = False,
        asset: bool = False,
        server: bool = False,
    ) -> None:
        for index in range(count):
            host = rng.choices(hosts, weights=[6] + [3] * (len(hosts) - 1), k=1)[0]
            if scanner:
                agent = rng.choice(AGENTS[4:])
                agent_label, agent_kind, device_kind, ua = agent
                roll = rng.random()
                if roll < 0.45:
                    referer, kind = "", "none"
                elif roll < 0.75:
                    referer, kind = "https://www.bing.com/", "external"
                else:
                    # Scanner spoofing a same-site path that does not exist:
                    # exercises the new "invalid" referer classification.
                    referer = f"https://{hosts[0]}{path}"
                    kind = "invalid"
            elif asset:
                agent_label, agent_kind, device_kind, ua = AGENTS[0]
                referer, kind = f"https://{hosts[0]}/", "internal"
            elif server:
                agent_label, agent_kind, device_kind, ua = rng.choice(AGENTS[:4])
                referer, kind = rng.choice([(f"https://{hosts[0]}/", "internal"), ("https://www.google.com/", "external")])
            else:
                agent_label, agent_kind, device_kind, ua = rng.choice(AGENTS[:4] + [AGENTS[3]])
                referer, kind = f"https://{hosts[0]}/", "internal"
            visited_at, day_key = make_visited(now_utc, rng.randint(1, 2600))
            content_type = "text/plain; charset=utf-8" if asset and status == 404 else "text/html; charset=utf-8"
            rows.append((
                site_id, host, day_key, path, "", "GET", status, "error",
                content_type, f"198.51.100.{rng.randint(10, 250)}",
                referer, referer_host(referer), kind, ua, agent_kind, agent_label,
                device_kind, visited_at,
            ))

    for path, status in SCANNER_ERRORS:
        add_error(path, status, 5 if path == "/wp-admin/install.php" else rng.randint(1, 4), scanner=True)
    for path in BROKEN_PAGE_ERRORS:
        add_error(path, 404, rng.randint(1, 4), asset=path.startswith(("/downloads/", "/uploads/", "/images/")))
    for path, status in SERVER_ERRORS:
        add_error(path, status, rng.randint(1, 3), server=True)

    if reset:
        conn.execute(f'DELETE FROM "{ACCESS_LOG}" WHERE "site_id" = ?', (site_id,))
        conn.execute(f'DELETE FROM "{IGNORE_TABLE}" WHERE "site_id" = ?', (site_id,))
    for path in ignores:
        conn.execute(
            f'INSERT OR IGNORE INTO "{IGNORE_TABLE}" ("site_id", "path") VALUES (?, ?)',
            (site_id, path),
        )
    conn.executemany(
        f'INSERT INTO "{ACCESS_LOG}" ("site_id","host","day_key","path","query","method","status","kind","content_type","ip","referer","referer_host","referer_kind","user_agent","agent_kind","agent_label","device_kind","visited_at") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)',
        rows,
    )
    errors = sum(1 for row in rows if int(row[6]) >= 400)
    return len(rows) - errors, errors, len(rows), len({row[3] for row in rows if int(row[6]) >= 400})


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--db", default=str(DEFAULT_DB), help="SQLite database path")
    parser.add_argument("--site", type=int, default=1, help="site id to seed (default: 1)")
    parser.add_argument("--hosts", default="", help="comma-separated extra hosts (default: derive from site)")
    parser.add_argument("--reset", action="store_true", help="delete this site's existing access logs and ignore rules first")
    parser.add_argument("--ignore", action="append", default=[], help="path to add to the ignore list (repeatable)")
    parser.add_argument("--no-default-ignore", action="store_true", help="do not add /xmlrpc.php to the ignore list")
    args = parser.parse_args()

    db_path = Path(args.db).expanduser().resolve()
    if not db_path.exists():
        raise SystemExit(f"database not found: {db_path}")
    conn = sqlite3.connect(db_path, timeout=10)
    conn.execute("PRAGMA busy_timeout = 10000")
    columns = {row[1] for row in conn.execute(f'PRAGMA table_info("{ACCESS_LOG}")')}
    if "host" not in columns:
        raise SystemExit("gocms_access_log.host is missing; start the new GoCMS binary once first")
    if not conn.execute(
        "SELECT 1 FROM sqlite_master WHERE type='table' AND name=?", (IGNORE_TABLE,)
    ).fetchone():
        raise SystemExit(f"{IGNORE_TABLE} is missing; start the new GoCMS binary once first")

    hosts = [host.strip().lower() for host in args.hosts.split(",") if host.strip()]
    if not hosts:
        hosts = fetch_site_hosts(conn, args.site)
    ignores = [normalize_ignore_path(path) for path in args.ignore if path.strip()]
    if not args.no_default_ignore and "/xmlrpc.php" not in ignores:
        ignores.append("/xmlrpc.php")

    with conn:
        before = conn.execute(
            f'SELECT COUNT(*) FROM "{ACCESS_LOG}" WHERE "site_id" = ?', (args.site,)
        ).fetchone()[0]
        pages, errors, total, error_paths = seed(conn, args.site, hosts, args.reset, ignores)

    print(f"db: {db_path}")
    print(f"site: #{args.site}  hosts: {', '.join(hosts)}")
    print(f"existing rows: {before}  reset: {'yes' if args.reset else 'no'}")
    print(f"seeded: {pages} page views + {errors} errors = {total} rows, {error_paths} distinct error paths")
    print(f"ignored paths: {', '.join(ignores) if ignores else '(none)'}")
    print("top error groups (ignored excluded):")
    query = f'''
        SELECT l."host", l."path", l."status", COUNT(*)
        FROM "{ACCESS_LOG}" l
        WHERE l."site_id" = ? AND l."status" >= 400
          AND NOT EXISTS (SELECT 1 FROM "{IGNORE_TABLE}" i WHERE i."site_id" = l."site_id" AND i."path" = CASE WHEN l."path" = '/' THEN '/' ELSE RTRIM(l."path", '/') END)
        GROUP BY l."host", l."path", l."status"
        ORDER BY COUNT(*) DESC, MAX(l."visited_at") DESC LIMIT 8
    '''
    for host, path, status, hits in conn.execute(query, (args.site,)):
        print(f"  {hits:>3}  {status}  {host:<18} {path}")
    conn.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
