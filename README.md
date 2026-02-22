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

v0.3 added a skills system and a local MCP server — drop a script or markdown file into a `skills/` folder to create new `/commands`, no code changes needed. Skills are also exposed as MCP tools Claude can call mid-task.

v0.4 adds a secrets vault. Skills can now declare which credentials they need, store them encrypted, and receive them as environment variables at runtime — without ever exposing values to Claude or other skills.

---

## How It Compares to OpenClaw

| | Artoo Bot | OpenClaw |
|---|---|---|
| Execution | Delegates to any agentic CLI on your machine | Runs its own agent loop |
| Web search | Delegated to the CLI tool (built-in to most) | Calls Brave/DuckDuckGo API directly |
| Multi-user | Yes — one instance, isolated per user | Typically one instance per user |
| Projects | Yes — README-driven, per-user, with their own dirs | Workspace support |
| Scheduling | Built-in (natural language cron) | Varies |
| Skills / plugins | Yes — custom `/commands` via `skills/` folders | Built-in skill system |
| MCP server | Yes — skills exposed as tools at `/mcp/sse` | No |
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

---

## Installation

### Quick install

```bash
curl -fsSL https://raw.githubusercontent.com/maxflach/artoo-bot/main/get.sh | bash
```

Detects your OS and architecture (macOS/Linux, arm64/amd64), downloads the right binary from the latest release, and installs it to `/usr/local/bin/artoo` (or `~/.local/bin/artoo` if you don't have write access).

Then run the setup wizard:

```bash
artoo --setup
```

### Or build from source

Requires Go 1.26+.

```bash
# macOS
brew install go

# Linux (replace the version number with the latest from https://go.dev/dl/)
curl -OL https://go.dev/dl/go1.26.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.26.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
source ~/.profile

# Linux (alternative, via snap)
sudo snap install go --classic
```

Then clone and build:

```bash
git clone https://github.com/maxflach/artoo-bot
cd artoo-bot
cd src && go build -o ../bot .
```

### Run the setup wizard

```bash
./bot --setup
```

The wizard walks through five steps:

**Step 1 · Backend** — choose your agentic CLI. Auto-detects the binary path if it's in a standard location. Set the default model and a separate (cheaper) model for background memory extraction.

**Step 2 · Telegram** — paste your bot token from BotFather and your numeric Telegram user ID. Optionally set a separate admin ID (defaults to your own ID).

**Step 3 · Persona** — give the bot a name and write a system prompt. A default is suggested based on the bot name and hostname — press enter to accept it.

**Step 4 · Memory** — how many days to retain memories (default: 90).

**Step 5 · REST API** — port for the HTTP API (default: 8088, set to 0 to disable).

A summary is shown before anything is written. Config is saved to `~/.config/bot/default/config.yaml` — excluded from git since it contains your bot token.

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
| `/skills` | List loaded custom skill commands |
| `/skills reload` | Reload skills from disk without restarting _(admin only)_ |
| `/secret set <name> <value> --skill <skill>` | Store a credential for the current project |
| `/secret set --global <name> <value> --skill <skill>` | Store a credential for all your projects |
| `/secret set --system <name> <value> --skill <skill>` | Store a credential for all users _(admin only)_ |
| `/secret list` | Show stored secret names (values never shown) |
| `/secret del <name>` | Delete a secret from the current project |
| `/secret del --global <name>` | Delete from your global scope |
| `/secret del --system <name>` | Delete a system secret _(admin only)_ |
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

### Skills

Drop a file or folder into a `skills/` directory to create a new `/command` — no code changes needed.

**Skill locations** (searched in order, later entries override earlier):

| Path | Scope |
|---|---|
| `~/.config/bot/skills/` | Global — all instances |
| `~/.config/bot/<instance>/skills/` | Per-instance |
| `<project_dir>/skills/` | Per-project (active project only) |

**Skill types:**

- **Executable** (`.sh` or any executable file) — run with user input as `$1`; stdout returned to the user
- **Markdown** (`.md`) — contents prepended to the user's input and run through Claude

**Folder-based skills** — a skill can be a folder. The entrypoint is `run.sh` (or `run`). The folder can contain data files.

```
~/.config/bot/skills/
└── dadjoke/
    ├── run.sh       # entrypoint — picked up automatically
    └── jokes.json   # bundled data file
```

A demo `dadjoke` skill is installed automatically by `install.sh`. To see what's loaded:

```
/skills
```

To add a new skill and pick it up without restarting:

```
/skills reload
```

Skills are also exposed as MCP tools — see the MCP Server section below.

### Secrets vault

Skills often need API keys — for image generation, external services, and so on. The secrets vault lets you store these credentials safely and inject them into skills at runtime.

#### How it works

1. You store a secret with `/secret set`. The value is encrypted immediately and never stored in plaintext.
2. When a skill runs, the bot checks which secrets are allowed for that skill, decrypts them, and injects them as environment variables (`ARTOO_SECRET_KEYNAME=value`).
3. The skill script reads them from its environment. Claude never sees them — secrets go directly to shell scripts, not into the AI context.

#### Skill locking — why `--skill` is required

Every secret must be locked to a specific skill name. This is the core security guarantee: **a secret can only be injected into the one skill you explicitly trusted with it.**

If you install a new skill later — even from an untrusted source — it cannot read your `GEMINI_API_KEY` or any other secret. It only gets the secrets that were locked to its own name.

```
/secret set GEMINI_API_KEY AIza... --skill imagine
→ imagine gets GEMINI_API_KEY ✓
→ any other skill gets nothing ✗
```

Without this, any skill script could read every secret you'd ever stored — making it trivial to write a skill that exfiltrates your credentials. Requiring an explicit lock makes the access declaration visible and auditable.

#### Secret scoping

Secrets are scoped to your current project by default, or globally across all projects with `--global`.

| Command | Where it's available |
|---|---|
| `/secret set KEY val --skill foo` | Your current project only |
| `/secret set --global KEY val --skill foo` | All your projects |
| `/secret set --system KEY val --skill foo` | All users on this bot _(admin only)_ |

When a skill runs, it receives secrets from all applicable scopes merged together, with more specific values overriding less specific ones:

```
System secret:           GEMINI_KEY = xxxxxxx   ← admin-provisioned, all users
Your global secret:      GEMINI_KEY = yyyyyyy   ← overrides system for you only
Your project "research": GEMINI_KEY = zzzzzzz   ← overrides all of the above
Your project "finance":  (none)                 ← uses your global GEMINI_KEY
Another user:            (none)                 ← uses the system GEMINI_KEY
```

System secrets are useful when you want to provision a shared API key for all users without each person having to set their own. Any user can override a system secret with their own value — they'll never see or access yours.

#### Encryption

Secret values are encrypted with **AES-256-GCM** before being written to the database:

- A 32-byte master key is generated once and stored at `~/.config/bot/<instance>/secrets.key` with permissions 0600 (readable only by you).
- Each secret gets its own random 12-byte nonce — even two identical values produce different ciphertext.
- The authentication tag means any tampering with the stored data is detected on decrypt.
- What's in the SQLite database: `hex(nonce || ciphertext || auth_tag)`. The key is never in the database. Stealing the `.db` file alone gives you nothing.

If you want to back up your secrets, back up both the database (`~/.config/bot/<instance>/memory/bot.db`) and the key file (`~/.config/bot/<instance>/secrets.key`). Keep them separate — together they unlock everything, apart they're useless.

#### Example: set up and use the `/imagine` skill

```
/secret set --global GEMINI_API_KEY AIzaSy... --skill imagine
→ Secret GEMINI_API_KEY saved for all your projects (global), skill: imagine ✓

/imagine a tiny robot sitting on a cloud at sunset
→ [image sent as photo]
```

Setting it with `--global` means it works in every project without having to set it again.

The `imagine` skill is installed automatically by `install.sh`. It calls the Gemini Imagen API, saves the result as a PNG to your working directory, and the bot sends it back as a Telegram photo.

---

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

An **OpenAPI 3.0 spec** is served at `/openapi.yaml` and a browsable **Swagger UI** at `/docs` (e.g. `http://localhost:8088/docs`). No authentication required to access either.

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

## MCP Server

When the API is enabled, the bot runs an MCP server on the same port, exposing all loaded skills as tools Claude can call mid-task.

**Endpoints:**

| Endpoint | Description |
|---|---|
| `GET /mcp/sse` | SSE stream — sends `endpoint` event with the message URL |
| `POST /mcp/message?id=<conn>` | JSON-RPC 2.0 messages (`initialize`, `tools/list`, `tools/call`) |

Both endpoints require the same Bearer token auth as the REST API.

**Auto-registration:** On first startup (when `api.port` is set), artoo writes its MCP server entry to `~/.claude.json` and generates a dedicated `artoo-mcp` API key. After a restart, Claude Code picks it up automatically — no manual config needed.

```json
{
  "mcpServers": {
    "artoo": {
      "type": "sse",
      "url": "http://localhost:8088/mcp/sse",
      "headers": { "Authorization": "Bearer artoo_..." }
    }
  }
}
```

Each skill becomes a tool with the skill's name and description. Claude can call `/dadjoke`, or any other skill, as part of a task.

---

## Architecture

```
Telegram ──→ Bot (Go) ←── HTTP API  (Bearer token auth)
                │               └── MCP server  (/mcp/sse, /mcp/message)
                ├── SQLite  (memories, projects, schedules, users, api keys, secrets)
                ├── secrets.key  (~/.config/bot/<instance>/secrets.key, mode 0600)
                ├── Cron runner  (schedules, one-off reminders)
                ├── Skills  (~/.config/bot/skills/, per-instance, per-project)
                │       └── env: ARTOO_SECRET_* (decrypted, skill-locked) + ARTOO_WD
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

- [x] Custom skills — drop scripts or prompts into `skills/` to create `/commands`
- [x] MCP server — skills exposed as tools for Claude mid-task
- [x] Secrets vault — encrypted credentials scoped per-project and locked to specific skills
- [x] Image generation — `/imagine` demo skill via Gemini Imagen
- [ ] Voice message support
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
