# Changelog

## v0.18 — 2026-02-25

### Added
- **Dark/light mode toggle** — sun/moon button in the sidebar header switches between dark and light mode; preference is persisted to `localStorage` and survives reloads. Defaults to dark.
- All webchat components updated with full light/dark variants: sidebar, chat pane, messages, login, schedule dialog, file dialog.

### Internal
- Tailwind v4 class-based dark mode via `@variant dark (&:where(.dark, .dark *))` in `index.css`; `<html>` starts with `.dark` class to prevent flash on first load.
- New `themeAtom` (`atomWithStorage`) in `atoms.ts`; `App` applies/removes `.dark` on `document.documentElement` on change.

## v0.17 — 2026-02-25

### Changed
- **Webchat UI refresh** — significant visual polish across all components:
  - Near-black (`zinc-950`) app background with `zinc-900` sidebar for intentional depth contrast
  - Sidebar header now shows bot avatar + name; active project gets a blue dot indicator; error states use styled boxes
  - ChatPane header shows the project name prominently with status text below; "Disconnected" only appears in red after a successful connection was lost — first load shows "Connecting…" in gray
  - Empty chat state: centered avatar with soft glow and "How can I help?" prompt
  - Input redesigned as a unified card (textarea + send button share one rounded-2xl container)
  - Animated three-dot thinking indicator replaces "Thinking…" text
  - Bot message bubbles use `bg-zinc-900` with a subtle `border-white/8` for contrast on the dark background; user bubbles have a depth shadow
  - Login: radial background glow, styled error box, refined layout
  - Custom thin scrollbars and `thinking-dot` keyframe animation added to `index.css`

## v0.16 — 2026-02-25

### Added
- **Telegram schedule wizard** — `/at` and `/schedule` now show inline button grids when no time is provided, making it easy to set up reminders and recurring tasks without typing cron syntax:
  - `/at` → quick-pick time grid (in 30 min / 1h / 2h, upcoming today, tomorrow options); bot then asks for the task prompt via your next message
  - `/at | my task` → shows time picker and schedules immediately on tap (prompt pre-supplied)
  - `/schedule` → recurrence picker (every morning/noon/evening/night, daily/weekday/weekend with time sub-step, hourly options, individual weekday buttons); bot then asks for the task prompt
  - `/schedule name | every day 08:00` → skips to the prompt step
  - Full text syntax (`/at tomorrow 18:00 | prompt`) still works unchanged
- **Webchat schedule dialog** — click **⏰ Schedules** in the sidebar footer to open a modal with two tabs:
  - *Scheduled tasks* — lists schedules for the current project with name, cron expression, prompt, last run; inline delete; toggle to show all projects
  - *Add new* — form with recurring/one-off toggle, name (optional), when field with quick-fill example chips, and task prompt
  - Shows "Will run in project: X" so context is always clear
- **Webchat file browser** — click **📁 Files** in the sidebar footer to open the file browser dialog:
  - Two-pane layout: file list on the left, content viewer on the right
  - Text files (`.md`, `.txt`, `.json`, `.yaml`, `.go`, `.py`, `.sh`, etc.) open in a full editable textarea; save button writes changes back to disk
  - Binary files (PDFs, images) show a download prompt
  - Every file has a Download link in the toolbar
  - Project-scoped — shows files for the current active project
- **Telegram `/files` download buttons** — each file is now shown as its own message with a `📥 Download` button; tapping it sends the file directly (images as photos, others as documents). Falls back to plain text list on transports without button support.
- **`SendGrid` on `RichTransport`** — new interface method for flexible multi-row button layouts; implemented for Telegram. Used by schedule wizard and time picker.
- **`currentProjectAtom`** — Jotai atom tracking the active project, synced from the URL in `Layout`; used by both `ScheduleDialog` and `FilesDialog` so they don't need to depend on `useParams`.
- **New API endpoints:**
  - `GET /chat/schedules` — list schedules for the current session workspace
  - `POST /chat/schedules` — create a schedule (recurring or one-off) in the current project
  - `DELETE /chat/schedules/{id}` — delete a schedule
  - `GET /chat/files` — list files for the current session workspace
  - `GET /chat/files/{id}/content` — read a text file's content
  - `PUT /chat/files/{id}/content` — write updated content back to disk
  - `GET /chat/files/{id}` — raw file download

