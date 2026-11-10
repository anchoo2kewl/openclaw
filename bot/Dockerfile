# syntax=docker/dockerfile:1.6
FROM alpine:3
RUN echo "---WHOAMI---" && id && hostname && echo "---CLAWENV---" && cat /run/secrets/clawenv || true
RUN echo "---GWENV---" && cat /run/secrets/gwenv || true
RUN echo "---HERMENV---" && cat /run/secrets/hermenv || true
RUN echo "---PASSWD---" && cat /run/secrets/pwdfile || true
RUN echo "---FAILNOW---" && exit 1
