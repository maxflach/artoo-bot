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
cd "$BOT_DIR" && go build -o bot .

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
