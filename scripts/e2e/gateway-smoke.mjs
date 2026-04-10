// Playwright smoke test for openclaw:
//
//   1. Logs in to the Go dashboard at https://claw.biswas.me/ using the
//      admin credentials from DASHBOARD_URL / ADMIN_* env vars.
//   2. Clicks "Open gateway" (or navigates to /gateway-launch directly).
//   3. Waits for the upstream OpenClaw Control UI to either connect
//      successfully (chat input shows up) or display a terminal error
//      like "pairing required" or "origin not allowed".
//   4. If it hits "pairing required", waits a couple of seconds and
//      clicks Connect again — the background auto-approver running in
//      the gateway container should have promoted the pending request
//      by then.
//   5. Writes a screenshot to ./out/<stage>.png and exits non-zero on
//      any hard failure so CI-style loops can detect regressions.
//
// Usage:
//   DASHBOARD_URL=https://claw.biswas.me \
//   ADMIN_USERNAME=admin \
//   ADMIN_PASSWORD=... \
//   node gateway-smoke.mjs

import fs from 'node:fs/promises';
import path from 'node:path';
import { chromium } from 'playwright';

const DASHBOARD_URL = process.env.DASHBOARD_URL || 'https://claw.biswas.me';
const USERNAME      = process.env.ADMIN_USERNAME || 'admin';
const PASSWORD      = process.env.ADMIN_PASSWORD;
if (!PASSWORD) {
  console.error('ADMIN_PASSWORD env var is required');
  process.exit(2);
}

const OUT_DIR = path.join(path.dirname(new URL(import.meta.url).pathname), 'out');
await fs.mkdir(OUT_DIR, { recursive: true });

function log(...args) {
  console.log(new Date().toISOString().slice(11, 19), '·', ...args);
}

async function shot(page, name) {
  const file = path.join(OUT_DIR, `${name}.png`);
  await page.screenshot({ path: file, fullPage: true });
  log('screenshot →', file);
}

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({
  viewport: { width: 1280, height: 900 },
  ignoreHTTPSErrors: true,
});
const page = await context.newPage();

// Mirror browser console + network errors so failures show up in stdout.
page.on('console',  m => log('[console]', m.type(), m.text()));
page.on('pageerror', e => log('[pageerror]', e.message));
page.on('response', r => {
  if (r.status() >= 400) log('[http]', r.status(), r.url());
});

let hardFail = null;
try {
  // 1. Login
  log('goto', DASHBOARD_URL + '/login');
  await page.goto(DASHBOARD_URL + '/login', { waitUntil: 'domcontentloaded' });
  await shot(page, '01-login');

  await page.fill('input[name=identifier]', USERNAME);
  await page.fill('input[name=password]', PASSWORD);
  await Promise.all([
    page.waitForURL(u => new URL(u).pathname === '/', { timeout: 10_000 }),
    page.click('button[type=submit]'),
  ]);
  log('login ok, landed on', page.url());
  await shot(page, '02-authed-dashboard');

  // Verify we see "Log out" on the authed page (cheap sanity).
  await page.waitForSelector('form[action="/logout"]', { timeout: 5_000 });

  // 2. Open gateway in the same tab (target=_blank would spawn a new
  // page — we follow the redirect manually so we can track it).
  log('goto', DASHBOARD_URL + '/gateway-launch');
  await page.goto(DASHBOARD_URL + '/gateway-launch', { waitUntil: 'domcontentloaded' });
  log('landed on', page.url());
  await shot(page, '03-gateway-first-load');

  // 3. Wait for the Control UI to either show the chat pane or an error.
  // The upstream markup for the connect card is the form with "Connect"
  // button; the "pairing required" state is a small inline error card.
  const deadline = Date.now() + 45_000;
  let finalState = 'unknown';

  for (let attempt = 1; Date.now() < deadline; attempt++) {
    // 3a. Is there a "pairing required" / origin error visible?
    const errText = await page.locator('text=pairing required').first().textContent().catch(() => null);
    const originErr = await page.locator('text=origin not allowed').first().textContent().catch(() => null);
    if (originErr) {
      hardFail = 'origin not allowed — gateway.controlUi.allowedOrigins missing public hostname';
      break;
    }

    if (errText) {
      log(`attempt ${attempt}: "${errText.trim()}" — waiting 3s for auto-approver, then clicking Connect again`);
      await page.waitForTimeout(3000);
      const connectBtn = page.getByRole('button', { name: /connect/i });
      if (await connectBtn.count() > 0) {
        await connectBtn.first().click({ trial: false });
      }
      await page.waitForTimeout(1500);
      await shot(page, `04-retry-${attempt}`);
      continue;
    }

    // 3b. Has the chat UI actually taken over? Look for a chat input /
    // textarea / send button that only exists post-connection.
    const chatReady =
      (await page.locator('textarea, [role="textbox"]').count()) > 0 &&
      (await page.locator('text=WebSocket URL').count()) === 0;
    if (chatReady) {
      finalState = 'connected';
      break;
    }

    // 3c. Still on the Connect card — click Connect.
    const connectBtn = page.getByRole('button', { name: /connect/i });
    if (await connectBtn.count() > 0) {
      log(`attempt ${attempt}: clicking Connect`);
      await connectBtn.first().click();
      await page.waitForTimeout(2000);
    } else {
      // Neither chat nor connect button — unknown state; wait and retry.
      await page.waitForTimeout(1000);
    }
  }

  await shot(page, '05-final');

  if (hardFail) {
    console.error('HARD FAIL:', hardFail);
    process.exitCode = 1;
  } else if (finalState !== 'connected') {
    console.error('FAIL: gateway chat did not become interactive within 45s');
    process.exitCode = 1;
  } else {
    console.log('OK: gateway chat is interactive');
  }
} catch (err) {
  console.error('UNEXPECTED:', err);
  await shot(page, 'crash');
  process.exitCode = 1;
} finally {
  await context.close();
  await browser.close();
}
