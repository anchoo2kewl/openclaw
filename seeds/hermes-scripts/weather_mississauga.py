#!/usr/bin/env python3
"""
Fetch weather for Mississauga (5836 Fieldon Rd area) via wttr.in.
No API key required — uses public wttr.in service + stdlib only.
"""

import json
import os
import sys
import urllib.request
import urllib.parse
from datetime import datetime, timezone

LOCATION = "Mississauga,Ontario,Canada"


def fetch_weather():
    url = f"https://wttr.in/{urllib.parse.quote(LOCATION)}?format=j1"
    req = urllib.request.Request(url, headers={"User-Agent": "curl/8.0"})
    with urllib.request.urlopen(req, timeout=15) as resp:
        return json.loads(resp.read())


def format_telegram(data):
    now = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
    cur = data.get("current_condition", [{}])[0]
    area = data.get("nearest_area", [{}])[0]
    city = area.get("areaName", [{}])[0].get("value", LOCATION)
    region = area.get("region", [{}])[0].get("value", "")

    temp_c = cur.get("temp_C", "?")
    temp_f = cur.get("temp_F", "?")
    feels_c = cur.get("FeelsLikeC", "?")
    feels_f = cur.get("FeelsLikeF", "?")
    desc = cur.get("weatherDesc", [{}])[0].get("value", "?")
    humidity = cur.get("humidity", "?")
    wind_kmph = cur.get("windspeedKmph", "?")
    wind_dir = cur.get("winddir16Point", "?")
    precip = cur.get("precipMM", "0")
    uv = cur.get("uvIndex", "?")
    visibility = cur.get("visibility", "?")

    lines = [
        f"🌤 <b>Weather — {city}, {region}</b>",
        f"📍 5836 Fieldon Rd area · {now}\n",
        f"<b>{desc}</b>",
        f"🌡 <b>{temp_c}°C</b> ({temp_f}°F) · Feels like {feels_c}°C ({feels_f}°F)",
        f"💧 Humidity: {humidity}% · Precip: {precip}mm",
        f"💨 Wind: {wind_kmph} km/h {wind_dir}",
        f"👁 Visibility: {visibility} km · UV: {uv}",
    ]

    # Today's forecast
    forecasts = data.get("weather", [])
    if forecasts:
        today = forecasts[0]
        hi_c = today.get("maxtempC", "?")
        lo_c = today.get("mintempC", "?")
        hi_f = today.get("maxtempF", "?")
        lo_f = today.get("mintempF", "?")
        sunrise = today.get("astronomy", [{}])[0].get("sunrise", "?")
        sunset = today.get("astronomy", [{}])[0].get("sunset", "?")
        lines.append(f"\n📅 <b>Today:</b> High {hi_c}°C ({hi_f}°F) · Low {lo_c}°C ({lo_f}°F)")
        lines.append(f"🌅 Sunrise {sunrise} · Sunset {sunset}")

    # Tomorrow
    if len(forecasts) > 1:
        tmrw = forecasts[1]
        hi_c = tmrw.get("maxtempC", "?")
        lo_c = tmrw.get("mintempC", "?")
        date = tmrw.get("date", "?")
        lines.append(f"📅 <b>Tomorrow ({date}):</b> {hi_c}°C / {lo_c}°C")

    return "\n".join(lines)


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
    data = fetch_weather()
    bot_token = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
    chat_id = os.environ.get("TELEGRAM_HOME_CHANNEL", "").strip()

    if bot_token and chat_id:
        msg = format_telegram(data)
        send_telegram(msg, bot_token, chat_id)
        print(f"Sent weather to Telegram chat {chat_id}")
    else:
        print(format_telegram(data).replace("<b>", "").replace("</b>", "").replace("<i>", "").replace("</i>", ""))


if __name__ == "__main__":
    main()
