# Transmission Send — плагин для Lampa

Минимальный плагин: пункты в меню долгого нажатия на торренте, чтобы перекинуть
раздачу в Transmission вместо просмотра онлайн. Никаких настроек и RPC.

## Пункты меню

В порядке появления (наши — после встроенных пунктов Lampa):

| Пункт | Когда показывается | Что делает |
|---|---|---|
| **Скачать .torrent** | у раздачи есть прямая http(s)-ссылка на файл | браузерная загрузка |
| **Скопировать магнет** | есть магнет | магнет в буфер обмена |
| **Открыть магнет** (всегда последним) | есть магнет | `window.location = magnet:` → системный обработчик схемы |

Сценарии:

- **Mac**: «Открыть магнет» → приложение по умолчанию для `magnet:`
  (например, Transmission Remote GUI) → добавление на сервер. Запасные пути:
  «Скопировать магнет» → вставить в TRG; «Скачать .torrent» → загрузки → folder action.
- **iPhone**: «Открыть магнет» → приложение-обработчик magnet: (например, NASctl,
  откроет окно подтверждения). «Скопировать магнет» → вставить в NASctl.
- **Apple TV**: пункты появляются, но качать там некуда.

## Установка плагина

```
https://0x3654.github.io/transmission-send/transmission-send.js
```

Настройки → Расширения → «+» → вставить URL. На каждом устройстве отдельно
(PWA на iPhone — отдельно от вкладки Safari).

## Выбор приложения-обработчика magnet: на macOS

Системного UI для схем нет; переключается записью в LaunchServices
(TRG здесь для примера, bundle id `com.transgui`):

```bash
defaults write com.apple.LaunchServices/com.apple.launchservices.secure LSHandlers \
  -array-add '<dict><key>LSHandlerURLScheme</key><string>magnet</string>\
<key>LSHandlerRoleAll</key><string>com.transgui</string></dict>'
killall lsd
```

Проверить: `open 'magnet:?xt=urn:btih:0000000000000000000000000000000000000000'`.
Bundle id любого приложения: `osascript -e 'id of app "Имя"'`.

## Разработка

- `node --check transmission-send.js` — синтаксис
- `node smoke-test.js` — смоук-тесты на стабах Lampa API
- Хуки: `Lampa.Listener('torrent'/'torrent_file', onlong)` — официальная точка
  расширения (пуш пунктов в `e.menu` с собственным `onSelect`), проверено по
  исходникам Lampa 3.3.3 (github.com/yumata/lampa-source)
