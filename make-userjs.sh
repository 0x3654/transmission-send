#!/bin/bash
# Пересобрать userscript-обёртку после правок transmission-send.js
cd "$(dirname "$0")"
cat userscript-header.txt transmission-send.js > transmission-send.user.js
echo "OK: transmission-send.user.js ($(wc -l < transmission-send.user.js | tr -d ' ') строк)"
