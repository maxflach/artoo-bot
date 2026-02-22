# Artoo Bot

A personal Telegram bot that gives you remote access to an agentic CLI running on your own machine. Send a message, get things done — research, file work, scheduled tasks, and more.

---

## Why I Built This

There are good tools in this space already. [OpenClaw](https://github.com/openclaw) and similar projects work well and are actively maintained. This is not an attempt to replace them or compete feature-for-feature.

I built this because I had a specific frustration: most Telegram AI bots call external APIs directly — they send your prompt to a model, get a response, and send it back. That works fine for conversation, but it means the AI is just generating text. It can't actually *do* things on your machine.

What I wanted was the opposite: let the tool handle the heavy lifting. Agentic CLIs are already built for this kind of work. They know how to search the web, read and write files, run code, and chain tasks together. The results feel noticeably better than calling the raw API because the tool has been optimised for exactly this kind of multi-step reasoning and execution.

The mental model I was going for is closer to Claude's own "computer use" or the spirit of claude.ai's Projects — but accessible via Telegram, running on my hardware, with my files available.

My main use case is **async research**. I fire off a task — "search for the latest news on this topic and send me a summary when you're done" — and let it run. The result arrives when it's done, not when a 30-second API timeout kicks in. That changes how you use it.

A few other things that mattered to me:

- **Multi-user, single instance.** Most self-hosted bots run one instance per user. I wanted one bot that handles multiple people, each fully isolated — their own projects, memory, files, working directories, and schedules. No separate deployments.
- **Projects with real context.** Each project has a README that the AI reads on every run. That README defines the project's purpose, focus, data schema, and step-by-step instructions. The AI doesn't need to re-learn what the project is — it's all there.
- **Persistence that survives restarts.** Sessions, active projects, and schedules are all stored in SQLite. Reboot the machine, the bot picks up exactly where it left off.
- **Async scheduling.** Natural language cron expressions. "Every weekday at 9am" or "in 2 hours" just work. One-off reminders clean themselves up automatically.

What's coming next: support for custom skills and a local MCP server to register and run them — so you can extend the bot's capabilities without touching the core code.

---

## How It Compares to OpenClaw

| | Artoo Bot | OpenClaw |
|---|---|---|
| Execution | Delegates to any agentic CLI on your machine | Runs its own agent loop |
| Web search | Delegated to the CLI tool (built-in to most) | Calls Brave/DuckDuckGo API directly |
| Multi-user | Yes — one instance, isolated per user | Typically one instance per user |
| Projects | Yes — README-driven, per-user, with their own dirs | Workspace support |
| Scheduling | Built-in (natural language cron) | Varies |
| Skills / plugins | Coming (local MCP server) | Built-in skill system |
| Setup | Single binary + interactive wizard | Interactive wizard |
| Philosophy | Delegate everything to the AI tool | Custom agent with its own tool layer |

The key philosophical difference: this bot is a thin shell around an existing agentic CLI. It handles auth, routing, memory, and scheduling — then hands the actual work off to whatever tool you configure. I use Claude Code personally, but any agentic CLI that accepts a prompt and returns output will work. OpenClaw builds its own tool layer. Neither is wrong; they're just different bets on where the intelligence should live.

---

## Prerequisites

### 1. A machine to run it on

The bot runs as a background service on a Linux or macOS machine with internet access. It needs to stay running to handle scheduled tasks.

### 2. An agentic CLI

The bot shells out to whichever CLI you configure. You need at least one installed and authenticated. Two good options:

**Claude Code:**
```bash
npm install -g @anthropic-ai/claude-code
claude  # follow login prompts
```

**OpenCode:**
```bash
# See https://github.com/sst/opencode for install instructions
```

Any CLI that accepts a prompt and returns text output can work — configure it under `backend` in the setup wizard.

### 3. A Telegram Bot

1. Open Telegram and message [@BotFather](https://t.me/botfather)
2. Send `/newbot`
3. Follow the prompts — choose a name and a `@username` (must end in `_bot`)
4. BotFather gives you a token like `1234567890:ABCdef...` — save it

**Find your Telegram user ID:**
Message [@userinfobot](https://t.me/userinfobot) — it replies with your numeric user ID.

### 4. Go 1.21+

```bash
# macOS
brew install go

# Linux
# See https://go.dev/dl/
```

---

## Installation

### Clone and build

```bash
git clone https://github.com/maxflach/artoo-bot
cd artoo-bot
go build -o bot .
```

### Run the setup wizard

```bash
./bot --setup
```

The wizard walks you through:

1. **Backend** — choose your agentic CLI; auto-detects the binary
2. **Telegram** — bot token and your user ID
3. **Persona** — bot name and system prompt
4. **Memory** — how long to retain memories (default: 90 days)

Config is saved to `~/.config/bot/default/config.yaml`. The file is excluded from git — it contains your bot token.

### Install as a background service

```bash
bash install.sh
```

Detects your OS and installs the appropriate service:

- **macOS** — creates a LaunchAgent that starts on login and restarts on crash
- **Linux** — creates a systemd user service with the same behaviour

For named instances:

```bash
bash install.sh workbot
```

### Multiple instances

You can run multiple isolated bots (different Telegram tokens, different personas) on the same machine:

```bash
./bot --setup --instance workbot
bash install.sh workbot
```

Each instance gets its own config (`~/.config/bot/workbot/`) and workspace (`~/bot-workspace/workbot/`).

---

## Usage

### Talking to the bot

Send any plain text message — it goes straight to your configured agentic CLI, running on your machine in the current project's directory.

### Commands

| Command | Description |
|---|---|
| `/project` | Show current project |
| `/project list` | List all your projects |
| `/project <name>` | Switch to (or create) a project |
| `/project <name> \| <description>` | Create a new project and generate a README |
| `/project update` | Improve the current project's README |
| `/project update <instruction>` | Update README with a specific change |
| `/memory` | Show recent memories |
| `/remember <fact>` | Save a fact to the current project memory |
| `/remember --global <fact>` | Save to global memory (shared across projects) |
| `/files` | List recently created files |
| `/model` | Show the active model |
| `/model <name>` | Switch model for this session |
| `/model <name> --save` | Persist model for the current project |
| `/at <time> \| <prompt>` | One-off reminder (`tomorrow 18:00`, `friday 09:00`, `in 2h`) |
| `/schedule <name> \| <when> \| <prompt>` | Recurring scheduled task |
| `/schedules` | List your scheduled tasks (with remove buttons) |
| `/unschedule <id>` | Remove a scheduled task |
| `/new` | Fresh start — clear history and reset to global |
| `/clear` | Clear conversation history only |
| `/help` | Show all commands |
| `/apikey new <name>` | Create a new API key _(admin only)_ |
| `/apikeys` | List all API keys with last-used time _(admin only)_ |
| `/apikey revoke <id>` | Revoke an API key _(admin only)_ |

### Projects

Projects are the core concept. Each project gets:

- Its own directory on the machine
- A `README.md` that the AI reads on every run (defines purpose, instructions, data schema)
- Its own memory (extracted automatically after each conversation)
- Its own file history
- Its own schedules

```
/project research | Track industry news and produce weekly PDF digests
```

This creates the project directory and generates a README based on your description. From then on, every message in that project context includes the README as instructions.

### Scheduling

Natural language scheduling that converts to cron:

```
/schedule digest | every day 08:00 | Search for sync industry news and update data.json
/schedule standup | every weekday 09:00 | What should I focus on today?
/at in 2h | remind me to review the report
/at friday 14:00 | send me a summary of the week
```

Schedules survive reboots. One-off reminders (`/at`) delete themselves after firing.

### Persona

The persona is defined entirely in the config — no code changes needed. The `name` is used in responses and the `/help` header. The `system_prompt` is injected into every request and shapes how the bot behaves.

A good system prompt is short and direct. It should define:
- Who the bot is and what its job is
- Tone (concise, friendly, formal — your call)
- Any standing rules (e.g. "never mention file paths", "always respond in English")

The persona is combined with the active project's README on each request, so you can keep the global persona generic and let individual projects define more specific behaviour through their own README instructions.

```yaml
persona:
  name: "Jarvis"
  system_prompt: |
    You are Jarvis — a calm, precise assistant.
    Be brief. Prefer bullet points over paragraphs.
    When asked to do something, do it — no caveats.
```

### User approval

The bot supports multiple users without running multiple instances. When someone new messages the bot, the admin (you) gets a notification with **Approve / Deny** buttons. Approved users get their own fully isolated environment — separate projects, memory, files, and schedules.

---

## Configuration

`~/.config/bot/default/config.yaml`:

```yaml
telegram:
  token: "YOUR_BOT_TOKEN"
  allowed_user_ids:
    - 123456789
  admin_user_id: 123456789

backend:
  type: "claude-code"        # or "opencode"
  binary: "/path/to/claude"
  working_dir: "~/bot-workspace/default"
  default_model: "claude-sonnet-4-6"
  extract_model: "claude-haiku-4-5"  # cheaper model for background memory extraction

persona:
  name: "Artoo"
  system_prompt: |
    You are Artoo — a sharp, reliable personal assistant.
    Be concise and natural. Never use the same greeting twice.
    When asked to do something, just do it. No disclaimers.

memory:
  max_age_days: 90

api:
  port: 8088  # set to 0 to disable
```

---

## REST API

The bot exposes an optional HTTP API so other services on your machine can trigger messages or run tasks. Enable it by setting `api.port` in your config.

### Authentication

All endpoints (except `/v1/health`) require an API key passed as a Bearer token:

```
Authorization: Bearer artoo_a1b2c3...
```

**Managing keys** (admin only, via Telegram):

```
/apikey new <name>     — create a new key (shown once, copy it)
/apikeys               — list all keys with last-used time
/apikey revoke <id>    — permanently revoke a key
```

---

### `GET /v1/health`

No authentication required. Returns bot status.

```bash
curl http://localhost:8088/v1/health
```

```json
{"bot": "Artoo", "status": "ok"}
```

---

### `POST /v1/send`

Send a text message to a user via Telegram.

```bash
curl -X POST http://localhost:8088/v1/send \
  -H "Authorization: Bearer artoo_..." \
  -H "Content-Type: application/json" \
  -d '{"text": "Your report is ready"}'
```

| Field | Type | Required | Description |
|---|---|---|---|
| `text` | string | ✓ | Message to send |
| `user_id` | int | — | Telegram user ID. Defaults to admin if omitted |

```json
{"ok": true}
```

---

### `POST /v1/run`

Run a prompt as a user. The task runs in the background and the result is sent via Telegram when complete.

```bash
curl -X POST http://localhost:8088/v1/run \
  -H "Authorization: Bearer artoo_..." \
  -H "Content-Type: application/json" \
  -d '{"prompt": "check disk usage and summarise", "workspace": "global"}'
```

| Field | Type | Required | Description |
|---|---|---|---|
| `prompt` | string | ✓ | The prompt to run |
| `user_id` | int | — | Telegram user ID. Defaults to admin if omitted |
| `workspace` | string | — | Project to run in. Defaults to user's active project |

```json
{"status": "queued"}
```

The response returns immediately. The result arrives as a Telegram message when the task finishes.

---

### Example: trigger from a cron job or script

```bash
#!/bin/bash
curl -s -X POST http://localhost:8088/v1/run \
  -H "Authorization: Bearer $ARTOO_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"prompt\": \"$1\", \"workspace\": \"${2:-global}\"}"
```

---

## Architecture

```
Telegram ──→ Bot (Go) ←── HTTP API  (Bearer token auth)
                ├── SQLite  (memories, projects, schedules, users, api keys)
                ├── Cron runner  (schedules, one-off reminders)
                └── exec.Command  ──→  agentic CLI  (claude, opencode, ...)
                                            └── runs on your machine
                                                with full filesystem access
```

The Go process is intentionally thin. It handles:
- Telegram polling and message routing
- Per-user session and project state
- Memory extraction (background, uses a cheaper model)
- Cron scheduling
- File uploads and delivery

Everything else — web search, file manipulation, code execution, PDF generation — is delegated to the configured CLI tool. The system prompt includes the persona, working directory rules, the project README, relevant memories, and recent conversation history.

---

## Roadmap

- [ ] Custom skills (define your own `/commands` without editing Go code)
- [ ] Local MCP server for skill registration and execution
- [ ] Voice message support
- [ ] Image generation commands
- [ ] Multi-modal file handling (images, audio)

---

## Why Artoo?

I'm a Star Wars fan. The name felt right for a loyal, always-on robot that just gets things done without making a fuss. Call yours whatever you want — the persona name and system prompt are fully configurable in the setup wizard.

<p align="center">
  <img src="artoo.png" width="480" alt="Artoo — R2-D2 in a server room" />
</p>

---

## License

MIT
