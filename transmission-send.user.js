// ==UserScript==
// @name         Lampa → Transmission
// @namespace    https://github.com/0x3654/transmission-send
// @version      1.1.0
// @description  Прямая отправка торрентов из Lampa (lampa.mx) в Transmission: долгое нажатие на раздаче → «Добавить в Transmission». В обход CORS через GM.xmlHttpRequest. Пункты «Скачать .torrent» и «Скопировать магнет» работают и без RPC.
// @author       0x3654
// @match        https://lampa.mx/*
// @grant        GM.xmlHttpRequest
// @connect      micro-transmission.koi-uaru.ts.net
// @connect      *
// @run-at       document-idle
// @noframes
// ==/UserScript==
/*
    Transmission Send — плагин Lampa (lampa.mx) и userscript (dual-mode)

    Добавляет в меню долгого нажатия на торренте:
      • «Скачать .torrent»        — браузерная загрузка файла (iCloud/Downloads + folder action)
      • «Скопировать магнет»      — буфер обмена (вставить в NASctl / Transmission Remote GUI)
      • «Добавить в Transmission» — RPC torrent-add с 409-handshake:
            - как userscript (Tampermonkey / Userscripts): GM.xmlHttpRequest — в обход CORS,
              работает с любым Transmission и http-адресами;
            - как плагин Lampa: обычный fetch — нужен https-URL и Transmission ≤ 4.1.2
              (в 4.1.3+ CORS удалён, браузер заблокирует запрос с именованной ошибкой).
      • «Открыть магнет»           — системный обработчик (только на Mac)

    Установка как плагин Lampa: Настройки → Расширения → «+» → URL этого файла.
    Установка как userscript:  см. transmission-send.user.js (генерируется make-userjs.sh).
*/

