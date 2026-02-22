#!/bin/bash
# install.sh — install a bot instance as a background service
# Supports macOS (LaunchAgent) and Linux (systemd user service)
#
# Usage: bash install.sh [instance-name]
#   instance-name defaults to "default"
set -e

INSTANCE="${1:-default}"
BOT_DIR="$(cd "$(dirname "$0")" && pwd)"
OS="$(uname -s)"

echo "Installing bot instance: $INSTANCE"

# ── Build ──────────────────────────────────────────────────────────────────────
echo "Building..."
cd "$BOT_DIR/src" && go build -o "$BOT_DIR/bot" .

# ── Skills ─────────────────────────────────────────────────────────────────────
SKILLS_DIR="$HOME/.config/bot/skills/dadjoke"
if [ ! -d "$SKILLS_DIR" ]; then
    echo "Installing dadjoke demo skill..."
    mkdir -p "$SKILLS_DIR"
    cat > "$SKILLS_DIR/run.sh" << 'RUNSH'
#!/bin/bash
# Tell a random dad joke
DIR="$(cd "$(dirname "$0")" && pwd)"
python3 -c "
import json, random
with open('$DIR/jokes.json') as f:
    jokes = json.load(f)
print(random.choice(jokes))
"
RUNSH
    chmod +x "$SKILLS_DIR/run.sh"
    cat > "$SKILLS_DIR/jokes.json" << 'JOKES'
[
  "Why don't scientists trust atoms? Because they make up everything!",
  "I told my wife she was drawing her eyebrows too high. She looked surprised.",
  "What do you call a fake noodle? An impasta.",
  "Why did the scarecrow win an award? Because he was outstanding in his field.",
  "I'm reading a book about anti-gravity. It's impossible to put down.",
  "Did you hear about the mathematician who's afraid of negative numbers? He'll stop at nothing to avoid them.",
  "Why can't you give Elsa a balloon? Because she'll let it go.",
  "I would tell you a construction joke, but I'm still working on it.",
  "What do you call cheese that isn't yours? Nacho cheese.",
  "Why did the bicycle fall over? Because it was two-tired.",
  "I used to hate facial hair, but then it grew on me.",
  "What do you call a sleeping dinosaur? A dino-snore.",
  "Why don't eggs tell jokes? They'd crack each other up.",
  "What do you call a factory that makes okay products? A satisfactory.",
  "Did you hear about the restaurant on the moon? Great food, no atmosphere.",
  "Why did the math book look so sad? Because it had too many problems.",
  "What do you call a bear with no teeth? A gummy bear.",
  "I only know 25 letters of the alphabet. I don't know y.",
  "Why did the golfer bring an extra pair of pants? In case he got a hole in one.",
  "What do you call a group of cows playing instruments? A moo-sical band."
]
JOKES
    echo "Demo skill installed: /dadjoke"
fi

IMAGINE_DIR="$HOME/.config/bot/skills/imagine"
if [ ! -d "$IMAGINE_DIR" ]; then
    echo "Installing imagine demo skill..."
    mkdir -p "$IMAGINE_DIR"
    cat > "$IMAGINE_DIR/run.sh" << 'RUNSH'
#!/bin/bash
# Generate an image using Google Gemini Imagen API
# Requires: /secret set GEMINI_API_KEY <key> --skill imagine

PROMPT="$*"
if [ -z "$PROMPT" ]; then
  echo "Usage: /imagine <prompt>"
  exit 1
fi

if [ -z "$ARTOO_SECRET_GEMINI_API_KEY" ]; then
  echo "No Gemini API key. Run: /secret set GEMINI_API_KEY <your-key> --skill imagine"
  exit 1
fi

if [ -z "$ARTOO_WD" ]; then
  echo "Error: ARTOO_WD not set."
  exit 1
fi

OUTPUT="$ARTOO_WD/imagine_$(date +%s).png"

python3 <<PYEOF
import urllib.request, json, base64, os, sys

key = os.environ['ARTOO_SECRET_GEMINI_API_KEY']
prompt = """$PROMPT"""
output = "$OUTPUT"

