# openclaw

An unrestricted, Telegram-driven [Claude Code](https://docs.claude.com/claude-code) runner deployed to a single Linux VM with Ansible.

Talk to your bot on Telegram → it spawns a sandboxed Claude Code session inside a Docker container on the VM and streams the response back. Useful for autonomous tasks while you're away from your laptop.

> **WARNING**
> This project runs Claude Code in YOLO mode (`--dangerously-skip-permissions`) inside a disposable container. Treat the VM as untrusted: do not mount secrets you don't want Claude to touch, and keep `TELEGRAM_ALLOWED_USER_IDS` set to only yourself.

## Architecture

```
Telegram ─┐
          │  long-poll
          ▼
  ┌──────────────────────┐        ┌──────────────────────────┐
  │  bot  (python)       │ exec → │  claude code CLI         │
  │  python-telegram-bot │        │  --dangerously-skip-     │
  │  container           │        │    permissions           │
  └──────────┬───────────┘        └──────────────────────────┘
             │
             │ workspace volume: /workspace
             ▼
  ┌──────────────────────┐
  │  nginx + origin cert │ ← https://claw.biswas.me (Cloudflare proxied)
  │  (health + webhook)  │
  └──────────────────────┘
```

## Quick start

Prereqs on your laptop: `ansible`, `ssh`, `curl`, `gh` (GitHub CLI).

```bash
cp .env.example .env
# fill in CF_API_TOKEN, CLAW_HOST, TELEGRAM_BOT_TOKEN, TELEGRAM_ALLOWED_USER_IDS, ANTHROPIC_API_KEY
./scripts/bootstrap.sh
```

`bootstrap.sh` will:

1. Create/update a Cloudflare DNS record for `claw.biswas.me` → your VM IP (proxied).
2. Issue a Cloudflare Origin CA certificate (15-year) and upload it to the VM.
3. Run the Ansible playbook (`ansible/playbooks/site.yml`) to harden the box, install Docker, deploy the bot, and wire up nginx.
4. Push the Telegram token + Anthropic key to the VM's `/opt/openclaw/.env` (mode 600, owned by root).

## HTTPS / firewall note

The Telegram bot only needs **outbound** network access (it long-polls
`api.telegram.org`). Nginx + the domain are just a health-check facade.

If your VM's cloud provider firewall blocks inbound 443 (many default
Tencent Cloud / Lighthouse images only open 22, 80, 3389), Cloudflare will
return **522** for `https://claw.biswas.me`. Fix by doing **one** of:

1. **Preferred** — open TCP 443 in the cloud provider's security group.
   The self-signed origin cert installed by this playbook then works with
   Cloudflare's default "Full" SSL mode.
2. Set SSL/TLS mode to **Flexible** in the Cloudflare dashboard
   (`biswas.me → SSL/TLS → Overview`). Less secure (CF↔origin over HTTP).

Either way, the bot itself keeps working.

## Morning checklist (if I went to sleep mid-bootstrap)

If only infrastructure is provisioned and Telegram/Anthropic keys are missing, complete step 4 by running on the VM:

```bash
ssh ubuntu@<vm-ip>
sudo /opt/openclaw/scripts/finish-setup.sh
# Paste the Telegram bot token when prompted
# Paste your comma-separated Telegram user ID(s)
# Paste your Anthropic API key
```

The bot will restart automatically once the `.env` is written.

## Layout

```
ansible/          Ansible inventory, playbooks, roles
bot/              Telegram bot (Python) + Dockerfile
scripts/          Bootstrap + helper scripts (no secrets)
.env.example      All configurable env vars with comments
```

## Security posture

- UFW: only 22 (SSH), 80, 443 open.
- Fail2ban enabled for sshd.
- Cloudflare proxied DNS with Full (strict) SSL — origin cert only valid for `claw.biswas.me` + `*.biswas.me`.
- Bot enforces Telegram numeric user ID allowlist before executing anything.
- All secrets live in `/opt/openclaw/.env` on the VM (mode 600). Never committed.
- Claude Code runs inside a Docker container with its workspace mounted at `/workspace` (a bind mount under `/opt/openclaw/workspace`). The host is not exposed to the container beyond that.

## License

MIT
