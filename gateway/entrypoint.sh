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
fs.writeFileSync(path, JSON.stringify(cfg, null, 2) + "\n", { mode: 0o600 });
console.log("gateway: config merged into " + path);
console.log("gateway: allowed origins: " + cfg.gateway.controlUi.allowedOrigins.join(", "));
' "${CONFIG_FILE}"

echo "gateway: starting on :${OPENCLAW_GATEWAY_PORT:-18789}"
exec openclaw gateway --port "${OPENCLAW_GATEWAY_PORT:-18789}" --allow-unconfigured
