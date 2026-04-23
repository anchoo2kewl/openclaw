#!/usr/bin/env python3
"""
Fetch stock/crypto pre-market and last close prices via Yahoo Finance.
No API key required — uses Yahoo Finance v8 API + stdlib only.

Tickers: GOOGL, RDDT (Reddit), ^GSPC (S&P 500), ZGLD, BTC-USD, ETH-USD
"""

import json
import os
import sys
import urllib.request
import urllib.parse
from datetime import datetime, timezone

TICKERS = [
    ("GOOGL", "Google (Alphabet)"),
    ("RDDT", "Reddit"),
    ("^GSPC", "S&P 500"),
    ("GC=F", "Gold (USD/oz)"),
    ("BTC-USD", "Bitcoin"),
    ("ETH-USD", "Ethereum"),
]


def fetch_quote(symbol):
    url = f"https://query1.finance.yahoo.com/v8/finance/chart/{urllib.parse.quote(symbol)}?range=1d&interval=1d&includePrePost=true"
    req = urllib.request.Request(url, headers={
        "User-Agent": "Mozilla/5.0",
    })
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read())
        result = data.get("chart", {}).get("result", [])
        if not result:
            return None
        meta = result[0].get("meta", {})
        return {
            "symbol": meta.get("symbol", symbol),
            "currency": meta.get("currency", "USD"),
            "regularMarketPrice": meta.get("regularMarketPrice"),
            "previousClose": meta.get("previousClose") or meta.get("chartPreviousClose"),
            "regularMarketTime": meta.get("regularMarketTime"),
            "preMarketPrice": meta.get("preMarketPrice"),
            "postMarketPrice": meta.get("postMarketPrice"),
            "exchangeName": meta.get("exchangeName", ""),
        }
    except Exception as e:
        print(f"Error fetching {symbol}: {e}", file=sys.stderr)
        return None


def fmt_price(val, currency="USD"):
    if val is None:
        return "—"
    if currency == "USD":
        return f"${val:,.2f}"
    return f"{val:,.2f} {currency}"


def pct_change(current, prev):
    if current is None or prev is None or prev == 0:
        return ""
    change = ((current - prev) / prev) * 100
    arrow = "📈" if change >= 0 else "📉"
    sign = "+" if change >= 0 else ""
    return f" {arrow} {sign}{change:.2f}%"


def format_telegram(quotes):
    now = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
    lines = [f"📊 <b>Morning Market Brief</b> — {now}\n"]

    for symbol, label, q in quotes:
        if q is None:
            lines.append(f"<b>{label}</b> ({symbol}): unavailable")
            continue

        prev = q["previousClose"]
        current = q["regularMarketPrice"]
        pre = q.get("preMarketPrice")
        post = q.get("postMarketPrice")
        currency = q.get("currency", "USD")

        lines.append(f"<b>{label}</b> ({symbol})")
        lines.append(f"  Last Close: {fmt_price(prev, currency)}")

        # Show pre-market if available, otherwise post-market, otherwise current
        if pre is not None:
            change = pct_change(pre, prev)
            lines.append(f"  Pre-Market: {fmt_price(pre, currency)}{change}")
        elif post is not None:
            change = pct_change(post, prev)
            lines.append(f"  After-Hours: {fmt_price(post, currency)}{change}")
        elif current is not None and current != prev:
            change = pct_change(current, prev)
            lines.append(f"  Current: {fmt_price(current, currency)}{change}")

        lines.append("")

    return "\n".join(lines).rstrip()


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
    quotes = []
    for symbol, label in TICKERS:
        q = fetch_quote(symbol)
        quotes.append((symbol, label, q))

    bot_token = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
    chat_id = os.environ.get("TELEGRAM_HOME_CHANNEL", "").strip()

    if bot_token and chat_id:
        msg = format_telegram(quotes)
        send_telegram(msg, bot_token, chat_id)
        print(f"Sent stocks to Telegram chat {chat_id}")
    else:
        print(format_telegram(quotes).replace("<b>", "").replace("</b>", "").replace("<i>", "").replace("</i>", ""))


if __name__ == "__main__":
    main()
