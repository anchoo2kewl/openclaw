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
// The Go bot container owns the Telegram long-poll for @clawdy. Make
// absolutely sure the upstream gateway never tries to become a second
// consumer — two processes racing getUpdates drops messages.
cfg.channels = cfg.channels || {};
cfg.channels.telegram = { enabled: false };
fs.writeFileSync(path, JSON.stringify(cfg, null, 2) + "\n", { mode: 0o600 });
console.log("gateway: config merged into " + path);
' "${CONFIG_FILE}"

echo "gateway: starting on :${OPENCLAW_GATEWAY_PORT:-18789}"
exec openclaw gateway --port "${OPENCLAW_GATEWAY_PORT:-18789}" --allow-unconfigured
