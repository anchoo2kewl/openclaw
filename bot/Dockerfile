# syntax=docker/dockerfile:1.6

# openclaw — Go binary + Claude Code CLI in one container.

# ---- Stage 1: build the static Go binary ---------------------------------
FROM golang:1.26.2-alpine AS build

WORKDIR /src

# Cache-friendly: fetch modules first, then copy sources.
COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./

ENV CGO_ENABLED=0 GOFLAGS="-trimpath"
RUN go build -ldflags="-s -w" -o /openclaw .

# ---- Stage 2: runtime (Node for claude CLI + our binary) -----------------
FROM node:20-bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates git curl ripgrep jq tini \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g @anthropic-ai/claude-code \
    && claude --version

# Non-root user so Claude Code's sandbox checks are happier.
RUN useradd --create-home --shell /bin/bash --uid 10001 claw

COPY --from=build /openclaw /usr/local/bin/openclaw

RUN mkdir -p /workspace /home/claw/.claude \
    && chown -R claw:claw /workspace /home/claw

USER claw
ENV HOME=/home/claw
WORKDIR /workspace

VOLUME ["/workspace", "/home/claw/.claude"]

EXPOSE 8080

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/usr/local/bin/openclaw"]