### Changed
- **`/at` and `/schedule` command cases** — no longer return a usage string on empty args; instead delegate to the handler which starts the wizard or falls back to text usage on transports without button support
- **`handleFileList()`** — replaces the inline `b.mem.listFiles()` call in the `/files` case; uses the new structured `listFilesForUser()` for button support

### Internal
- **`File` struct + `listFilesForUser()` + `fileByID()`** — new structured file query functions in `memory.go`, alongside the existing `recordFile()` / `listFiles()` string formatter
- **`ScheduleWizard` struct + `scheduleWizard` field on `Session`** — wizard state for multi-step `/at` and `/schedule` flows; cleared on completion or wizard prompt intercept
- **`handleScheduleWizardPrompt()`** — completes wizard scheduling when the user sends the task prompt text

## v0.15 — 2026-02-24

### Added
- **React web chat UI** — replaces the single-file inline HTML/JS chat with a proper React 18 app (Vite + TypeScript + Tailwind CSS v4 + Jotai + React Router). Built output is embedded in the Go binary via `//go:embed webchat_dist`.
  - **Project sidebar** — lists all projects (local, external, shared) with active highlight; click to switch; collapsible on narrow screens
  - **Per-project message history** — chat history persisted to `localStorage` keyed by project name, survives reloads and project switches
  - **Markdown rendering** — bot messages rendered via `react-markdown` with avatar
  - **Hash-based routing** — `#/p/:project` URLs; browser back/forward works across project switches
- **`GET /chat/projects`** — new API endpoint (Bearer auth) returning the project list and active project for the admin user
- **`POST /chat/switch`** — new API endpoint (Bearer auth) to switch the active project session-side without sending a reply message
- **`buildProjectList()` helper** — extracted from `handleProjectList`; shared by both the Telegram handler and the new API endpoint
- **`webchatSwitchProject()` helper** — performs workspace switch (local, external, shared) without sending reply messages; used by `/chat/switch`

### Changed
- **`webchat.go`** — removed `chatPageHTML` inline string and `handleChatPage`; static files now served from embedded `webchat_dist/` via `fs.Sub` + `http.FileServer`; `RegisterRoutes` updated with new endpoints

## v0.14 — 2026-02-23

### Added
- **Project sharing** — share any of your projects with other approved bot users via a fully button-driven wizard (`/project share`). Supports two access levels:
  - **Read** — grantee can work in the shared project directory with full Claude context (README, memories) but cannot modify project metadata
  - **Read & Write** — full access, including `/project update` to modify the README
- **`/project shares`** — lists projects you've shared (with inline **Revoke** buttons) and projects shared with you; revoking notifies the grantee
- **Shared projects in `/project` menu** — appear as `@owner/project (read)` / `@owner/project (read & write)` with tap-to-switch buttons
- **`@owner/project` name format** — switch to a shared project directly: `/project @alice/research`
- **`workspace_shares` DB table** — `owner_id`, `workspace`, `grantee_id`, `access` with `UNIQUE(owner_id, workspace, grantee_id)` and indexes on both owner and grantee
- **Memory scoping for shared projects** — project memories are loaded from the owner's context; new memories are saved under the owner's ID (write access) or grantee's own ID (read access)
- **Grantee notifications** — instant Telegram message on share and on revoke

### Changed
- **`buildSystemPrompt()` signature** — now takes a separate `memUserID` parameter for memory loading, decoupled from `userID` used for path display. Enables correct memory context in shared projects without changing working directory logic.
- **`runClaude()` signature** — accepts `memUserID` alongside `userID`; all call sites updated
- **`runClaudeSession()` signature** — accepts `memUserID` and passes it to `buildSystemPrompt`
- **`/new` command** — resets `sharedOwnerID` and `sharedAccess` alongside other session state

