# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Artoo Bot** is a Telegram-based personal assistant that delegates execution to an agentic CLI (Claude Code or OpenCode). It is a thin orchestration layer providing multi-user isolation, memory persistence, scheduling, and project context management — all in Go.

## Build & Run

All Go source lives in `src/`. Build from there:

```bash
cd src && go build -o ../bot .
```

Run the bot:
```bash
./bot                        # default instance
./bot --instance <name>      # named instance
./bot --setup                # interactive setup wizard
```

Install as a background service (macOS LaunchAgent or Linux systemd):
```bash
bash install.sh [instance-name]
```

## Architecture

The app is a single Go binary with no subpackages. All source is in `src/`:

| File | Role |
|------|------|
| `main.go` | Bot struct, session management, Telegram message routing, command handlers, Claude CLI invocation |
| `config.go` | Config struct (YAML), setup wizard, auto-detection of claude/opencode binary |
| `api.go` | REST API server (`/v1/health`, `/v1/send`, `/v1/run`) with Bearer token auth |
| `memory.go` | SQLite store — memories, workspaces, files, schedules, users, api_keys tables |
| `cron.go` | Cron runner wrapping robfig/cron; loads schedules from DB on start, supports one-shot jobs |
| `timeat.go` | Natural language → cron expression parser ("tomorrow 18:00", "every weekday 09:00") |
| `pdf.go` | Styled PDF renderer: markdown → A4 PDF via go-pdf/fpdf. Used bot-wide across all projects. |

## Key Concepts

**Session state** (`Session` struct in `main.go`): per-user mutable state stored in a `map[int64]*Session`. Holds conversation history (capped at 20 messages), current project/workspace, working directory, and an optional session-level model override.

**Message flow**: Plain Telegram messages fire `go b.runUserMessage()` immediately, returning "_On it..._" to the user. The function assembles a system prompt (persona + working dir rules + project README + memories + history), invokes the Claude CLI as a subprocess, captures stdout, detects new files via pre/post filesystem snapshots, and sends the result back via Telegram.

**Backend invocation**: Claude Code is called with `--dangerously-skip-permissions`. OpenCode uses `opencode run`. Both are configured in `config.yaml` and auto-detected at setup.

**Multi-user isolation**: Each user gets `~/bot-workspace/<instance>/<userID>/`. Memory, files, schedules, and projects are partitioned by `user_id` in SQLite.

**Memory extraction**: After each conversation turn, a background goroutine calls the configured `extract_model` (typically Haiku) to auto-extract facts into the `memories` table.

## Configuration & Data

- Config: `~/.config/bot/<instance>/config.yaml` (mode 0600)
- Database: `~/.config/bot/<instance>/memory/bot.db` (SQLite)
- Workspace: `~/bot-workspace/<instance>/`

## Dependencies

- `github.com/go-telegram-bot-api/telegram-bot-api/v5` — Telegram client
- `github.com/robfig/cron/v3` — cron scheduler
- `modernc.org/sqlite` — pure-Go SQLite (no C compiler needed)
- `gopkg.in/yaml.v3` — config parsing
- `github.com/google/uuid` — API key generation

Go version: 1.26.0. No test files exist in the codebase.

## API Documentation

The HTTP API is documented in `src/openapi.yaml` (embedded into the binary at build time).

**Rule: If you modify API endpoints in `api.go`, update `src/openapi.yaml` to match.**

The spec is served at `/openapi.yaml` and browsable via Swagger UI at `/docs`.

## PDF Report System

`pdf.go` provides `RenderMarkdownReport(mdPath, outPath, tmpl)` — used bot-wide, not per-project.

**Auto-render trigger**: any time a Claude Code session writes a file named exactly `report.md` to its working directory, the bot automatically renders it to PDF and sends it via Telegram. This is how all project reports work.

**Encoding rule**: fpdf's built-in fonts (Helvetica, Courier) use single-byte Windows-1252 / Latin-1 encoding. All text passed to fpdf MUST go through `pdfText()`, which converts UTF-8 multi-byte characters to their Latin-1 byte equivalents. Without this, characters like ö/ä/å/Ö/Ä/Å render as garbage (e.g. ö → Ã¶). When adding new text rendering calls to `pdf.go`, always wrap strings with `pdfText()`.

**Template**: visual styling (colors, header/footer text, logo) is controlled by `template.yaml`. Look-up order: `projectDir/template.yaml` → `userBaseDir/template.yaml` → `~/.config/bot/report-template/template.yaml` → hardcoded default (dark theme).

**Cover page**: the first H1 in the markdown becomes the cover title. All subsequent content is rendered as the body starting on page 2.

**Do not** introduce external PDF tools (pandoc, wkhtmltopdf, weasyprint) for report generation inside bot-managed workspaces — the bot handles it natively via `pdf.go`.

## Notes

- Do not use `push-status` in this project. It is a personal Claude Code skill for long coding sessions and is not relevant here.
