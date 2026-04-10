# openclaw end-to-end smoke test

Playwright script that exercises the full login → open gateway → wait for
chat flow against a running deployment. Catches regressions that unit
tests miss — WebSocket routing, cookie auth, gateway pairing, etc.

## One-time setup

```bash
cd scripts/e2e
npm install
npx playwright install chromium
```

## Run

```bash
cd scripts/e2e
DASHBOARD_URL=https://claw.biswas.me \
ADMIN_USERNAME=admin \
ADMIN_PASSWORD="<dashboard password from /opt/openclaw/.env>" \
node gateway-smoke.mjs
```

Exits non-zero on any hard failure. Screenshots land in `out/`:

- `01-login.png`
- `02-authed-dashboard.png`
- `03-gateway-first-load.png`
- `04-retry-N.png` — one per pairing retry
- `05-final.png`
- `crash.png` — on unexpected errors
