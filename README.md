# Transmission Send — плагины для Lampa

Минимальный плагин: пункты в меню долгого нажатия на торренте, чтобы перекинуть
раздачу в Transmission вместо просмотра онлайн. Никаких настроек и RPC.

Плюс плагин **Top** — два пункта меню вместо «странной ленты» (см. ниже).

## Установка

| Плагин | URL для Настройки → Расширения → «+» |
|---|---|
| transmission-send | `https://0x3654.github.io/transmission-send/transmission-send.js` |
| top | `https://0x3654.github.io/transmission-send/top.js` |

На каждом устройстве отдельно (PWA на iPhone — отдельно от вкладки Safari).

---

# Top — «адекватный топ» вместо ленты

Два пункта в главном меню:

- **Топ** — популярное/лучшее по TMDB: тренды за день/неделю, лучшее по рейтингу,
  новинки 2025+. Переключение варианта — кнопка **вправо** на экране (или через
  список). Карточки и пагинация нативные, запросы идут через API Lampa (учитывается
  прокси TMDB из настроек).
- **Топ трекеров** — топ раздач NNMClub по сидам (`tracker.php?o=10`), только
  видео-категории, обогащённый постерами/метаданными TMDB. Клик по карточке —
  обычная страница фильма в Lampa (дальше работает transmission-send).
  Раздачи, не сматчившиеся с TMDB (софт, игры, сборники), не показываются.

Настройки плагина: **Настройки → Топ → Адрес сервера топа** — URL сервера
tracker-top (см. ниже). Без адреса экран подскажет, куда его вписать.

Матчинг: запрос `search/multi` по оригинальному (или русскому) названию,
жёсткий фильтр по году ±1 для фильмов (для сезонных раздач допуск больше),
порог похожести названия; дубликаты раздач схлопываются по TMDB id (остаётся
самая сидируемая).

## tracker-top — сервер топа NNMClub

Один файл `tracker-top/server.py`, без зависимостей (python3 stdlib):
парсит топ-страницы NNMClub (cp1251), фильтрует видео-поддерево категорий
(список разделов парсится со страницы, не хардкод), кэш в памяти, CORS `*`.

    GET /top?cat=video|all&pages=1..3[&limit=N]
    GET /healthz

Запуск локально: `PORT=8355 python3 tracker-top/server.py`.
Тесты: `python3 tracker-top/tests/test_server.py` (парсер на живом снимке топа).

### Деплой (docker)

    docker build -t tracker-top tracker-top/
    docker run -d --name tracker-top --restart unless-stopped -p 8355:8355 tracker-top

Зеркало трекера и TTL — через env `NNM_BASE` / `TTL` (по умолчанию
`https://nnmclub.to`, 600 с). Сервер переносим: micro, VPN-сервер — в плагине
меняется только адрес.

В Lampa (https-страница) адрес должен быть https либо открываться из PWA без
mixed-content ограничений; проще всего опубликовать сервис через tsdproxy
(tailnet-домен с сертификатом).

---

## Пункты меню transmission-send

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

- `node --check top.js && node smoke-top.js` — синтаксис и смоук-тесты top-плагина
- `node --check transmission-send.js && node smoke-test.js` — то же для transmission-send
- `python3 tracker-top/tests/test_server.py` — парсер tracker-top на живом снимке
- Хуки: `Lampa.Listener('torrent'/'torrent_file', onlong)` — официальная точка
  расширения (пуш пунктов в `e.menu` с собственным `onSelect`), проверено по
  исходникам Lampa 3.3.3 (github.com/yumata/lampa-source)
- Экраны top-плагина: `Lampa.InteractionCategory` (нативные карточки/пагинация,
  приём официального плагина collections) + `Lampa.Api.sources.tmdb.get`;
  меню: `Lampa.Menu.addButton`; настройки: `Lampa.SettingsApi`
