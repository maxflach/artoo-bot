# Changelog

## v0.7 — 2026-02-22

### Added
- **Artoo Reports — PDF template system** — Claude writes `report.md`; the bot auto-detects it and renders a styled PDF using a pure-Go renderer (no external tools required, works everywhere)
- **`src/pdf.go`** — new file: `ReportTemplate` struct hierarchy, `loadReportTemplate()`, `RenderMarkdownReport()`; goldmark parses markdown AST; fpdf renders cover page + body pages
- **Cover page** — full-page branded background, accent bar, title from H1, date, optional logo, brand name; no header/footer on cover
- **Body rendering** — H2 section headers with rule lines, H3 sub-headers, inline bold/italic/code spans, bullet and numbered lists with accent-coloured markers, fenced code blocks, blockquotes, thematic breaks
- **Template priority chain** — templates resolve from most to least specific: `<projectDir>/template.yaml` → `<userBaseDir>/template.yaml` → `~/.config/bot/report-template/template.yaml` → hardcoded defaults
- **Template upload via Telegram** — upload `template.yaml` or `logo.png` to a project and the bot saves it directly (no Claude extraction); confirms "Report template updated for project X ✓"
- **Template provisioning** — new users receive a copy of the global template in their base dir when approved; `install.sh` creates `~/.config/bot/report-template/template.yaml` on first install
- **`/report` command** — manually render `report.md` in the current project to PDF and send it; `/report reload` confirms templates load fresh per-render
- **Report instructions in system prompt** — Claude is told to write `report.md` (not call PDF tools) when producing digests or summaries; format instructions included
- **`report.md` intercepted in all file-send loops** — `runUserMessage`, `runSkill`, and `runScheduledTask` all auto-render and send the PDF, skipping the raw `.md`
- **Interactive project setup** — `/project <name> | <description>` now asks two button questions on Telegram before generating the README: "Does this project involve web research?" and "Auto-generate PDF reports after each run?"; README is tailored to the answers; non-Telegram transports fall back to immediate generation
- **`buildReadmePrompt()`** — generates context-appropriate README prompts based on project type (research vs general) and auto-report preference

### Changed
- `generateWorkspaceReadme()` accepts a `*ProjectOptions` parameter; callers that don't need options pass `nil` for backward-compatible behaviour
- MusicDataLabs README updated: Step 5 now writes `report.md` in the standard digest format instead of a hand-crafted PDF

### New dependencies
- `github.com/go-pdf/fpdf v0.9.0` — pure-Go PDF generation
- `github.com/yuin/goldmark v1.7.16` — CommonMark-compliant markdown parser

---

## v0.6 — 2026-02-22

### Added
- **Multi-transport architecture** — the bot is no longer Telegram-only; a `Transport` interface decouples all messaging backends from core logic
- **Discord transport** — connect via a Discord bot token; configure `discord.token` and `discord.allowed_user_ids` in config; DM and guild messages supported
- **Web chat transport** — browser-based chat UI served at `/chat` (requires API port enabled); SSE event stream at `/chat/sse`; dark-mode UI with keyboard shortcut to send; auth via existing API key mechanism
- **`RichTransport` extension** — optional interface for transports that support interactive buttons; Telegram implements it (inline keyboards for schedule remove); other transports fall back to plain-text with `/unschedule <id>` hint
- **Multi-admin support** — `isAdmin()` checks all configured transport admin IDs; Discord admin can manage API keys and secrets just like the Telegram admin

### Changed
- `Session.chatID` changed from `int64` to `string` — transport-prefixed format (`tg:123`, `dc:456`, `wc:abc`)
- `Schedule.ChatID` likewise changed to `string`; DB migration adds `chat_id_str` column and back-fills existing rows as `tg:<old_id>` on startup (backward compatible)
- All Telegram-specific code extracted from `main.go` into `telegram.go`
- `bot.reply()` and `bot.sendFile()` now route via transport prefix instead of calling the Telegram API directly
- `bot.run()` starts all configured transports concurrently

### New files
- `src/transport.go` — `Transport` / `RichTransport` interfaces, `IncomingMessage`, `Button`, `makeChatID` / `splitChatID`
- `src/telegram.go` — `TelegramTransport` (extracted from `main.go`)
- `src/discord.go` — `DiscordTransport` via `github.com/bwmarrin/discordgo`
- `src/webchat.go` — `WebChatTransport` (SSE + HTTP POST)

---

## v0.5 — 2026-02-22

### Added
- **System secrets** — admin can provision API keys shared across all users with `/secret set --system <name> <value> --skill <skill>`; stored as `user_id=0` in the database
- **Full secret priority chain** — system (lowest) → user global → user project (highest); more specific values always win
- **`/secret del --system`** — admin can remove system secrets
- **`/secret list`** shows system secrets to the admin alongside their own

### Changed
- Secret merge query updated to include `user_id=0` rows in a single pass with correct priority ordering

---

