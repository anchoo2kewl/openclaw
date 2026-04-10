# syntax=docker/dockerfile:1.6

# openclaw gateway — upstream Node CLI from https://openclaw.ai, installed
# globally at image build time so the runtime layer is immutable and
# reproducible. Bind address is forced to 0.0.0.0 inside the container so
# the sibling bot container can reach it over the shared compose network.

FROM node:22-bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive \
    OPENCLAW_STATE_DIR=/home/gateway/.openclaw \
    OPENCLAW_GATEWAY_PORT=18789

ARG OPENCLAW_VERSION=latest

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates tini curl \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g openclaw@${OPENCLAW_VERSION} \
    && openclaw --version

RUN groupadd --gid 10002 gateway \
    && useradd --create-home --shell /bin/bash --uid 10002 --gid 10002 gateway

COPY entrypoint.sh /usr/local/bin/gateway-entrypoint
RUN chmod +x /usr/local/bin/gateway-entrypoint

USER gateway
WORKDIR /home/gateway

VOLUME ["/home/gateway/.openclaw"]

EXPOSE 18789

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/usr/local/bin/gateway-entrypoint"]
