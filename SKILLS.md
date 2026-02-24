# Skills

Skills extend the bot with custom `/commands`. Drop a script or folder into a `skills/` directory and it becomes immediately available — no code changes needed.

---

## Overview

| Skill | Command | Description | Secrets required |
|---|---|---|---|
| [dadjoke](#dadjoke) | `/dadjoke` | Tell a random dad joke | none |
| [imagine](#imagine) | `/imagine <prompt>` | Generate an image via Gemini Imagen | `GEMINI_API_KEY` |
| [gmail](#gmail) | `/gmail [command]` | Access your Gmail inbox | `CLIENT_ID`, `CLIENT_SECRET`, `REFRESH_TOKEN` |
| [gcal](#gcal) | `/gcal [command]` | View and manage Google Calendar | `CLIENT_ID`, `CLIENT_SECRET`, `REFRESH_TOKEN` |

---

## How skills work

**Skill locations** (searched in order, later entries override earlier):

| Path | Scope |
|---|---|
| `~/.config/bot/skills/` | Global — all instances |
| `~/.config/bot/<instance>/skills/` | Per-instance |
| `<project_dir>/skills/` | Per-project (active project only) |

**Skill types:**

- **Executable** (`.sh` or any executable file) — run with user input as args; stdout returned to the user
- **Markdown** (`.md`) — contents prepended to the user's message and run through Claude
- **Folder-based** — a folder with a `run.sh` entrypoint; can bundle data files

The description line shown in `/skills` is taken from the first comment line in `run.sh` (the line beginning with `# `).

Skills also double as **MCP tools** — Claude can call any loaded skill mid-task when the API server is enabled.

---

## dadjoke

Tells a random dad joke from a bundled JSON list.

**Installed by:** `install.sh` (automatically)

**Usage:**
```
/dadjoke
```

**Files:**
```
~/.config/bot/skills/dadjoke/
├── run.sh
└── jokes.json
```

No secrets or configuration needed.

---

## imagine

Generates an image from a text prompt using the [Google Gemini Imagen API](https://ai.google.dev/gemini-api/docs/image-generation) and sends it back as a photo.

**Installed by:** `install.sh` (automatically)

**Usage:**
```
/imagine a tiny robot sitting on a cloud at sunset
```

The generated image is saved as a PNG to your working directory and sent back via Telegram.

**Setup:**

1. Get a Gemini API key from [aistudio.google.com](https://aistudio.google.com)
2. Store it:
   ```
   /secret set --global GEMINI_API_KEY AIzaSy... --skill imagine
   ```
   Using `--global` means it works in every project without repeating the command.

**Files:**
```
~/.config/bot/skills/imagine/
└── run.sh
```

---

## gmail

Full Gmail inbox access — read, search, archive, and triage your email.

**Installation:** copy to your skills folder:
```bash
cp -r path/to/skills/gmail ~/.config/bot/skills/
```

**Usage:**

| Command | Description |
|---|---|
| `/gmail` | Inbox overview — unread count + top important emails |
| `/gmail unread` | List recent unread (up to 15) with short IDs |
| `/gmail important` | Inbox + Important, unread first |
| `/gmail cleanup` | Unread counts by category (Promotions, Social, Updates) with top senders |
| `/gmail search <query>` | Search using any Gmail operator (`from:alice`, `subject:report`, etc.) |
| `/gmail read <id>` | Full email body (short 8-char ID from any list) |
| `/gmail archive <id>` | Archive a message (removes from Inbox) |
| `/gmail trash <id>` | Move to Trash |
| `/gmail archive-all <query>` | Bulk-archive all messages matching a Gmail query |
| `/gmail <anything else>` | Treated as a Gmail search |

**Example output:**
```
📬 Inbox — 12 unread

[abc123ef] From: Alice Smith
           Subject: Quarterly Review
           2h ago · Important ⭐
           "Hi, just wanted to follow up on the proposal..."

[def45678] From: GitHub
           Subject: PR review requested
           5h ago
           "maxflach requested your review on..."
```

Short IDs (first 8 chars) are stable within a session — chain commands freely:
```
/gmail read abc123ef
/gmail archive abc123ef
/gmail archive-all from:newsletter@substack.com older_than:7d
```

**Claude integration (MCP):** Claude can call `/gmail` mid-task — e.g. fetch recent emails to summarise, then archive the ones actioned.

**Setup:**

Requires an OAuth2 refresh token. No `gcloud` CLI needed — use the OAuth Playground on your laptop.

**Step 1: Create OAuth2 credentials**
1. [Google Cloud Console](https://console.cloud.google.com) → APIs & Services → Enable **Gmail API**
2. Credentials → Create credentials → **OAuth 2.0 Client ID** → Application type: **Desktop app**
3. Copy the `client_id` and `client_secret`
4. APIs & Services → **OAuth consent screen** → set to External, add yourself as a test user
5. After confirming everything works, click **Publish** on the consent screen — this prevents the refresh token from expiring every 7 days. No Google review is required for personal scopes.

**Step 2: Get a refresh token**
1. Go to [developers.google.com/oauthplayground](https://developers.google.com/oauthplayground) on your laptop
2. Gear icon (top right) → check "Use your own OAuth credentials" → enter your `client_id` and `client_secret`
3. In the scope box, add: `https://www.googleapis.com/auth/gmail.modify`
4. Click "Authorize APIs" → sign in → "Exchange authorization code for tokens"
5. Copy the `refresh_token`

**Step 3: Store secrets**
```
/secret set CLIENT_ID <id> --skill gmail
/secret set CLIENT_SECRET <secret> --skill gmail
/secret set REFRESH_TOKEN <token> --skill gmail
```

**Files:**
```
~/.config/bot/skills/gmail/
├── run.sh
└── gmail.py
```

---

## gcal

View and manage your Google Calendar — today's schedule, weekly overview, event search, and event creation.

**Installation:** copy to your skills folder:
```bash
cp -r path/to/skills/gcal ~/.config/bot/skills/
```

**Usage:**

| Command | Description |
|---|---|
| `/gcal` or `/gcal today` | Today's events |
| `/gcal tomorrow` | Tomorrow's events |
| `/gcal week` | This week (Monday–Sunday) |
| `/gcal next` | The single next upcoming event |
| `/gcal search <query>` | Search events by keyword |
| `/gcal add <title> \| <date> \| <time>` | Create an event |
| `/gcal <YYYY-MM-DD>` | Events for a specific date |
| `/gcal <day name>` | Events for e.g. `friday`, `monday` |

**Example output:**
```
📅 Tuesday, Feb 24

  10:00 – 11:00   Team standup
                  Google Meet · 4 attendees
  14:00 – 15:00   1:1 with Alice
  19:00 – 20:00   Dinner reservation
```

**Adding events:**
```
/gcal add Team lunch | tomorrow | 12:00-13:00
/gcal add Doctor appointment | friday | 09:30
/gcal add Call with Bob | 2026-03-15 | 15:00-15:30
```

Dates accept: `today`, `tomorrow`, day names (`monday`, `friday`), or `YYYY-MM-DD`. Times accept `HH:MM-HH:MM` or just `HH:MM` (defaults to 1 hour duration).

**Setup:**

Same OAuth2 flow as Gmail — you can reuse the same `client_id`, `client_secret`, and `refresh_token` if both skills use the same Google account.

**Step 1: Enable Calendar API**
1. [Google Cloud Console](https://console.cloud.google.com) → APIs & Services → Enable **Google Calendar API**
2. Use the same OAuth2 Desktop app credentials as Gmail (or create new ones)
3. If setting up fresh (no Gmail app yet): OAuth consent screen → External, add yourself as a test user, then **Publish** — prevents the refresh token expiring every 7 days. No Google review required.

**Step 2: Get a refresh token** (if not already done for Gmail)
1. Go to [developers.google.com/oauthplayground](https://developers.google.com/oauthplayground) on your laptop
2. Gear icon → enter your `client_id` and `client_secret`
3. Authorize scope: `https://www.googleapis.com/auth/calendar`
4. Exchange auth code → copy `refresh_token`

> **Tip:** If you already have a token with `gmail.modify` scope, add `calendar` scope in the same OAuth Playground session and exchange once to get a single token that covers both. Then use the same token for both skills.

**Step 3: Store secrets**
```
/secret set CLIENT_ID <id> --skill gcal
/secret set CLIENT_SECRET <secret> --skill gcal
/secret set REFRESH_TOKEN <token> --skill gcal
```

**Files:**
```
~/.config/bot/skills/gcal/
├── run.sh
└── gcal.py
```

---

## Writing your own skills

**Minimal shell script skill:**
```bash
#!/bin/bash
# Short description shown in /skills
echo "Hello, $*"
```
Save as `~/.config/bot/skills/hello` (executable), then `/skills reload`.

**Markdown prompt skill:**
```markdown
You are a code reviewer. Be concise and direct.
Focus on correctness, then readability, then style.
```
Save as `~/.config/bot/skills/review.md`. Invoked as `/review <code>`.

**Folder skill with secrets:**
```
~/.config/bot/skills/myskill/
├── run.sh       # #!/bin/bash / # Description / exec python3 "$(dirname "$0")/main.py" "$@"
└── main.py      # reads ARTOO_SECRET_MY_KEY from os.environ
```

**Available environment variables in every skill:**

| Variable | Value |
|---|---|
| `ARTOO_WD` | The user's current working directory |
| `ARTOO_SECRET_<NAME>` | Decrypted secret values locked to this skill |

Secrets must be locked at storage time with `--skill <name>`. See [README.md](README.md#secrets-vault) for full details.
