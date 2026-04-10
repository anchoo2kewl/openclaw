"""
openclaw — Telegram bridge to Claude Code.

A single-file long-polling Telegram bot that shells out to the `claude` CLI
in YOLO mode (`--dangerously-skip-permissions`) and streams the response back
to the user. Per-user Claude Code sessions are persisted by `claude --resume`.

Environment variables (all required unless noted):
    TELEGRAM_BOT_TOKEN          — Telegram bot token from @BotFather
    TELEGRAM_ALLOWED_USER_IDS   — Comma-separated numeric Telegram user IDs
    ANTHROPIC_API_KEY           — (optional) API key for claude CLI; if absent,
                                  `claude` must already be logged in on the host
    CLAUDE_MODEL                — (optional) default model
    CLAW_WORKSPACE              — (optional) working dir, default /workspace
"""
from __future__ import annotations

import asyncio
import json
import logging
import os
import shlex
import signal
from dataclasses import dataclass, field
from pathlib import Path
from typing import Dict, Optional

from telegram import Update
from telegram.constants import ChatAction, ParseMode
from telegram.ext import (
    Application,
    CommandHandler,
    ContextTypes,
    MessageHandler,
    filters,
)

# ---------------------------------------------------------------------------
# Config

LOG_LEVEL = os.environ.get("LOG_LEVEL", "INFO").upper()
logging.basicConfig(
    level=LOG_LEVEL,
    format="%(asctime)s %(levelname)s %(name)s: %(message)s",
)
log = logging.getLogger("openclaw")

TELEGRAM_BOT_TOKEN = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
RAW_ALLOWED = os.environ.get("TELEGRAM_ALLOWED_USER_IDS", "").strip()
ALLOWED_USER_IDS = {
    int(x) for x in RAW_ALLOWED.split(",") if x.strip().isdigit()
}
CLAUDE_MODEL = os.environ.get("CLAUDE_MODEL", "").strip()
WORKSPACE = Path(os.environ.get("CLAW_WORKSPACE", "/workspace"))
MAX_TELEGRAM_MESSAGE = 3800  # Telegram hard limit is 4096; leave headroom.

if not TELEGRAM_BOT_TOKEN:
    raise SystemExit("TELEGRAM_BOT_TOKEN is required")
if not ALLOWED_USER_IDS:
    log.warning(
        "TELEGRAM_ALLOWED_USER_IDS is empty — the bot will refuse all users."
    )


# ---------------------------------------------------------------------------
# Session state

@dataclass
class Session:
    """Per-user Claude Code session state."""

    user_id: int
    session_id: Optional[str] = None  # claude --session-id
    cwd: Path = field(default_factory=lambda: WORKSPACE)
    lock: asyncio.Lock = field(default_factory=asyncio.Lock)

    def workspace(self) -> Path:
        p = WORKSPACE / str(self.user_id)
        p.mkdir(parents=True, exist_ok=True)
        return p


SESSIONS: Dict[int, Session] = {}


def get_session(user_id: int) -> Session:
    sess = SESSIONS.get(user_id)
    if sess is None:
        sess = Session(user_id=user_id)
        sess.cwd = sess.workspace()
        SESSIONS[user_id] = sess
    return sess


# ---------------------------------------------------------------------------
# Access control

def is_authorized(update: Update) -> bool:
    if update.effective_user is None:
        return False
    return update.effective_user.id in ALLOWED_USER_IDS


async def deny(update: Update) -> None:
    who = update.effective_user.id if update.effective_user else "?"
    log.warning("denying unauthorized user %s", who)
    if update.message is not None:
        # Plain text on purpose — the allowlist var name has underscores
        # that break Telegram's legacy Markdown parser.
        await update.message.reply_text(
            f"Unauthorized. Your Telegram user id is {who}.\n"
            "Ask the operator to add it to the allowlist."
        )


# ---------------------------------------------------------------------------
# Claude Code invocation