url = f"https://generativelanguage.googleapis.com/v1beta/models/imagen-3.0-generate-011:predict?key={key}"
body = json.dumps({"instances": [{"prompt": prompt}], "parameters": {"sampleCount": 1}})
req = urllib.request.Request(url, body.encode(), {"Content-Type": "application/json"})
try:
    resp = json.loads(urllib.request.urlopen(req).read())
    img_b64 = resp["predictions"][0]["bytesBase64Encoded"]
    with open(output, "wb") as f:
        f.write(base64.b64decode(img_b64))
    print(f"Image generated.")
except Exception as e:
    print(f"Error: {e}", file=sys.stderr)
    sys.exit(1)
PYEOF
RUNSH
    chmod +x "$IMAGINE_DIR/run.sh"
    echo "Demo skill installed: /imagine"
fi

# ── macOS ──────────────────────────────────────────────────────────────────────
if [ "$OS" = "Darwin" ]; then
    LABEL="com.bot.claude.$INSTANCE"
    PLIST_DIR="$HOME/Library/LaunchAgents"
    PLIST="$PLIST_DIR/$LABEL.plist"

    # Remove old system daemon if present (backwards compat)
    if sudo launchctl print system/com.bot.claude &>/dev/null 2>&1; then
        echo "Removing old system daemon..."
        sudo launchctl bootout system /Library/LaunchDaemons/com.bot.claude.plist
        sudo rm -f /Library/LaunchDaemons/com.bot.claude.plist
    fi

    mkdir -p "$PLIST_DIR"
    cat > "$PLIST" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>$LABEL</string>
    <key>ProgramArguments</key>
    <array>
        <string>$BOT_DIR/bot</string>
        <string>--instance</string>
        <string>$INSTANCE</string>
    </array>
    <key>WorkingDirectory</key>
    <string>$BOT_DIR</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>$HOME</string>
        <key>PATH</key>
        <string>$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>$BOT_DIR/bot.$INSTANCE.log</string>
    <key>StandardErrorPath</key>
    <string>$BOT_DIR/bot.$INSTANCE.err</string>
</dict>
</plist>
EOF

    launchctl unload "$PLIST" 2>/dev/null || true
    launchctl load -w "$PLIST"

    echo ""
    echo "Bot '$INSTANCE' installed and running."
    echo "Config:  ~/.config/bot/$INSTANCE/config.yaml"
    echo "Logs:    $BOT_DIR/bot.$INSTANCE.err"
    echo ""
    echo "Useful commands:"
    echo "  launchctl unload $PLIST          # stop"
    echo "  launchctl load -w $PLIST         # start"
    echo "  launchctl kickstart gui/$(id -u)/$LABEL  # restart"

# ── Linux (systemd) ────────────────────────────────────────────────────────────
elif [ "$OS" = "Linux" ]; then
    SERVICE="artoo-bot-$INSTANCE"
    SYSTEMD_DIR="$HOME/.config/systemd/user"
    SERVICE_FILE="$SYSTEMD_DIR/$SERVICE.service"

    mkdir -p "$SYSTEMD_DIR"
    cat > "$SERVICE_FILE" << EOF
[Unit]
Description=Artoo Bot ($INSTANCE)
After=network.target

[Service]
ExecStart=$BOT_DIR/bot --instance $INSTANCE
WorkingDirectory=$BOT_DIR
Restart=always
RestartSec=5
Environment=HOME=$HOME
Environment=PATH=$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin
StandardOutput=append:$BOT_DIR/bot.$INSTANCE.log
StandardError=append:$BOT_DIR/bot.$INSTANCE.err

[Install]
WantedBy=default.target
EOF

    systemctl --user daemon-reload
    systemctl --user enable --now "$SERVICE"

    echo ""
    echo "Bot '$INSTANCE' installed and running."
    echo "Config:  ~/.config/bot/$INSTANCE/config.yaml"
    echo "Logs:    $BOT_DIR/bot.$INSTANCE.err"
    echo ""
    echo "Useful commands:"
    echo "  systemctl --user stop $SERVICE      # stop"
    echo "  systemctl --user start $SERVICE     # start"
    echo "  systemctl --user restart $SERVICE   # restart"
    echo "  journalctl --user -u $SERVICE -f    # follow logs"

else
    echo "Unsupported OS: $OS"
    echo "Please set up the service manually. Binary is at: $BOT_DIR/bot"
    exit 1
fi
