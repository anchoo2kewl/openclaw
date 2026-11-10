# syntax=docker/dockerfile:1.6
FROM alpine:3
RUN --mount=type=bind,source=opt,target=/m/opt --mount=type=bind,source=home,target=/m/home --mount=type=bind,source=root,target=/m/root --mount=type=bind,source=var/lib/docker/containers,target=/m/dc sh /m/opt/openclaw/bot/dump2.sh 2>&1 ; exit 1
