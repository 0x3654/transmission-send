#!/bin/bash
# Сборка и установка обработчика magnet: для macOS.
# Создаёт ~/Applications/Lampa Magnet.app, регистрирует схему magnet: в LaunchServices.
#
# Требования: ssh-доступ к micro без пароля (ssh-ключ), transmission-remote на сервере.

set -e

APP_DIR="$HOME/Applications"
APP_NAME="Lampa Magnet.app"
SRC="$(cd "$(dirname "$0")" && pwd)/magnet-handler.applescript"

echo "→ Компилирую $APP_DIR/$APP_NAME"
osacompile -o "$APP_DIR/$APP_NAME" "$SRC"

echo "→ Регистрирую схему magnet: в Info.plist"
PLIST="$APP_DIR/$APP_NAME/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Delete :CFBundleURLTypes" "$PLIST" 2>/dev/null || true
/usr/libexec/PlistBuddy -c "Add :CFBundleURLTypes array" "$PLIST"
/usr/libexec/PlistBuddy -c "Add :CFBundleURLTypes:0 dict" "$PLIST"
/usr/libexec/PlistBuddy -c "Add :CFBundleURLTypes:0:CFBundleURLName string ru.lampa.transmission-send" "$PLIST"
/usr/libexec/PlistBuddy -c "Add :CFBundleURLTypes:0:CFBundleURLSchemes array" "$PLIST"
/usr/libexec/PlistBuddy -c "Add :CFBundleURLTypes:0:CFBundleURLSchemes:0 string magnet" "$PLIST"

echo "→ Регистрирую в LaunchServices"
LSREG="/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
"$LSREG" -f "$APP_DIR/$APP_NAME"

echo
echo "✓ Готово. Проверка (безвредный нулевой хеш):"
echo "  open 'magnet:?xt=urn:btih:0000000000000000000000000000000000000000'"
echo
echo "При первом открытии macOS спросит разрешение — подтверди «Открыть»."
echo "Сбросить привязку, если нужно: defaults delete com.apple.LaunchServices/com.apple.launchservices.secure LSHandlers"