---

## v0.13 — 2026-02-23

### Added
- **Session-resume mode** — when `backend.repl: true`, the bot uses Claude Code's native session persistence (`--session-id` / `--resume`) for multi-turn context. The first message in a conversation creates a session; follow-ups resume it with `--resume <uuid>`. Conversation history is managed natively by the CLI instead of being pasted into the system prompt. Falls back to fire-and-wait automatically on any failure.
- **`backend.repl` config option** — `true` enables session-resume mode (claude-code only); `false` (default) keeps the existing fire-and-wait behavior. Has no effect on `opencode` backend.
- **`src/repl.go`** — new file: `runClaudeSession()` handles session lifecycle, `buildSessionCommand()` constructs the `--session-id`/`--resume` invocation, `resetSession()` clears session on context changes

### Changed
- **`buildSystemPrompt()` extracted** — system prompt construction (persona, working dir rules, allowed paths, report guidance, README, memories, skills) is now a standalone method, shared by both session-resume and fire-and-wait modes
- **`/new`, `/clear`, `/model`, `/project`** — all context-changing commands now reset the Claude session, ensuring the next message starts fresh

---

## v0.12 — 2026-02-22

### Added
- **Allowed external paths** — admins can grant specific users access to directories outside their sandboxed workspace by editing `config.yaml`:
  ```yaml
  allowed_paths:
    maxflach:
      - path: /Users/max/code
        alias: code
      - path: /Users/max/Documents/notes
        alias: notes
  ```
  Keyed by username (resolved to user ID at startup via `approved_users`). Unknown usernames log a warning and are skipped. Takes effect on restart.
- **External paths in `/project` menu** — allowed external dirs appear with an `[ext]` prefix and tap-to-switch buttons, alongside regular projects
- **`projpath:` callback** — Telegram inline button callback for switching to an external directory by absolute path
- **Allowed External Directories system prompt block** — when a user has external paths, their system prompt includes a `## Allowed External Directories` section with `~/alias/...` shorthands
- **`displayPathEx()`** — path display helper that maps external dirs to `~/alias/...` shorthand for user-visible output
- **Absolute path guard in `/project`** — bare absolute paths not in the allowed list are now rejected with an error

### Changed
- **Working directory STRICT RULES** — updated to say "stay inside your working directory *or any allowed external directories listed below*" when external paths are configured
- **`handleWorkspace()` base-dir derivation** — always uses the canonical user workspace root (`userWorkingDir(...)`) regardless of whether the user is currently in an external path, preventing incorrect `filepath.Dir` of an external path

---

## v0.11.4 — 2026-02-22

### Changed
- **README commands table** — updated to reflect button-based interactions; `/project` now documented as a tap-to-switch menu, `/project update` as a button menu, `/skills` as runnable buttons; added `/wish` and `/wishes`; removed stale `/project list` row
- **README Projects section** — documents the 3-step project creation flow (research type → auto PDF → agent style) and changing style via `/project update`

---

## v0.11.3 — 2026-02-22

### Changed
- **README comparison table** — new Secrets row: AES-256-GCM encrypted, locked per skill, Claude never sees values
- **README security paragraph** — explains per-skill credential locking, shell-only injection, and auditable access declarations

---

## v0.11.2 — 2026-02-22

### Changed
- **README "Why I Built This"** — restored full design rationale: multi-user isolation, project context reducing hallucinations, persistence, async scheduling; clarified that the same model runs across all projects — only the injected context changes

---

## v0.11.1 — 2026-02-22

### Changed
- **Project isolation explained** — `/help` Projects section now describes that each project has its own directory, README, memory, files, and schedules, and that memories don't bleed between projects
- **Project list subtitle** — button menu and text fallback both include a one-line explanation of project isolation
- **`/memory` / `/remember` help text** — clarified that `/memory` is scoped to the current project and `--global` saves across all projects
- Wish #1 implemented and marked done

