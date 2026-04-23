#!/usr/bin/env python3
"""
Fetch top 10 world news headlines from Google News RSS.
No API key required — uses public RSS feed + stdlib only.
"""

import json
import os
import sys
import urllib.request
import urllib.parse
import xml.etree.ElementTree as ET
from datetime import datetime, timezone
from html import unescape

FEED_URL = "https://news.google.com/rss/topics/CAAqJggKIiBDQkFTRWdvSUwyMHZNRGx1YlY4U0FtVnVHZ0pWVXlnQVAB?hl=en-US&gl=US&ceid=US:en"
TOP = 10


def fetch_news():
    req = urllib.request.Request(FEED_URL, headers={"User-Agent": "Mozilla/5.0"})
    with urllib.request.urlopen(req, timeout=15) as resp:
        data = resp.read()
    root = ET.fromstring(data)
    items = root.findall(".//item")[:TOP]
    stories = []
    for item in items:
        title = unescape(item.findtext("title", ""))
        link = item.findtext("link", "")
        pub = item.findtext("pubDate", "")
        source = item.findtext("source", "")
        # Clean up title — Google News appends " - Source" to titles
        if " - " in title and source:
            title = title.rsplit(" - ", 1)[0].strip()
        stories.append({"title": title, "link": link, "pub": pub, "source": source})
    return stories


def format_telegram(stories):
    now = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
    lines = [f"🌍 <b>World News Top {TOP}</b> — {now}\n"]
    for i, s in enumerate(stories, 1):
        title = s["title"]
        link = s["link"]
        source = s["source"]
        pub = s["pub"]
        # Parse date
        try:
            dt = datetime.strptime(pub, "%a, %d %b %Y %H:%M:%S %Z")
            date_str = dt.strftime("%b %d")
        except Exception:
            date_str = ""
        src_tag = f" · <i>{source}</i>" if source else ""
        date_tag = f" · {date_str}" if date_str else ""
        lines.append(f"<b>{i}.</b> <a href=\"{link}\">{title}</a>{src_tag}{date_tag}")
    return "\n\n".join(lines)


def send_telegram(text, bot_token, chat_id):
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


def main():
    stories = fetch_news()
    bot_token = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
    chat_id = os.environ.get("TELEGRAM_HOME_CHANNEL", "").strip()

    if bot_token and chat_id:
        msg = format_telegram(stories)
        send_telegram(msg, bot_token, chat_id)
        print(f"Sent world news top {TOP} to Telegram chat {chat_id}")
    else:
        for i, s in enumerate(stories, 1):
            print(f"{i}. {s['title']} ({s['source']})\n   {s['link']}\n")


if __name__ == "__main__":
    main()