## v0.4 — 2026-02-22

### Added
- **Secrets vault** — store named credentials encrypted at rest in SQLite; secrets are injected as `ARTOO_SECRET_*` env vars when skills run, never passed to Claude
- **AES-256-GCM encryption** — each secret value encrypted with a random nonce; 32-byte master key generated once and stored at `~/.config/bot/<instance>/secrets.key` (mode 0600); key is never in the database
- **Skill-locked secrets** — `--skill <name>` required on every `/secret set`; a secret can only be injected into the specific skill it was locked to — no other skill can access it, even in the same project
- **Secret scoping** — secrets are either project-scoped (default) or global (`--global`); global secrets are available across all your projects, project-scoped ones only in their project; project values override global ones for the same key name
- **`ARTOO_WD` env var** — skills receive their current working directory; files written there are auto-detected and sent back to the user
- **Auto file delivery for skills** — after a skill runs, any new files in the working directory are sent via Telegram; images (PNG/JPG) sent as photos, everything else as documents
- **`/secret` command** — full CRUD for secrets: `set`, `list`, `del`; values are never shown in responses, only key names
- **Demo skill: `imagine`** — generates images via Google Gemini Imagen API; saves PNG to working dir, bot sends it as a photo; requires `/secret set GEMINI_API_KEY <key> --skill imagine`; installed automatically by `install.sh`

### Changed
- `dispatchSkill()` now accepts an `extraEnv map[string]string` for secret injection into script skills
- `runSkill()` now snapshots the working directory before execution and sends any new files after

---

## v0.3 — 2026-02-22

### Added
- **Skills system** — drop `.md` prompt templates or executable files (or folders with `run.sh`) into a `skills/` directory to create custom `/commands`; no code changes needed
- **Skill locations** searched in order: global (`~/.config/bot/skills/`), per-instance (`~/.config/bot/<instance>/skills/`), per-project (`<project>/skills/`); later entries override earlier
- **Folder-based skills** — a skill folder can bundle data files alongside its entrypoint (e.g. `dadjoke/run.sh` + `dadjoke/jokes.json`)
- **`/skills`** — list all loaded skills with type and description
- **`/skills reload`** — reload skills from disk without restarting the bot (admin only)
- **MCP server** — skills exposed as MCP tools on the existing API server (`/mcp/sse`, `/mcp/message`); Claude can call them mid-task via JSON-RPC 2.0 over SSE
- **Auto-registration** — on startup, artoo writes its MCP server entry to `~/.claude.json` with a dedicated `artoo-mcp` API key (skips if already configured)
- **Skills in system prompt** — available skills listed in Claude's context so it's aware of them even without MCP
- **Demo skill: `dadjoke`** — folder-based script skill bundled with `jokes.json`; installed automatically by `install.sh`

---

## v0.2 — 2026-02-22

### Added
- **REST API** — HTTP server with Bearer token auth; trigger messages and run tasks from scripts or other services
- **API key management** — `/apikey new`, `/apikeys`, `/apikey revoke` (admin only)
- **User approval** — admin receives Approve/Deny buttons when an unknown user messages the bot
- **Session persistence** — active project survives bot restarts (stored in SQLite)
- **Project listing** — `/project list` shows all projects with README titles and active marker
- **Natural language scheduling** — `every day 08:00`, `every weekday`, `every monday`, `in 2h`, `tomorrow 18:00`, etc.
- **One-shot reminders** — `/at` command; schedules auto-delete after firing
- **Configurable backend** — swap between `claude-code`, `opencode`, or any agentic CLI via config
- **Interactive setup wizard** — `./artoo --setup` walks through all configuration in five steps
- **Multi-OS install script** — `install.sh` auto-detects macOS (LaunchAgent) and Linux (systemd)
- **One-liner installer** — `get.sh` downloads the correct binary for your platform from the latest release
- **Per-user isolation** — each user gets their own projects, memory, files, schedules, and working directory
- **Inline schedule management** — `/schedules` shows remove buttons per entry
- **Immediate ack messages** — bot replies instantly before the task completes, showing active project name
- **Source reorganised** — Go source moved into `src/` subfolder

### Changed
- Config section renamed from `claude` to `backend` to reflect support for multiple CLI tools
- `newMemoryStore` no longer takes a binary path; memory extraction uses a configurable runner
- Help text and replies use "project" consistently instead of "workspace"

---

## v0.1 — 2026-02-21

Initial release.

### Features
- Telegram bot backed by an agentic CLI running on your own machine
- Per-user sessions with conversation history
- SQLite-backed memory (auto-extracted after each conversation)
- Named projects with README-driven context
- File upload support — files saved to project and extracted as markdown
- Cron scheduling stored in SQLite (survives reboots)
- `/remember`, `/memory`, `/files`, `/model`, `/project`, `/schedule`, `/schedules`, `/at`
- LaunchAgent install for macOS
- Persona and system prompt fully configurable via YAML