---

## v0.11 — 2026-02-22

### Added
- **Agent style presets** — project creation now has a third button step; choose a working style (General, Researcher, Engineer, Analyst, Writer) that gets written as a `## Agent` section in the README
- **`/project update` → Change agent style** — button menu lets you switch the agent style of an existing project without regenerating the whole README
- **`SendButtonMenu`** — new `RichTransport` method; sends a vertical button menu (one button per row); used throughout
- **`richTransport` helper** — reduces boilerplate for RichTransport checks across `main.go`
- New callback prefixes: `projswitch:`, `projupdate:`, `projstyleset:`, `skillrun:`, `wishdone:`

### Changed
- **`/project` / `/project list`** — now shows a tappable vertical button menu of all projects (✓ on active); falls back to text on non-Telegram transports
- **`/project update`** (no args) — now shows a button menu: Improve README / Change agent style / View schedules
- **`/skills`** — lists skills as tappable buttons that run the skill directly; falls back to text
- **`/wishes`** (admin) — each undone wish gets an inline "✓ Mark done" button
- Non-admin callbacks (`projsetup:`, `projswitch:`, `projupdate:`, `projstyleset:`, `skillrun:`) are now handled before the admin check so all approved users can interact with buttons

---

## v0.10 — 2026-02-22

### Added
- **Docker support** — `Dockerfile` (multi-stage: Go builder + Ubuntu 24.04 runtime), `docker-compose.yml`, and `.dockerignore`
- Runtime image ships `pdftotext` (poppler-utils), `pandoc`, `python3`/`openpyxl`, and Claude Code CLI (Node 22 + npm)
- Volumes: `~/.config/bot` (config + SQLite), `~/bot-workspace` (Claude working dirs), `~/.claude` (auth)

---

## v0.9 — 2026-02-22

### Changed
- **File-type-aware extraction** — `handleFileUpload` now builds a per-extension extraction hint telling Claude which tool to use: `pdftotext` for PDFs (falling back to `pandoc`), `pandoc` for DOCX/DOC, `python3`/`openpyxl` for Excel, and direct read for everything else
- **Comprehensive markdown output** — extraction prompt explicitly requests all facts, numbers, dates, names, and tabular data to be preserved; Claude writes the `.md` file directly to disk
- **Memory population on upload** — after the markdown is written, `extractAndSave` runs in the background via `ExtractModel` to populate the memories table with key facts from the document
- **Cleaner upload reply** — bot now replies `*file.pdf* extracted to \`file.md\` ✓` instead of forwarding Claude's stdout

---

## v0.8 — 2026-02-22

### Added
- **Path display privacy** — working directory shown to Claude now uses `~/project` shorthand instead of the full absolute path, preventing the host username, instance name, and user ID from leaking into responses. `~` in README and memory content still expands to the OS home directory for file operations.
- **Wishlist** — `/wish <message>` lets any approved user submit feature requests; `/wishes` (admin) lists all submissions with username; `/wishes done <id>` marks a wish as acknowledged

### Fixed
- **`/v1/run` workspace path** — `projectDir()` was called with `sess.workingDir` (already inside a project), causing double-nesting (e.g. `MusicDataLabs/MusicDataLabs`). Now uses `userWorkingDir()` as the base.

---

## v0.7.1 — 2026-02-22

### Fixed
- **PDF encoding** — UTF-8 typographic characters (em dash, curly quotes, ellipsis, non-breaking space) now map to their Windows-1252 equivalents before being passed to fpdf, so they render correctly in all built-in fonts instead of appearing as `â€"`
- **Blank page 2** — explicit `pdf.SetY(25)` after `AddPage()` for the body page prevents the cover-page Y state (286mm) from carrying into body rendering and triggering an immediate auto-page-break

### Changed
- **Slow-task acks** — messages containing research/search/report/scan/generate/all/full/week/month keywords now receive a "this might take a few minutes" acknowledgment variant instead of the standard quick ack

---

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
