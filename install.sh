#!/bin/bash
# install.sh — install/reinstall a bot instance as a LaunchAgent
# Usage: bash install.sh [instance-name]
#   instance-name defaults to "default"
# Examples:
#   bash install.sh         → installs "default" instance
#   bash install.sh rex     → installs "rex" instance
#   bash install.sh sara    → installs "sara" instance
set -e

INSTANCE="${1:-default}"
LABEL="com.bot.claude.$INSTANCE"
PLIST_DIR="$HOME/Library/LaunchAgents"
PLIST="$PLIST_DIR/$LABEL.plist"
BOT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "Installing bot instance: $INSTANCE"

# Remove old system daemon if it exists (requires sudo once, backwards compat)
if sudo launchctl print system/com.bot.claude &>/dev/null 2>&1; then
    echo "Removing old system daemon..."
    sudo launchctl bootout system /Library/LaunchDaemons/com.bot.claude.plist
    sudo rm -f /Library/LaunchDaemons/com.bot.claude.plist
fi

# Build binary (shared across all instances)
echo "Building bot..."
cd "$BOT_DIR" && go build -o bot .

# Create LaunchAgent plist
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

# Unload if already running
launchctl unload "$PLIST" 2>/dev/null || true

# Load
launchctl load -w "$PLIST"

echo ""
echo "Bot '$INSTANCE' installed and running."
echo "Config:  ~/.config/bot/$INSTANCE/config.yaml"
echo "Logs:    $BOT_DIR/bot.$INSTANCE.err"
echo ""
echo "Useful commands:"
echo "  launchctl unload $PLIST           # stop"
echo "  launchctl load -w $PLIST          # start"
echo "  launchctl kickstart gui/$(id -u)/$LABEL  # restart"
