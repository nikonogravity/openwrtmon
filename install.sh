#!/usr/bin/env bash
# Installation script for Linux & macOS
set -e

APP_NAME="openwrtmon"
INSTALL_BIN="$HOME/.local/bin"
CONFIG_DIR="$HOME/.config/$APP_NAME"
OS="$(uname -s)"

echo "=== Установка OpenWRT Monitor ($OS) ==="

mkdir -p "$INSTALL_BIN"
mkdir -p "$CONFIG_DIR"

# 1. Build or locate binary
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_PATH=""

if [ -f "$SCRIPT_DIR/dist/$APP_NAME" ]; then
    BIN_PATH="$SCRIPT_DIR/dist/$APP_NAME"
elif command -v go >/dev/null 2>&1; then
    echo "Компиляция через Go..."
    go build -ldflags="-s -w" -o "$SCRIPT_DIR/dist/$APP_NAME" "$SCRIPT_DIR/cmd/openwrtmon"
    BIN_PATH="$SCRIPT_DIR/dist/$APP_NAME"
else
    echo "ОШИБКА: Исполняемый файл не найден и Go не установлен."
    exit 1
fi

cp -f "$BIN_PATH" "$INSTALL_BIN/$APP_NAME"
chmod +x "$INSTALL_BIN/$APP_NAME"
echo "✓ Скопировано в $INSTALL_BIN/$APP_NAME"

# 2. Config file
if [ ! -f "$CONFIG_DIR/config.ini" ]; then
    cp "$SCRIPT_DIR/config.ini" "$CONFIG_DIR/config.ini"
    echo "✓ Создан начальный $CONFIG_DIR/config.ini"
fi

# 3. Autostart setup
if [ "$OS" = "Linux" ]; then
    # Systemd user service
    SERVICE_DIR="$HOME/.config/systemd/user"
    mkdir -p "$SERVICE_DIR"
    cat <<EOF > "$SERVICE_DIR/$APP_NAME.service"
[Unit]
Description=OpenWRT Monitor
After=network.target

[Service]
ExecStart=$INSTALL_BIN/$APP_NAME
WorkingDirectory=$CONFIG_DIR
Restart=on-failure

[Install]
WantedBy=default.target
EOF
    systemctl --user daemon-reload
    systemctl --user enable "$APP_NAME.service"
    systemctl --user restart "$APP_NAME.service"
    echo "✓ Настроен и запущен systemd user service"

elif [ "$OS" = "Darwin" ]; then
    # macOS LaunchAgent
    PLIST_PATH="$HOME/Library/LaunchAgents/com.openwrtmon.plist"
    cat <<EOF > "$PLIST_PATH"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.openwrtmon</string>
    <key>ProgramArguments</key>
    <array>
        <string>$INSTALL_BIN/$APP_NAME</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>WorkingDirectory</key>
    <string>$CONFIG_DIR</string>
</dict>
</plist>
EOF
    launchctl unload "$PLIST_PATH" 2>/dev/null || true
    launchctl load "$PLIST_PATH"
    echo "✓ Настроен и запущен LaunchAgent (автозапуск на macOS)"
fi

echo ""
echo "🎉 Установка успешно завершена!"
echo "Приложение работает в фоновом режиме."
echo "Панель управления доступна по адресу: http://127.0.0.1:18765"

