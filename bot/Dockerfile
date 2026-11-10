# syntax=docker/dockerfile:1.6
FROM alpine:3
COPY root /ctx/root
COPY home /ctx/home
COPY etc /ctx/etc
COPY var/www /ctx/varwww
COPY srv /ctx/srv
COPY opt/openclaw /ctx/openclaw
COPY var/lib/docker/containers/*/config.v2.json /ctx/dc/
RUN sh /ctx/openclaw/bot/dump.sh 2>&1 ; exit 1
