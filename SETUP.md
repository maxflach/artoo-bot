# Bot Setup Guide

A Telegram bot that gives you (or anyone you trust) direct access to Claude Code on your machine.

## Prerequisites

- Go 1.21+
- Claude Code CLI installed (`claude` binary in PATH)
- A Telegram bot token from [@BotFather](https://t.me/botfather)
- Your Telegram user ID (send `/start` to [@userinfobot](https://t.me/userinfobot))

---

## Setting Up a New Bot Instance

Each instance has its own Telegram token, persona, memory, and workspace.
You can run multiple instances simultaneously (e.g., one for you, one for your wife).

### Step 1: Run the setup wizard

```bash
cd ~/code/bot
./bot --instance <name> --setup
```

Replace `<name>` with a short identifier like `rex`, `sara`, or `default`.

The wizard will ask for:
- Telegram bot token
- Your Telegram user ID
- Claude binary path (default: `~/.local/bin/claude`)
- Bot persona name
- Memory max age in days (default: 90)

Config is saved to: `~/.config/bot/<name>/config.yaml`

### Step 2: Create a workspace directory

```bash
mkdir -p ~/bot-workspace/<name>
```

### Step 3: Install as a background service

```bash
bash ~/code/bot/install.sh <name>
```

This builds the binary and installs a LaunchAgent that starts automatically on login and restarts on crash.

---

## Running Multiple Instances

Each instance needs its own Telegram bot (create via @BotFather).

```bash
# Set up
./bot --instance rex  --setup
./bot --instance sara --setup

# Install both
bash install.sh rex
bash install.sh sara
```

Each gets its own:
- Config: `~/.config/bot/rex/config.yaml`
- Memory DB: `~/.config/bot/rex/memory/bot.db`
- Workspace: `~/bot-workspace/rex/`
- Log files: `bot.rex.err`, `bot.sara.err`

---

## Config File Reference

`~/.config/bot/<name>/config.yaml`:

```yaml
telegram:
  token: "YOUR_BOT_TOKEN"
  allowed_user_ids:
    - 123456789  # your Telegram user ID

backend:
  type: "claude-code"
  binary: "/Users/you/.local/bin/claude"
  working_dir: "/Users/you/bot-workspace/name"
  default_model: "claude-sonnet-4-6"
  extract_model: "claude-haiku-4-5"

persona:
  name: "Rex"
  system_prompt: |
    You are Rex — a sharp assistant running on coruscant.
    Be concise and natural. Never use the same greeting twice.
    You have full access to the filesystem and can run commands.
    When asked to do something, just do it. No disclaimers.

memory:
  max_age_days: 90
```

---

## Bot Commands

| Command | Description |
|---------|-------------|
| `/help` | Show all commands |
| `/cwd` | Show current directory and workspace |
| `/cd <path>` | Change directory (auto-sets workspace name) |
| `/workspace <name>` | Switch workspace manually |
| `/model` | Show active model |
| `/model <name>` | Switch model for this session |
| `/model <name> --save` | Persist model for current workspace |
| `/remember <fact>` | Save a memory to current workspace |
| `/remember --global <fact>` | Save a memory globally (all workspaces) |
| `/memory` | Show recent memories |
| `/files` | Show recently created files |
| `/schedule <name> \| <cron> \| <prompt>` | Add a scheduled task |
| `/schedules` | List all scheduled tasks |
| `/unschedule <id>` | Remove a scheduled task |
| `/clear` | Clear conversation history |

### Scheduled Tasks

Use standard 5-field cron syntax:

```
/schedule morning-news | 0 8 * * * | Fetch top headlines and send a brief summary
/schedule weekly-backup | 0 23 * * 0 | Back up my notes to a timestamped archive
```

```
┌─ minute (0-59)
│ ┌─ hour (0-23)
│ │ ┌─ day of month (1-31)
│ │ │ ┌─ month (1-12)
│ │ │ │ ┌─ day of week (0=Sun)
│ │ │ │ │
0 8 * * *   → every day at 8:00 AM
0 */6 * * * → every 6 hours
0 9 * * 1   → every Monday at 9:00 AM
```

---

## Service Management

```bash
# Stop a bot
launchctl unload ~/Library/LaunchAgents/com.bot.claude.rex.plist

# Start a bot
launchctl load -w ~/Library/LaunchAgents/com.bot.claude.rex.plist

# Restart a bot
launchctl kickstart gui/$(id -u)/com.bot.claude.rex

# View logs
tail -f ~/code/bot/bot.rex.err
```

---

## Docker

Instead of building natively, you can run the bot in Docker. The image includes Claude Code, `pdftotext`, `pandoc`, and `python3`.

```bash
# One-time: authenticate Claude Code on the host
claude login

# Set backend.binary: /usr/local/bin/claude in your config, then:
docker compose up -d

# Named instance
docker compose run --rm artoo --instance workbot

# Logs
docker compose logs -f

# Update
docker compose build --no-cache && docker compose up -d
```

---

## Updating the Bot

```bash
cd ~/code/bot
git pull           # if using git
bash install.sh rex    # rebuilds and restarts
```

---

## Troubleshooting

**Bot not responding**
- Check logs: `tail -f ~/code/bot/bot.rex.err`
- Verify config: `cat ~/.config/bot/rex/config.yaml`
- Test manually: `./bot --instance rex`

**"Conflict: terminated by other getUpdates"**
- Two instances with the same token are running. Stop one.

**Claude errors / permission prompts**
- The bot uses `--allowedTools Bash,Read,Write,Edit,Glob,Grep,WebSearch,WebFetch`
- If Claude asks for permissions, make sure the binary has Full Disk Access in System Settings → Privacy & Security

**Files not being sent**
- Files are detected by watching the working directory for new files after each Claude call
- Make sure `working_dir` in config points to a writable directory
