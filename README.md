# Transmission Send — плагин для Lampa

Добавляет в меню долгого нажатия на торренте (в списке раздач и в списке файлов)
пункты, чтобы перекинуть раздачу в Transmission вместо просмотра онлайн.

## Пункты меню

| Пункт | Когда показывается | Что делает |
|---|---|---|
| **Скачать .torrent** | у раздачи есть прямая http(s)-ссылка на .torrent-файл | браузерная загрузка файла |
| **Скопировать магнет** | есть магнет (кроме Apple TV) | магнет в буфер обмена |
| **Открыть магнет** | есть магнет, Mac | `window.location = magnet:` → системный обработчик |
| **Добавить в Transmission** | есть магнет | прямой RPC-запрос `torrent-add` |

Сценарии:

- **Mac**: «Скачать .torrent» → файл падает в загрузки Safari → folder action скармливает его Transmission. Или «Скопировать магнет» → вставить в Transmission Remote GUI.
- **iPhone**: «Скопировать магнет» → вставить в NASctl. Или «Скачать .torrent» → файл в iCloud Drive → синк на Mac → folder action.
- **Прямой RPC** — работает при двух условиях: адрес RPC **https** и **Transmission ≤ 4.1.2** (в 4.1.3+ CORS-заголовки удалены полностью, браузер не пустит запрос со страницы lampa.mx). Если условия не выполняются — плагин покажет именованную ошибку и подсказку, пункты выше остаются рабочими.

## Хостинг плагина

Плагин — один файл `transmission-send.js`. Lampa грузит его по https-ссылке
(MIME должен быть `application/javascript`).

**GitHub Pages (рекомендую):**

```bash
gh repo create transmission-send --public --source=. --push
gh api repos/{owner}/transmission-send/pages -X POST -f source[branch]=main -f source[path]=/
# подождать пару минут, затем:
# https://<user>.github.io/transmission-send/transmission-send.js
```

**jsDelivr** (без включения Pages, но с кэшем ~12ч):

```
https://cdn.jsdelivr.net/gh/<user>/transmission-send@main/transmission-send.js
```

⚠️ `raw.githubusercontent.com` **не подходит** — отдаёт `text/plain` + `nosniff`, скрипт не исполнится.

## Установка в Lampa

Настройки → Расширения → «+» → вставить URL плагина.

Настройки хранятся в localStorage **каждого устройства** — на Mac, iPhone и Apple TV
плагин нужно добавить отдельно (в PWA на iPhone — тоже отдельно от Safari-вкладки).

Пункты «Скачать .torrent», «Скопировать магнет», «Открыть магнет» работают сразу,
без конфигурации.

## Настройки RPC (Настройки → Transmission)

- **Адрес RPC** — `https://хост:9091`. `http://` со страницы `https://lampa.mx` браузер
  заблокирует (mixed content) — плагин честно об этом скажет.
- **Логин / Пароль** — авторизация Transmission RPC. Пароль хранится в localStorage
  устройства в открытом виде (типа «password» в API Lampa нет).
- **Метка (labels)** — метки для добавляемых торрентов через запятую (Transmission 4+;
  пусто = не отправлять).
- **Проверить соединение** — покажет версию Transmission или причину блокировки.

### Как дать RPC адрес https без боли: Tailscale Serve

На Linux-сервере с Transmission (если он в tailnet):

```bash
sudo tailscale serve --bg 9091
```

Появится `https://<хост>.<tailnet>.ts.net` с валидным сертификатом. В настройках
плагина укажите `https://<хост>.<tailnet>.ts.net` — этого достаточно для mixed content.
Для CORS по-прежнему нужен Transmission ≤ 4.1.2. Проверить версию:
`transmission-remote <хост>:9091 --auth <user>:<pass> -si` или кнопка
«Проверить соединение».

## Диагностика

| Сообщение | Причина | Что делать |
|---|---|---|
| `mixed content` | страница https, адрес RPC http | https-адрес (tailscale serve) |
| `session-id (… ≥ 4.1.3)` | Transmission 4.1.3+ без CORS-заголовков | использовать копирование/скачивание |
| `Нет соединения (сервер недоступен или CORS)` | сервер не отвечает или CORS не пропустил preflight | проверить хост/порт, версию Transmission |
| `Торрент уже добавлен` | дубликат в Transmission | это не ошибка |

## Технические детали

- Хук: `Lampa.Listener('torrent'/'torrent_file', onlong)` — событие стреляет до открытия
  меню, плагин пушит пункты с собственным `onSelect` (официальная точка расширения).
- RPC: POST `torrent-add`, Basic auth, 409-handshake `X-Transmission-Session-Id`.
- Смоук-тест логики: стабы Lampa API + прогон всех веток — `node smoke-test.js`.
- Проверено по исходникам Lampa 3.3.3 (github.com/yumata/lampa-source).
