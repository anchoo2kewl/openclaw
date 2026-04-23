#!/usr/bin/env python3
"""
Fetch the top 10 Hacker News stories and send them to Telegram.

Uses the public HN Firebase API (no key required) and Telegram Bot API
for delivery. When TELEGRAM_BOT_TOKEN and TELEGRAM_HOME_CHANNEL are set,
sends the formatted digest directly. Otherwise prints to stdout for
hermes cron to pick up.

No external dependencies — uses only stdlib urllib + json.
"""

import json
import os
import sys
import urllib.request
import urllib.parse
from datetime import datetime, timezone

API = "https://hacker-news.firebaseio.com/v0"
TOP = 10


def fetch_json(url: str):
    with urllib.request.urlopen(url, timeout=10) as resp:
        return json.loads(resp.read())


def fetch_stories():
    top_ids = fetch_json(f"{API}/topstories.json")[:TOP]
    stories = []
    for item_id in top_ids:
        item = fetch_json(f"{API}/item/{item_id}.json")
        if item:
            stories.append(item)
    return stories


def format_digest(stories):
    now = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
    lines = [f"📡 <b>Hacker News Top {TOP}</b> — {now}\n"]

    for i, s in enumerate(stories, 1):
        title = s.get("title", "(no title)")
        url = s.get("url", f"https://news.ycombinator.com/item?id={s['id']}")
        score = s.get("score", 0)
        comments = s.get("descendants", 0)
        ts = s.get("time", 0)
        posted = datetime.fromtimestamp(ts, tz=timezone.utc).strftime("%Y-%m-%d") if ts else "?"
        hn_link = f"https://news.ycombinator.com/item?id={s['id']}"

        lines.append(
            f"<b>{i}.</b> <a href=\"{url}\">{title}</a>\n"
            f"    ⬆ {score} pts · 💬 <a href=\"{hn_link}\">{comments} comments</a> · {posted}"
        )

    return "\n\n".join(lines)


def format_plain(stories):
    """Plain text version for stdout (hermes cron context)."""
    now = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
    lines = [f"Hacker News Top {TOP} — fetched {now}\n"]

    for i, s in enumerate(stories, 1):
        title = s.get("title", "(no title)")
        url = s.get("url", f"https://news.ycombinator.com/item?id={s['id']}")
        score = s.get("score", 0)
        comments = s.get("descendants", 0)
        author = s.get("by", "?")
        ts = s.get("time", 0)
        posted = datetime.fromtimestamp(ts, tz=timezone.utc).strftime("%Y-%m-%d") if ts else "?"
        hn_link = f"https://news.ycombinator.com/item?id={s['id']}"

        lines.append(
            f"{i}. {title}\n"
            f"   Link: {url}\n"
            f"   HN Discussion: {hn_link}\n"
            f"   Points: {score} | Comments: {comments} | By: {author} | Date: {posted}"
        )

    return "\n\n".join(lines)


def send_telegram(text, bot_token, chat_id):
    """Send a message via Telegram Bot API (HTML parse mode)."""
    url = f"https://api.telegram.org/bot{bot_token}/sendMessage"
    data = urllib.parse.urlencode({
        "chat_id": chat_id,
        "text": text,
        "parse_mode": "HTML",
        "disable_web_page_preview": "true",
    }).encode()
    req = urllib.request.Request(url, data=data)
    with urllib.request.urlopen(req, timeout=15) as resp:
        result = json.loads(resp.read())
        if not result.get("ok"):
            print(f"Telegram API error: {result}", file=sys.stderr)
            sys.exit(1)
        return result


def main():
    stories = fetch_stories()

    bot_token = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
    chat_id = os.environ.get("TELEGRAM_HOME_CHANNEL", "").strip()

    if bot_token and chat_id:
        digest = format_digest(stories)
        send_telegram(digest, bot_token, chat_id)
        print(f"Sent HN top {TOP} to Telegram chat {chat_id}")
    else:
        # Stdout mode for hermes cron context
        print(format_plain(stories))


if __name__ == "__main__":
    main()
