# Transmission Send — плагин для Lampa

Минимальный плагин: пункты в меню долгого нажатия на торренте, чтобы перекинуть
раздачу в Transmission вместо просмотра онлайн. Никаких настроек и RPC.

## Пункты меню

В порядке появления (наши — после встроенных пунктов Lampa):

| Пункт | Когда показывается | Что делает |
|---|---|---|
| **Скачать .torrent** | у раздачи есть прямая http(s)-ссылка на файл | браузерная загрузка |
| **Скопировать магнет** | есть магнет | магнет в буфер обмена |
| **Открыть магнет** (всегда последним) | есть магнет | `window.location = magnet:` → системный обработчик |

Сценарии:

- **Mac**: «Открыть магнет» → Lampa Magnet.app → `ssh micro` → `transmission-remote -a` →
  торрент на сидбоксе в один тап (см. `mac-handler/`). Альтернативы: «Скопировать магнет» →
  Transmission Remote GUI; «Скачать .torrent» → загрузки → folder action.
- **iPhone**: «Скопировать магнет» → вставить в NASctl. «Открыть магнет» сработает, только
  если установлено приложение, зарегистрировавшее схему `magnet:` (iOS сама не открывает).
- **Apple TV**: пункты появляются, но скачивать там некуда.

## Установка плагина

```
https://0x3654.github.io/transmission-send/transmission-send.js
```

Настройки → Расширения → «+» → вставить URL. На каждом устройстве отдельно
(PWA на iPhone — отдельно от вкладки Safari).

## Обработчик magnet: для macOS (mac-handler/)

Чтобы «Открыть магнет» добавлял торрент на сервер Transmission:

```bash
cd mac-handler
./install.sh
```

Что делает: компилирует `~/Applications/Lampa Magnet.app` (AppleScript: ловит магнет,
дёргает `ssh micro transmission-remote 127.0.0.1:9091 -a <магнет>`), регистрирует схему
`magnet:` в LaunchServices. Требования: ssh-ключ на сервер, `transmission-remote` на нём.

Проверка (безвредный нулевой хеш): `open 'magnet:?xt=urn:btih:0000...'` — выскочит
уведомление. Сброс привязки схемы:
`defaults delete com.apple.LaunchServices/com.apple.launchservices.secure LSHandlers`.

Зачем так: в Transmission 4.1.3+ CORS из RPC удалён, поэтому браузер (и PWA) не может
стучаться в RPC напрямую — нативный обработчик схемы обходит это легально, как
Transmission Remote GUI.

## Разработка

- `node --check transmission-send.js` — синтаксис
- `node smoke-test.js` — смоук-тесты на стабах Lampa API
- Хуки: `Lampa.Listener('torrent'/'torrent_file', onlong)` — официальная точка
  расширения (пуш пунктов в `e.menu` с собственным `onSelect`), проверено по
  исходникам Lampa 3.3.3 (github.com/yumata/lampa-source)