async def run_claude(session: Session, prompt: str) -> str:
    """Run `claude -p` in YOLO mode and capture its final text response."""
    cmd = [
        "claude",
        "-p",
        prompt,
        "--dangerously-skip-permissions",
        "--output-format",
        "json",
    ]
    if CLAUDE_MODEL:
        cmd.extend(["--model", CLAUDE_MODEL])
    if session.session_id:
        cmd.extend(["--resume", session.session_id])

    env = os.environ.copy()
    env.setdefault("CI", "1")

    log.info("user=%s cmd=%s cwd=%s", session.user_id, shlex.join(cmd), session.cwd)

    proc = await asyncio.create_subprocess_exec(
        *cmd,
        cwd=str(session.cwd),
        env=env,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
    )

    try:
        stdout_b, stderr_b = await asyncio.wait_for(proc.communicate(), timeout=60 * 30)
    except asyncio.TimeoutError:
        proc.kill()
        return "⏱️ Claude session timed out after 30 minutes."

    stdout = stdout_b.decode("utf-8", errors="replace")
    stderr = stderr_b.decode("utf-8", errors="replace")

    if proc.returncode != 0:
        log.error("claude exited %s stderr=%s", proc.returncode, stderr[:2000])
        snippet = stderr.strip() or stdout.strip() or "(no output)"
        return f"❌ claude exited {proc.returncode}\n\n```\n{snippet[-1500:]}\n```"

    # `claude -p --output-format json` prints a single JSON object.
    text, new_session_id = _parse_claude_json(stdout)
    if new_session_id:
        session.session_id = new_session_id
    return text or "(claude returned no text)"


def _parse_claude_json(stdout: str) -> tuple[str, Optional[str]]:
    stdout = stdout.strip()
    if not stdout:
        return "", None
    try:
        data = json.loads(stdout)
    except json.JSONDecodeError:
        return stdout, None
    # Shape varies slightly across versions; try common fields.
    text = (
        data.get("result")
        or data.get("text")
        or data.get("response")
        or ""
    )
    if not text and isinstance(data.get("messages"), list):
        parts = []
        for msg in data["messages"]:
            content = msg.get("content", "")
            if isinstance(content, list):
                for item in content:
                    if isinstance(item, dict) and item.get("type") == "text":
                        parts.append(item.get("text", ""))
            elif isinstance(content, str):
                parts.append(content)
        text = "\n".join(p for p in parts if p)
    session_id = data.get("session_id") or data.get("sessionId")
    return text, session_id


# ---------------------------------------------------------------------------
# Handlers

HELP_TEXT = (
    "openclaw\n"
    "Send any message and I'll pass it to Claude Code running on the server.\n\n"
    "Commands:\n"
    "/new — start a fresh Claude session (forget history)\n"
    "/status — show current session info\n"
    "/help — show this message"
)


async def start_cmd(update: Update, _ctx: ContextTypes.DEFAULT_TYPE) -> None:
    if not is_authorized(update):
        return await deny(update)
    await update.message.reply_text(HELP_TEXT)


async def new_cmd(update: Update, _ctx: ContextTypes.DEFAULT_TYPE) -> None:
    if not is_authorized(update):
        return await deny(update)
    sess = get_session(update.effective_user.id)
    sess.session_id = None
    await update.message.reply_text("🧹 New Claude session started.")


async def status_cmd(update: Update, _ctx: ContextTypes.DEFAULT_TYPE) -> None:
    if not is_authorized(update):
        return await deny(update)
    sess = get_session(update.effective_user.id)
    await update.message.reply_text(
        f"session_id: {sess.session_id or '(none)'}\n"
        f"cwd: {sess.cwd}\n"
        f"model: {CLAUDE_MODEL or '(default)'}"
    )


async def on_message(update: Update, _ctx: ContextTypes.DEFAULT_TYPE) -> None:
    if not is_authorized(update):
        return await deny(update)
    if update.message is None or not update.message.text:
        return
    prompt = update.message.text
    session = get_session(update.effective_user.id)

    if session.lock.locked():
        await update.message.reply_text("⏳ Previous request still running, hang on…")
        return

    async with session.lock:
        await update.message.chat.send_action(ChatAction.TYPING)
        try:
            reply = await run_claude(session, prompt)
        except Exception as exc:  # noqa: BLE001
            log.exception("claude failed")
            reply = f"❌ bot error: {exc}"

    for chunk in _chunk(reply, MAX_TELEGRAM_MESSAGE):
        await update.message.reply_text(chunk)


def _chunk(text: str, size: int):
    for i in range(0, len(text), size):
        yield text[i : i + size]


# ---------------------------------------------------------------------------
# Main

def main() -> None:
    WORKSPACE.mkdir(parents=True, exist_ok=True)

    app = Application.builder().token(TELEGRAM_BOT_TOKEN).build()
    app.add_handler(CommandHandler(["start", "help"], start_cmd))
    app.add_handler(CommandHandler("new", new_cmd))
    app.add_handler(CommandHandler("status", status_cmd))
    app.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, on_message))

    log.info(
        "openclaw starting — allowed_users=%s workspace=%s",
        sorted(ALLOWED_USER_IDS),
        WORKSPACE,
    )

    # Graceful shutdown on SIGTERM (docker stop)
    def _stop(*_a):
        log.info("shutdown signal received")

    signal.signal(signal.SIGTERM, _stop)

    app.run_polling(stop_signals=(signal.SIGINT, signal.SIGTERM))


if __name__ == "__main__":
    main()
