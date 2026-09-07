/*
    Transmission Send — плагин Lampa (lampa.mx)

    Добавляет в меню долгого нажатия на торренте:
      • «Скачать .torrent»   — браузерная загрузка файла (iCloud/Downloads + folder action)
      • «Скопировать магнет» — буфер обмена (вставить в NASctl / Transmission Remote GUI)
      • «Добавить в Transmission» — прямая отправка через RPC (нужен https-URL и Transmission ≤ 4.1.2)
      • «Открыть магнет»      — системный обработчик (только на Mac)

    Установка: Настройки → Расширения → «+» → https-ссылка на этот файл.
    API: Lampa.Listener('torrent'/'torrent_file', onlong), Lampa.SettingsApi, Lampa.Noty.
*/

(function(){
    'use strict'

    var FLAG = '__lampa_transmission_send'

    if(window[FLAG]) return
    window[FLAG] = true

    var COMPONENT = 'transmission_send'

    var ICON = '<svg viewBox="0 0 24 24" width="30" height="30" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3v12m0 0l-5-5m5 5l5-5"/><path d="M4 21h16"/></svg>'

    function T(name){
        return Lampa.Lang.translate('transmission_send_' + name)
    }

    function cfg(key, def){
        var val = Lampa.Storage.field(key)
        return val === undefined || val === null || val === '' ? def : val
    }

    function normalizeRpcUrl(raw){
        var url = (raw || '').trim().replace(/\/+$/, '')

        if(!url) return ''

        if(!/^https?:\/\//i.test(url)) url = 'http://' + url
        if(url.indexOf('/transmission/rpc') < 0) url += '/transmission/rpc'

        return url
    }

    function rpcHost(){
        var url = normalizeRpcUrl(cfg('transmission_rpc_url', ''))
        var match = url.match(/^https?:\/\/([^/]+)/)

        return match ? match[1] : ''
    }

    function magnetOf(el){
        if(!el) return ''

        if(el.MagnetUri) return el.MagnetUri

        return /^magnet:/i.test(el.Link || '') ? el.Link : ''
    }

    function torrentUrlOf(el){
        if(!el || !/^https?:/i.test(el.Link || '')) return ''

        return el.Link
    }

    function titleOf(el){
        return (el && (el.Title || el.title || el.path_human)) || ''
    }

    function noty(text, style){
        Lampa.Noty.show(text, style ? { style: style } : {})
    }

    //---------- действия

    function copyMagnet(magnet){
        var ok   = function(){ noty(T('copied'), 'success') }
        var fail = function(){ noty(T('copy_fail'), 'error') }

        if(navigator.clipboard && navigator.clipboard.writeText){
            navigator.clipboard.writeText(magnet).then(ok, function(){
                Lampa.Utils.copyTextToClipboard(magnet, ok, fail)
            })
        }
        else{
            Lampa.Utils.copyTextToClipboard(magnet, ok, fail)
        }
    }

    function downloadTorrent(url){
        var a = document.createElement('a')

        a.href = url
        a.setAttribute('download', '')
        a.setAttribute('rel', 'noopener')
        a.style.display = 'none'

        document.body.appendChild(a)
        a.click()
        a.remove()

        noty(T('download_started'))
    }

    function openMagnet(magnet){
        noty(T('magnet_open'))
        window.location = magnet
    }

    //---------- RPC

    function rpcHeaders(){
        var headers = { 'Content-Type': 'application/json' }
        var user    = cfg('transmission_rpc_user', '')
        var pass    = cfg('transmission_rpc_password', '')

        if(user) headers['Authorization'] = 'Basic ' + btoa(user + ':' + pass)

        return headers
    }

    /**
     * Вызов Transmission RPC с 409-handshake.
     * fail(message, fatal) — fatal = ошибка конфигурации/сети, к ней добавляем подсказку.
     */
    function rpcCall(body, done, fail){
        var url = normalizeRpcUrl(cfg('transmission_rpc_url', ''))

        if(!url)                                   return fail(T('err_nourl'), true)
        if(window.location.protocol === 'https:' && url.indexOf('http://') === 0) return fail(T('err_mixed'), true)
        if(typeof fetch !== 'function')            return fail(T('err_fetch'), true)

        var headers = rpcHeaders()

        fetch(url, {
            method: 'POST',
            headers: headers,
            body: JSON.stringify(body)
        }).then(function(response){
            if(response.status === 409){
                var sid = response.headers.get('X-Transmission-Session-Id')

                if(!sid) throw { code: 'cors_header' }

                headers['X-Transmission-Session-Id'] = sid

                return fetch(url, {
                    method: 'POST',
                    headers: headers,
                    body: JSON.stringify(body)
                })
            }

            return response
        }).then(function(response){
            return response.json().then(function(json){
                if(json.result === 'success')         done(json)
                else if(json.result === 'duplicate torrent') fail(T('dup'), false)
                else                                  fail('Transmission: ' + json.result, false)
            })
        }).catch(function(e){
            if(e && e.code === 'cors_header') fail(T('err_cors_header'), true)
            else                              fail(T('err_network'), true)
        })
    }

    function rpcAdd(magnet, title){
        var args   = { filename: magnet }
        var labels = cfg('transmission_labels', '').trim()

        if(labels){
            args.labels = labels.split(',').map(function(l){ return l.trim() }).filter(function(l){ return l })
        }

        noty(T('sending'))

        rpcCall({ method: 'torrent-add', arguments: args }, function(json){
            var added = json.arguments['torrent-added'] || json.arguments['torrent-duplicate'] || {}
            var name  = added.name || title

            noty(T('added') + (name ? ': ' + name : ''), 'success')
        }, function(message, fatal){
            noty(message + (fatal ? ' ' + T('err_hint') : ''), 'error')
        })
    }

    function rpcTest(){
        noty(T('sending'))

        rpcCall({ method: 'session-stats', arguments: {} }, function(json){
            noty(T('connected') + (json.arguments.version ? ' · Transmission ' + json.arguments.version : ''), 'success')
        }, function(message, fatal){
            noty(message + (fatal ? ' ' + T('err_hint') : ''), 'error')
        })
    }

    //---------- меню

    function wrap(prev, fn){
        return function(element, item){
            fn(element, item)
            Lampa.Controller.toggle(prev)
        }
    }

    function pushMagnetItems(menu, prev, magnet, title){
        if(!magnet) return

        var canClipboard = !Lampa.Platform.is('apple_tv')

        if(canClipboard){
            menu.push({
                title: T('menu_copy'),
                onSelect: wrap(prev, function(){ copyMagnet(magnet) })
            })

            if(Lampa.Platform.macOS && (Lampa.Platform.macOS() || Lampa.Platform.desktop())){
                menu.push({
                    title: T('menu_open'),
                    onSelect: wrap(prev, function(){ openMagnet(magnet) })
                })
            }
        }

        var host = rpcHost()

        menu.push({
            title: T('menu_add'),
            subtitle: host || T('subtitle_no_rpc'),
            onSelect: wrap(prev, function(){ rpcAdd(magnet, title) })
        })
    }

    //---------- настройки

    function addSettings(){
        Lampa.SettingsApi.addComponent({
            component: COMPONENT,
            name: 'Transmission',
            icon: ICON,
            before: 'more'
        })

        var input = function(name, placeholder, field_name, field_descr){
            Lampa.SettingsApi.addParam({
                component: COMPONENT,
                param: { name: name, type: 'input', values: '', default: '', placeholder: placeholder },
                field: { name: T(field_name), description: field_descr ? T(field_descr) : undefined }
            })
        }

        input('transmission_rpc_url',      'https://192.168.1.10:9091', 'settings_url',      'settings_url_descr')
        input('transmission_rpc_user',     '',                           'settings_user',     '')
        input('transmission_rpc_password', '',                           'settings_password', 'settings_password_descr')
        input('transmission_labels',       'lampa',                      'settings_labels',   'settings_labels_descr')

        Lampa.SettingsApi.addParam({
            component: COMPONENT,
            param: { name: 'transmission_test', type: 'button' },
            field: { name: T('settings_test') },
            onChange: rpcTest
        })
    }

    //---------- хуки

    function addHooks(){
        // список раздач
        Lampa.Listener.follow('torrent', function(e){
            if(e.type !== 'onlong' || !e.menu || !e.element) return

            var prev   = Lampa.Controller.enabled().name
            var magnet = magnetOf(e.element)
            var turl   = torrentUrlOf(e.element)
            var title  = titleOf(e.element)

            if(turl){
                e.menu.push({
                    title: T('menu_download'),
                    onSelect: wrap(prev, function(){ downloadTorrent(turl) })
                })
            }

            pushMagnetItems(e.menu, prev, magnet, title)
        })

        // список файлов внутри торрента (магнет восстанавливаем из info-hash)
        Lampa.Listener.follow('torrent_file', function(e){
            if(e.type !== 'onlong' || !e.menu || !e.element || !e.element.torrent_hash) return

            var prev = Lampa.Controller.enabled().name

            pushMagnetItems(e.menu, prev, 'magnet:?xt=urn:btih:' + e.element.torrent_hash, titleOf(e.element))
        })
    }

    //---------- словарь

    function addLangs(){
        Lampa.Lang.add({
            transmission_send_menu_download:    { ru: 'Скачать .torrent',    en: 'Download .torrent' },
            transmission_send_menu_copy:        { ru: 'Скопировать магнет',  en: 'Copy magnet' },
            transmission_send_menu_add:         { ru: 'Добавить в Transmission', en: 'Add to Transmission' },
            transmission_send_menu_open:        { ru: 'Открыть магнет',      en: 'Open magnet' },
            transmission_send_subtitle_no_rpc:  { ru: 'RPC не настроен',     en: 'RPC is not configured' },

            transmission_send_copied:           { ru: 'Магнет скопирован',   en: 'Magnet copied' },
            transmission_send_copy_fail:        { ru: 'Не удалось скопировать', en: 'Copy failed' },
            transmission_send_download_started: { ru: 'Загрузка .torrent начата', en: '.torrent download started' },
            transmission_send_magnet_open:      { ru: 'Открываю магнет…',    en: 'Opening magnet…' },

            transmission_send_sending:          { ru: 'Отправляю…',          en: 'Sending…' },
            transmission_send_added:            { ru: 'Добавлено в Transmission', en: 'Added to Transmission' },
            transmission_send_dup:              { ru: 'Торрент уже добавлен', en: 'Torrent already added' },
            transmission_send_connected:        { ru: 'Соединение установлено', en: 'Connection established' },

            transmission_send_err_nourl:        { ru: 'Не указан адрес Transmission RPC (Настройки → Transmission)', en: 'Transmission RPC URL is not set (Settings → Transmission)' },
            transmission_send_err_mixed:        { ru: 'Браузер блокирует http-запрос со страницы https (mixed content) — нужен https-адрес RPC', en: 'Browser blocks http request from https page (mixed content) — RPC URL must be https' },
            transmission_send_err_cors_header:  { ru: 'Transmission не вернул session-id (в версиях ≥ 4.1.3 CORS удалён)', en: 'Transmission did not return session-id (CORS removed in ≥ 4.1.3)' },
            transmission_send_err_network:      { ru: 'Нет соединения (сервер недоступен или CORS)', en: 'No connection (server unreachable or CORS)' },
            transmission_send_err_fetch:        { ru: 'Браузер не поддерживает fetch', en: 'Browser does not support fetch' },
            transmission_send_err_hint:         { ru: '— используйте «Скопировать магнет» или «Скачать .torrent»', en: '— use "Copy magnet" or "Download .torrent" instead' },

            transmission_send_settings_url:         { ru: 'Адрес RPC', en: 'RPC URL' },
            transmission_send_settings_url_descr:   { ru: 'https://хост:9091 (http заблокируется браузером на lampa.mx)', en: 'https://host:9091 (http will be blocked by browser on lampa.mx)' },
            transmission_send_settings_user:        { ru: 'Логин', en: 'User' },
            transmission_send_settings_password:    { ru: 'Пароль', en: 'Password' },
            transmission_send_settings_password_descr: { ru: 'Хранится в localStorage устройства в открытом виде', en: 'Stored in device localStorage as plain text' },
            transmission_send_settings_labels:      { ru: 'Метка (labels)', en: 'Label (labels)' },
            transmission_send_settings_labels_descr:{ ru: 'Через запятую, пусто = без меток (Transmission 4+)', en: 'Comma-separated, empty = no labels (Transmission 4+)' },
            transmission_send_settings_test:        { ru: 'Проверить соединение', en: 'Test connection' }
        })
    }

    //---------- запуск

    function init(){
        addLangs()
        addSettings()
        addHooks()
    }

    if(window.appready) init()
    else Lampa.Listener.follow('app', function(e){
        if(e.type === 'ready') init()
    })
})()
