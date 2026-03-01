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
ARCH="$(uname -m)"
REPO="maxflach/artoo-bot"

echo "Installing bot instance: $INSTANCE"

# ── Detect platform ────────────────────────────────────────────────────────────
case "$OS" in
  Darwin) OS_SLUG="darwin" ;;
  Linux)  OS_SLUG="linux"  ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac
case "$ARCH" in
  x86_64)          ARCH_SLUG="amd64" ;;
  arm64 | aarch64) ARCH_SLUG="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# ── Download latest release ────────────────────────────────────────────────────
echo "Fetching latest release..."
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\(.*\)".*/\1/')
if [ -z "$LATEST" ]; then
  echo "Could not determine latest release. Check your internet connection."
  exit 1
fi
echo "Latest release: $LATEST"

ASSET="artoo-${OS_SLUG}-${ARCH_SLUG}"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST}/${ASSET}"
ICNS_URL="https://github.com/${REPO}/releases/download/${LATEST}/artoo.icns"

echo "Downloading $ASSET..."
curl -fSL "$DOWNLOAD_URL" -o "$BOT_DIR/bot"
chmod +x "$BOT_DIR/bot"

# Download icon (best-effort)
curl -fsSL "$ICNS_URL" -o "$BOT_DIR/artoo.icns" 2>/dev/null || true

# ── App bundle (icon + code signing) ───────────────────────────────────────────
echo "Creating Artoo.app bundle..."
BUNDLE="$BOT_DIR/Artoo.app"
mkdir -p "$BUNDLE/Contents/MacOS" "$BUNDLE/Contents/Resources"
cp "$BOT_DIR/bot" "$BUNDLE/Contents/MacOS/bot"
cp "$BOT_DIR/artoo.icns" "$BUNDLE/Contents/Resources/artoo.icns" 2>/dev/null || true

cat > "$BUNDLE/Contents/Info.plist" << 'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>bot</string>
    <key>CFBundleIdentifier</key>
    <string>com.bot.artoo</string>
    <key>CFBundleName</key>
    <string>Artoo</string>
    <key>CFBundleIconFile</key>
    <string>artoo</string>
    <key>CFBundleVersion</key>
    <string>1.0</string>
    <key>CFBundleShortVersionString</key>
    <string>1.0</string>
    <key>LSUIElement</key>
    <true/>
</dict>
</plist>
PLIST

# Sign with local identity if available
if security find-identity -v -p codesigning 2>/dev/null | grep -q "Max Flach"; then
    codesign --force --sign "Max Flach" --options runtime --timestamp=none "$BUNDLE" 2>/dev/null \
        && echo "Bundle signed with local certificate"
fi

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

url = f"https://generativelanguage.googleapis.com/v1beta/models/imagen-4.0-generate-001:predict?key={key}"
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

# ── Report template ────────────────────────────────────────────────────────────
TMPL_DIR="$HOME/.config/bot/report-template"
if [ ! -f "$TMPL_DIR/template.yaml" ]; then
    echo "Installing default report template..."
    mkdir -p "$TMPL_DIR"
    cat > "$TMPL_DIR/template.yaml" << 'TMPL'
# Artoo Reports — Template Configuration
# Edit this file to customise the look of all generated reports.
# Upload a modified template.yaml to any project via Telegram to override per-project.

cover:
  background_color: "#0f0f23"   # dark navy background
  title_color:      "#ffffff"
  subtitle_color:   "#aaaaaa"   # date / subtitle line
  accent_color:     "#4a9eff"   # decorative bar under title
  logo:             ""          # absolute path to PNG logo, or empty

body:
  font_size:        11
  h2_color:         "#2d3561"
  h3_color:         "#555588"
  text_color:       "#333333"
  bullet_color:     "#4a9eff"   # colour of bullet points

header:
  text:             "Artoo Reports"
  color:            "#aaaaaa"

footer:
  left:             "Artoo Reports"
  show_page_numbers: true
  color:            "#aaaaaa"

brand:
  name:             "Artoo Reports"
TMPL
    echo "Report template installed: $TMPL_DIR/template.yaml"
fi

# ── macOS ──────────────────────────────────────────────────────────────────────
if [ "$OS" = "Darwin" ]; then
    LABEL="com.bot.claude.$INSTANCE"
    PLIST_DIR="$HOME/Library/LaunchAgents"
    PLIST="$PLIST_DIR/$LABEL.plist"

    # Resolve the binary to run: prefer source-build bundle, fall back to get.sh bundle
    if [ -x "$BOT_DIR/Artoo.app/Contents/MacOS/bot" ]; then
        BOT_EXECUTABLE="$BOT_DIR/Artoo.app/Contents/MacOS/bot"
    elif [ -x "$HOME/.local/share/artoo/Artoo.app/Contents/MacOS/artoo" ]; then
        BOT_EXECUTABLE="$HOME/.local/share/artoo/Artoo.app/Contents/MacOS/artoo"
    else
        BOT_EXECUTABLE="$BOT_DIR/bot"
    fi

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
        <string>$BOT_EXECUTABLE</string>
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
