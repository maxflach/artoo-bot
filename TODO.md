# Telegram → Claude Code Bot

A lightweight Telegram bot that routes messages directly to Claude Code (`claude -p`),
giving full Claude Code power accessible from your phone.

## Stack
- Go + go-telegram-bot-api
- Runs as `max` user (full machine access)
- LaunchDaemon for auto-start on boot

## TODO

### Setup
- [ ] Create new Telegram bot via BotFather
- [ ] `go mod init bot`
- [ ] `go get github.com/go-telegram-bot-api/telegram-bot-api/v5`
- [ ] Create `main.go` — core message handler
- [ ] Create `.env` — store bot token + allowed Telegram user IDs
- [ ] Test locally: send message → `claude -p` → response back

### Bot features
- [ ] Basic message → claude -p → reply
- [ ] Show typing indicator while Claude is thinking
- [ ] Handle long responses (Telegram max 4096 chars — split if needed)
- [ ] /new command — not needed (each message is already stateless)
- [ ] /help command — show what the bot can do

### Security
- [ ] Allowlist by Telegram user ID (not username — IDs can't be spoofed)
- [ ] Ignore all messages from non-allowlisted users silently

### Auto-start
- [ ] Create LaunchDaemon plist
- [ ] Load and test reboot survival

### Nice to have
- [ ] Pass working directory context in system prompt
- [ ] /cd command — change active project directory
- [ ] Streaming responses (send partial replies as Claude thinks)
