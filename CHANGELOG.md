# Changelog

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
