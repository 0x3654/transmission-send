/*
    t — bootstrap-плагин Lampa (lampa.mx)

    Короткий URL для чистой лампы (вводится с пульта один раз):
    https://0x3654.github.io/transmission-send/t.js

      • доустанавливает плагины (top, transmission-send) — при каждом
        старте, если плагина нет в списке; отключённые (выключенные)
        не трогает; чтобы плагин не вернулся — удалите сам t.js
      • один раз применяет настройки из CONFIG ниже (сервер топа,
        фильтры, TorrServer) и перезагружает приложение
      • повторно настройки применяются только после повышения VERSION —
        поправили CONFIG → подняли версию → при следующем запуске
        настройки перезапишутся заново (руками сделанное затрётся)

    Установка: Настройки → Расширения → «+» → URL этого файла.
*/

(function(){
    'use strict'

    var FLAG = '__lampa_boot'

    if(window[FLAG]) return
    window[FLAG] = true

    // поднять после правки CONFIG — настройки применятся заново
    var VERSION = '1'

    var CONFIG = {
        plugins: [
            'https://0x3654.github.io/transmission-send/top.js',
            'https://0x3654.github.io/transmission-send/transmission-send.js'
            // 'https://0x3654.github.io/transmission-send/plex-sync.js'
        ],

        storage: {
            // сервер «Топа · трекеров» (tracker-top на micro, tsdproxy)
            top_server_url: 'https://micro-tracker.koi-uaru.ts.net',

            // TorrServer на micro (tsdproxy);
            // встроенный в tvOS-приложение TorrServer — 'http://127.0.0.1:8090'
            torrserver_url: 'https://torrserver.koi-uaru.ts.net',

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
        var applied = false

        try{ applied = Lampa.Storage.get(MARKER) === VERSION }
        catch(e){}

        if(!applied){
            for(var key in CONFIG.storage) Lampa.Storage.set(key, CONFIG.storage[key])

            Lampa.Storage.set(MARKER, VERSION)

            setTimeout(function(){ window.location.reload() }, 700)
        }
    }

    if(window.appready) init()
    else Lampa.Listener.follow('app', function(e){
        if(e.type === 'ready') init()
    })
})()
