# ── Stage 1: Build ─────────────────────────────────────────────────────────────
FROM golang:alpine AS builder
WORKDIR /build
COPY src/ .
RUN go build -o /bot .

# ── Stage 2: Runtime ───────────────────────────────────────────────────────────
FROM ubuntu:24.04

# System tools for file extraction (pdftotext, pandoc, python3/openpyxl)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    poppler-utils \
    pandoc \
    python3 \
    python3-openpyxl \
    && rm -rf /var/lib/apt/lists/*

# Node.js 22 (Claude Code requires ≥ 20)
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# Claude Code CLI
RUN npm install -g @anthropic-ai/claude-code

COPY --from=builder /bot /usr/local/bin/bot

ENV HOME=/root

ENTRYPOINT ["bot"]
