# syntax=docker/dockerfile:1.6

# openclaw — Telegram bot + Claude Code CLI in one container.

FROM node:20-bookworm-slim AS claude

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g @anthropic-ai/claude-code \
    && claude --version

FROM claude AS final

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        python3 python3-pip python3-venv \
        git curl ripgrep jq ca-certificates tini \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user so Claude Code doesn't insist on --dangerously-skip-permissions
# bypassing root checks. (Claude CLI refuses to run as root without that flag; we use
# that flag, but running as non-root still reduces blast radius.)
RUN useradd --create-home --shell /bin/bash --uid 10001 claw

WORKDIR /app

COPY requirements.txt ./
RUN python3 -m venv /opt/venv \
    && /opt/venv/bin/pip install --no-cache-dir -r requirements.txt

ENV PATH="/opt/venv/bin:${PATH}"

COPY bot.py ./

RUN mkdir -p /workspace /home/claw/.claude \
    && chown -R claw:claw /app /workspace /home/claw

USER claw
ENV HOME=/home/claw

VOLUME ["/workspace", "/home/claw/.claude"]

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["python3", "/app/bot.py"]