(function(){
    'use strict'

    var FLAG = '__lampa_transmission_send'

    // В песочнице userscript-менеджера (Tampermonkey @grant) страница доступна через
    // unsafeWindow; в контексте плагина Lampa unsafeWindow нет и PAGE === window.
    var PAGE = typeof unsafeWindow !== 'undefined' && unsafeWindow ? unsafeWindow : window

    if(PAGE[FLAG]) return
    PAGE[FLAG] = true

    function hasGM(){
        return typeof GM !== 'undefined' && GM.xmlHttpRequest
    }

    function boot(attempt){
        if(!PAGE.Lampa){
            if(attempt < 200) return setTimeout(function(){ boot(attempt + 1) }, 50)

            return console.error('[transmission-send] Lampa не обнаружена на странице')
        }

        if(PAGE.appready) return init()

        PAGE.Lampa.Listener.follow('app', function(e){
            if(e.type === 'ready') init()
        })
    }

    function init(){
        var Lampa = PAGE.Lampa

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
            var match = normalizeRpcUrl(cfg('transmission_rpc_url', '')).match(/^https?:\/\/([^/]+)/)

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

            if(PAGE.navigator.clipboard && PAGE.navigator.clipboard.writeText){
                PAGE.navigator.clipboard.writeText(magnet).then(ok, function(){
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
            PAGE.location = magnet
        }

        function canShare(){
            return !Lampa.Platform.is('apple_tv') && PAGE.navigator.share
        }

        function shareMagnet(magnet){
            PAGE.navigator.share({ text: magnet }).then(function(){
                noty(T('shared'), 'success')
            }, function(e){
                if(e && e.name === 'AbortError') return

                noty(T('share_fail'), 'error')
            })
        }

        //---------- HTTP-транспорт: GM.xmlHttpRequest (в обход CORS) или fetch

        function httpPost(url, headers, data, cb){
            if(hasGM()){
                try{
                    GM.xmlHttpRequest({
                        method: 'POST',
                        url: url,
                        headers: headers,
                        data: data,
                        onload: function(r){
                            cb(null, {
                                status: r.status,
                                text: r.responseText,
                                header: function(name){
                                    var lines = (r.responseHeaders || '').split('\n')

                                    for(var i = 0; i < lines.length; i++){
                                        var m = lines[i].match(/^([^:]+):\s*(.*)$/)

                                        if(m && m[1].trim().toLowerCase() === name.toLowerCase()) return m[2].trim()
                                    }

                                    return null
                                }
                            })
                        },
                        onerror: function(e){ cb(e || {}) },
                        ontimeout: function(){ cb({}) }
                    })
                }
                catch(e){ cb(e) }
            }
            else if(typeof fetch === 'function'){
                fetch(url, {
                    method: 'POST',
                    headers: headers,
                    body: data
                }).then(function(response){
                    return response.text().then(function(text){
                        cb(null, {
                            status: response.status,
                            text: text,
                            header: function(name){ return response.headers.get(name) }
                        })
                    })
                }, function(e){ cb(e) })
            }
            else cb({ message: 'no transport' })
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

            if(!url) return fail(T('err_nourl'), true)

            // mixed content актуален только для fetch; GM-транспорт ходит напрямую
            if(!hasGM() && PAGE.location.protocol === 'https:' && url.indexOf('http://') === 0){
                return fail(T('err_mixed'), true)
            }

            var headers = rpcHeaders()
            var payload = JSON.stringify(body)

            function send(session){
                if(session) headers['X-Transmission-Session-Id'] = session

                httpPost(url, headers, payload, function(err, res){
                    if(err) return fail(T('err_network'), true)

                    if(res.status === 409){
                        var sid = res.header('X-Transmission-Session-Id')

                        if(!sid) return fail(T('err_cors_header'), true)

                        return send(sid)
                    }

                    var json

                    try{ json = JSON.parse(res.text) }
                    catch(e){ return fail('Transmission: HTTP ' + res.status, false) }

                    if(json.result === 'success')                done(json)
                    else if(json.result === 'duplicate torrent') fail(T('dup'), false)
                    else                                         fail('Transmission: ' + json.result, false)
                })
            }

            send()
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

            if(!Lampa.Platform.is('apple_tv')){
                menu.push({
                    title: T('menu_copy'),
                    onSelect: wrap(prev, function(){ copyMagnet(magnet) })
                })

                if(canShare()){
                    menu.push({
                        title: T('menu_share'),
                        onSelect: wrap(prev, function(){ shareMagnet(magnet) })
                    })
                }

                menu.push({
                    title: T('menu_open'),
                    onSelect: wrap(prev, function(){ openMagnet(magnet) })
                })
            }

            menu.push({
                title: T('menu_add'),
                subtitle: rpcHost() || T('subtitle_no_rpc'),
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

            function input(name, placeholder, field_name, field_descr){
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
                transmission_send_menu_share:       { ru: 'Поделиться магнетом', en: 'Share magnet' },

                transmission_send_shared:           { ru: 'Магнет отправлен',    en: 'Magnet shared' },
                transmission_send_share_fail:       { ru: 'Не удалось поделиться', en: 'Share failed' },
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

                transmission_send_settings_url:            { ru: 'Адрес RPC', en: 'RPC URL' },
                transmission_send_settings_url_descr:      { ru: 'https://хост:9091 — или любой, если установлен как userscript', en: 'https://host:9091 — or any, when installed as a userscript' },
                transmission_send_settings_user:           { ru: 'Логин', en: 'User' },
                transmission_send_settings_password:       { ru: 'Пароль', en: 'Password' },
                transmission_send_settings_password_descr: { ru: 'Хранится в localStorage устройства в открытом виде', en: 'Stored in device localStorage as plain text' },
                transmission_send_settings_labels:         { ru: 'Метка (labels)', en: 'Label (labels)' },
                transmission_send_settings_labels_descr:   { ru: 'Через запятую, пусто = без меток (Transmission 4+)', en: 'Comma-separated, empty = no labels (Transmission 4+)' },
                transmission_send_settings_test:           { ru: 'Проверить соединение', en: 'Test connection' }
            })
        }

        addLangs()
        addSettings()
        addHooks()
    }

    boot(0)
})()
