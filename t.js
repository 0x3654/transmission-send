/*
    t — bootstrap-плагин Lampa (lampa.mx)

    Короткий адрес для ввода с пульта (user-site GitHub Pages, корень):
    https://0x3654.github.io/t.js

      • доустанавливает плагины (top, transmission-send) — при каждом
        старте, если плагина нет в списке; отключённые (выключенные)
        не трогает; чтобы плагин не вернулся — удалите сам t.js
      • один раз применяет настройки из CONFIG ниже (сервер топа,
        фильтры, TorrServer) и перезагружает приложение
      • повторно настройки применяются только после повышения VERSION —
        поправили CONFIG → подняли версию → при следующем запуске
        настройки перезапишутся заново (руками сделанное затрётся)
      • TorrServer: основная ссылка — сервер на micro, дополнительная —
        встроенный (http://127.0.0.1:8090). Активная ссылка выбирается
        пробой при каждом запуске: локальный TorrServer отвечает —
        «дополнительная», нет — «основная». Сам автозапуск встроенного
        TorrServer плагином не включается (нет моста в нативное меню) —
        один раз включите руками: Настройки → Настройки (внизу списка) →
        TorrServer/автозапуск. Ручной выбор ссылки уважается: изменили
        «Использовать ссылку» сами — bootstrap больше её не трогает
        (до повышения VERSION)

    Установка: Настройки → Расширения → «+» → URL этого файла.

    Копия для длинного URL живёт в репо transmission-send (t.js) —
    при правке CONFIG синхронизировать оба файла.
*/

(function(){
    'use strict'

    var FLAG = '__lampa_boot'

    if(window[FLAG]) return
    window[FLAG] = true

    // поднять после правки CONFIG — настройки применятся заново
    var VERSION = '2'

    var CONFIG = {
        plugins: [
            'https://0x3654.github.io/transmission-send/top.js',
            'https://0x3654.github.io/transmission-send/transmission-send.js'
            // 'https://0x3654.github.io/transmission-send/plex-sync.js'
        ],

        storage: {
            // сервер «Топа · трекеров» (tracker-top на micro, tsdproxy)
            top_server_url: 'https://micro-tracker.koi-uaru.ts.net',

            // TorrServer: основная — micro (tsdproxy), дополнительная —
            // встроенный TorrServer приложения (Apple TV/Android/macOS)
            torrserver_url: 'https://torrserver.koi-uaru.ts.net',
            torrserver_url_two: 'http://127.0.0.1:8090',

            // фильтры топа (значения — как в настройках плагина top)
            top_min_quality:  '1080',
            top_voice_1:      'Дубляж',
            top_voice_2:      'LostFilm',
            top_trackers_sort: 'seeds',
            top_hide_watched: 'true',
            top_trackers_only: 'true',
            top_ru_titles:    'true',
            top_no_cam:       'true',
            top_hide_series:  'false',

            // «Топ» вместо главной
            top_as_home: 'true'
        }
    }

    // отвечает ли локальный TorrServer (встроенный в приложение)
    function probeLocal(cb){
        var tries = 2

        ;(function attempt(){
            var xhr = new XMLHttpRequest()
            var done = false

            function finish(alive){
                if(done) return
                done = true

                if(alive || !--tries) cb(alive)
                else setTimeout(attempt, 400)
            }

            xhr.open('HEAD', 'http://127.0.0.1:8090', true)
            xhr.timeout = 1200
            xhr.onload = function(){ finish(xhr.status > 0) }
            xhr.onerror = xhr.ontimeout = function(){ finish(false) }

            try{ xhr.send() }
            catch(e){ finish(false) }
        })()
    }

    // «использовать ссылку»: два — локальный TorrServer жив, один — micro;
    // пробуем только там, где локальный вообще бывает, на остальном — «один»
    function pickLink(cb){
        var Lampa = window.Lampa
        var local = false

        try{
            local = Lampa.Platform.is('apple_tv') || Lampa.Platform.is('android') || Lampa.Platform.macOS()
        }
        catch(e){}

        if(!local) return cb('one')

        probeLocal(function(alive){ cb(alive ? 'two' : 'one') })
    }

    function init(){
        var Lampa = window.Lampa

        // имя в списке расширений (лампа берёт имя только из каталога cub)
        ;(function selfName(){
            try{
                var list = Lampa.Plugins.get()

                for(var i = 0; i < list.length; i++){
                    if((list[i].url || '').indexOf('/t.js') > -1){
                        list[i].name   = 'Запуск — настройка лампы'
                        list[i].author = '@0x3654'
                        list[i].descr  = 'Ставит плагины и применяет настройки (bootstrap)'
                    }
                }

                Lampa.Plugins.save()
            }
            catch(e){}
        })()

        // доустановить недостающие плагины
        var installed = Lampa.Plugins.get().map(function(p){ return p.url })

        CONFIG.plugins.forEach(function(url){
            if(installed.indexOf(url) === -1)
                Lampa.Plugins.add({ url: url, status: 1, author: '@0x3654' })
        })

        // настройки — только при первом запуске (или после повышения VERSION)
        var MARKER = 'boot_ver'
        var LINK   = 'torrserver_use_link'
        var MEM    = 'boot_link_written'
        var reload = false

        try{
            if(Lampa.Storage.get(MARKER) !== VERSION){
                for(var key in CONFIG.storage) Lampa.Storage.set(key, CONFIG.storage[key])

                Lampa.Storage.set(MARKER, VERSION)
                Lampa.Storage.set(MEM, '') // вернуться к авто-выбору ссылки

                reload = true
            }
        }
        catch(e){ reload = true }

        // активная ссылка TorrServer — пробой при каждом запуске;
        // значение, изменённое не нами, трогаем только после VERSION
        var cur     = String(Lampa.Storage.get(LINK) || '')
        var written = String(Lampa.Storage.get(MEM) || '')

        function done(){
            if(reload) setTimeout(function(){ window.location.reload() }, 700)
        }

        if(!written || cur === written){
            pickLink(function(link){
                if(link !== cur || !written){
                    try{
                        Lampa.Storage.set(LINK, link)
                        Lampa.Storage.set(MEM, link)

                        reload = true
                    }
                    catch(e){}
                }

                done()
            })
        }
        else done()
    }

    if(window.appready) init()
    else Lampa.Listener.follow('app', function(e){
        if(e.type === 'ready') init()
    })
})()
