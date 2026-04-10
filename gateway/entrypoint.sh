#!/usr/bin/env bash
# openclaw gateway entrypoint.
#
# On every start we merge our enforced settings into the persisted
# openclaw.json so that:
#   - gateway.mode is set to "local" (required, or the CLI refuses to boot)
#   - gateway.bind is set to "0.0.0.0" so sibling containers can reach us
#   - gateway.port matches OPENCLAW_GATEWAY_PORT
#   - gateway.auth.token matches OPENCLAW_GATEWAY_TOKEN (shared with the
#     bot container's reverse proxy via docker-compose env_file)
#
# We do the merge with node so we keep whatever else openclaw has written
# to the config over time (sessions, channel state, etc.).
set -euo pipefail

STATE_DIR="${OPENCLAW_STATE_DIR:-/home/gateway/.openclaw}"
CONFIG_FILE="${STATE_DIR}/openclaw.json"
mkdir -p "${STATE_DIR}"

node -e '
const fs = require("fs");
const path = process.argv[1];
let cfg = {};
try { cfg = JSON.parse(fs.readFileSync(path, "utf8")); } catch (_) {}
cfg.gateway = cfg.gateway || {};
cfg.gateway.mode = "local";
cfg.gateway.bind = "0.0.0.0";
cfg.gateway.port = parseInt(process.env.OPENCLAW_GATEWAY_PORT || "18789", 10);
cfg.gateway.auth = cfg.gateway.auth || {};
if (process.env.OPENCLAW_GATEWAY_TOKEN) {
  cfg.gateway.auth.token = process.env.OPENCLAW_GATEWAY_TOKEN;
}
// Public origin(s) that are allowed to connect to the gateway WebSocket.
// We serve the Control UI behind our own reverse proxy, so the browser
// Origin header is whatever the user hits from the outside — we inject
// all configured origins into the allow-list on every restart.
const publicOrigins = (process.env.OPENCLAW_PUBLIC_ORIGINS || "")
  .split(",")
  .map(s => s.trim())
  .filter(Boolean);
cfg.gateway.controlUi = cfg.gateway.controlUi || {};
const existing = new Set(cfg.gateway.controlUi.allowedOrigins || []);
existing.add("http://localhost:18789");
existing.add("http://127.0.0.1:18789");
for (const o of publicOrigins) existing.add(o);
cfg.gateway.controlUi.allowedOrigins = Array.from(existing);
// The Go bot container owns the Telegram long-poll for @clawdy. Make
// absolutely sure the upstream gateway never tries to become a second
// consumer — two processes racing getUpdates drops messages.
cfg.channels = cfg.channels || {};
cfg.channels.telegram = { enabled: false };

// Default agent model: pick a Claude Sonnet via the Anthropic direct
// provider. The ANTHROPIC_API_KEY env var (set via gateway.env from the
// bot container'\''s CLAUDE_CODE_OAUTH_TOKEN) supplies the credential.
// We never persist the key into this JSON file — only the chosen model
// identifier — so the on-disk config is safe to share in screenshots.
cfg.agents = cfg.agents || {};
cfg.agents.defaults = cfg.agents.defaults || {};
cfg.agents.defaults.model = cfg.agents.defaults.model || {};
if (!cfg.agents.defaults.model.primary) {
  cfg.agents.defaults.model.primary = "anthropic/claude-sonnet-4-5";
}

fs.writeFileSync(path, JSON.stringify(cfg, null, 2) + "\n", { mode: 0o600 });
console.log("gateway: config merged into " + path);
console.log("gateway: allowed origins: " + cfg.gateway.controlUi.allowedOrigins.join(", "));
console.log("gateway: default model: " + cfg.agents.defaults.model.primary);
console.log("gateway: anthropic credential: " + (process.env.ANTHROPIC_API_KEY ? "present" : "missing"));
' "${CONFIG_FILE}"

# Background auto-approver: the openclaw Control UI requires a paired
# device before it will show the chat pane, but access to this gateway
# is already gated by the Go dashboard's cookie login (see
# /opt/openclaw/bot/... in the other container). Any request that
# reaches us has therefore already been authenticated, so auto-approving
# new pairing requests is safe and avoids a manual SSH step every time
# a new browser / tab connects.
#
# We poll aggressively (every 1s) because the upstream Control UI
# reports "pairing required" and stops retrying if the approval
# doesn't land inside its own timeout window.
(
    # Give the gateway a moment to open its WS listener before we start
    # poking at it with the CLI client.
    sleep 6
    while true; do
        openclaw devices approve --latest \
            --url "ws://127.0.0.1:${OPENCLAW_GATEWAY_PORT:-18789}" \
            --token "${OPENCLAW_GATEWAY_TOKEN:-}" \
            >/dev/null 2>&1 || true
        sleep 1
    done
) &

echo "gateway: starting on :${OPENCLAW_GATEWAY_PORT:-18789}"
exec openclaw gateway --port "${OPENCLAW_GATEWAY_PORT:-18789}" --allow-unconfigured
