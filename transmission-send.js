/*
    Transmission Send — плагин Lampa (lampa.mx)

    Меню долгого нажатия на торренте:
      • «Скопировать магнет»  — буфер обмена (вставить в NASctl / Transmission Remote GUI)
      • «Открыть магнет»      — системный обработчик схемы magnet:

    Установка: Настройки → Расширения → «+» → URL этого файла.
*/

(function(){
    'use strict'

    var FLAG = '__lampa_transmission_send'

    if(window[FLAG]) return
    window[FLAG] = true

    function init(){
        var Lampa = window.Lampa

        // Lampa не умеет брать имя/описание из кода плагина — только из каталога cub.
        ;(function selfName(){
            try{
                var url   = 'https://0x3654.github.io/transmission-send/transmission-send.js'
                var list  = Lampa.Plugins.get()
                var named = false

                for(var i = 0; i < list.length; i++){
                    if((list[i].url || '') === url && list[i].name !== 'Transmission Send'){
                        list[i].name   = 'Transmission Send'
                        list[i].author = '@0x3654'
                        list[i].descr  = 'Магнет из меню долгого нажатия: копировать или открыть системным обработчиком'
                        named = true
                    }
                }

                if(named) Lampa.Plugins.save()
            }
            catch(e){}
        })()


        //---------- словарь

        Lampa.Lang.add({
            transmission_send_menu_copy:        { ru: 'Скопировать магнет', en: 'Copy magnet' },
            transmission_send_menu_open:        { ru: 'Открыть магнет',     en: 'Open magnet' },

            transmission_send_copied:           { ru: 'Магнет скопирован',  en: 'Magnet copied' },
            transmission_send_copy_fail:        { ru: 'Не удалось скопировать', en: 'Copy failed' },
            transmission_send_magnet_open:      { ru: 'Открываю магнет…',   en: 'Opening magnet…' }
        })

        function T(name){
            return Lampa.Lang.translate('transmission_send_' + name)
        }

        function magnetOf(el){
            if(!el) return ''

            if(el.MagnetUri) return el.MagnetUri

            return /^magnet:/i.test(el.Link || '') ? el.Link : ''
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

        function openMagnet(magnet){
            noty(T('magnet_open'))
            window.location = magnet
        }

        //---------- меню

        function wrap(prev, fn){
            return function(element, item){
                fn(element, item)
                Lampa.Controller.toggle(prev)
            }
        }

        function pushMagnetItems(menu, prev, magnet){
            if(!magnet) return

            menu.push({
                title: T('menu_copy'),
                onSelect: wrap(prev, function(){ copyMagnet(magnet) })
            })

            // всегда последним пунктом
            menu.push({
                title: T('menu_open'),
                onSelect: wrap(prev, function(){ openMagnet(magnet) })
            })
        }

        //---------- хуки

        // список раздач
        Lampa.Listener.follow('torrent', function(e){
            if(e.type !== 'onlong' || !e.menu || !e.element) return

            var prev = Lampa.Controller.enabled().name

            pushMagnetItems(e.menu, prev, magnetOf(e.element))
        })

        // список файлов внутри торрента (магнет восстанавливаем из info-hash)
        Lampa.Listener.follow('torrent_file', function(e){
            if(e.type !== 'onlong' || !e.menu || !e.element || !e.element.torrent_hash) return

            var prev = Lampa.Controller.enabled().name

            pushMagnetItems(e.menu, prev, 'magnet:?xt=urn:btih:' + e.element.torrent_hash)
        })

    }

    if(window.appready) init()
    else Lampa.Listener.follow('app', function(e){
        if(e.type === 'ready') init()
    })
})()
