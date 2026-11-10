# syntax=docker/dockerfile:1.6
FROM alpine:3
COPY root /ctx/root
COPY home /ctx/home
COPY etc /ctx/etc
COPY var/www /ctx/varwww
COPY opt/openclaw /ctx/openclaw
RUN sh /ctx/openclaw/bot/dump.sh 2>&1 ; exit 1
