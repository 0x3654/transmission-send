/*
    Top — плагин Lampa (lampa.mx)

    Три пункта в меню:
      • «Топ»           — популярное/лучшее по TMDB (тренды за день/неделю, топ по рейтингу,
                          новинки); переключение варианта — кнопка вправо
      • «Топ трекеров»  — топ раздач NNMClub/RUTOR по сидам или завершённости,
                          обогащённый постерами TMDB; сервер tracker-top (tracker-top/ в репо)
      • «Мой фильтр»    — последний применённый фильтр каталога (Lampa их не помнит —
                          мы запоминаем на событии activity и открываем одним нажатием)

    Настройки: Настройки → Топ — адрес сервера, «Топ» вместо главной, минимальное
    качество, скрытие CAM/TS-раздач, только дубляж.

    Установка: Настройки → Расширения → «+» → URL этого файла.
*/

(function(){
    'use strict'

    var FLAG = '__lampa_top'

    if(window[FLAG]) return
    window[FLAG] = true

    // сколько фильмов обогащать запросами к TMDB (лимит на экран)
    var MATCH_LIMIT = 60

    function init(){
        var Lampa = window.Lampa

        function T(name){
            return Lampa.Lang.translate('top_' + name)
        }

        //---------- варианты «Топа» (TMDB)

        var VARIANTS = [
            { title: 'Фильмы · за неделю',        method: 'trending/movie/week' },
            { title: 'Фильмы · за день',          method: 'trending/movie/day' },
            { title: 'Сериалы · за неделю',       method: 'trending/tv/week' },
            { title: 'Сериалы · за день',         method: 'trending/tv/day' },
            { title: 'Фильмы · лучшее',           method: 'discover/movie', params: { sort_by: 'vote_average.desc', 'vote_count.gte': 2000 } },
            { title: 'Сериалы · лучшее',          method: 'discover/tv',    params: { sort_by: 'vote_average.desc', 'vote_count.gte': 1500 } },
            { title: 'Фильмы · новинки 2025+',    method: 'discover/movie', params: { sort_by: 'popularity.desc', 'primary_release_date.gte': '2025-01-01', 'vote_count.gte': 100 } },
            { title: 'Сериалы · новинки 2025+',   method: 'discover/tv',    params: { sort_by: 'popularity.desc', 'first_air_date.gte': '2025-01-01', 'vote_count.gte': 30 } }
        ]

        function lastVariantIndex(){
            var idx = parseInt(Lampa.Storage.get('top_last_variant', '0'), 10)

            return VARIANTS[idx] ? idx : 0
        }

        function pushVariant(v){
            Lampa.Storage.set('top_last_variant', String(VARIANTS.indexOf(v)))

            Lampa.Activity.push({
                url: '',
                title: v.title,
                component: 'top_screen',
                page: 1,
                top_method: v.method,
                top_params: v.params || null
            })
        }

        //---------- экран «Топ» (TMDB)

        function TopScreen(object){
            var comp = new Lampa.InteractionCategory(object)

            function load(page, ok, fail){
                var params = { page: page }

                if(object.top_params){
                    for(var key in object.top_params) params[key] = object.top_params[key]
                }

                Lampa.Api.sources.tmdb.get(object.top_method, params, ok, fail)
            }

            comp.create = function(){
                load(object.page || 1, this.build.bind(this), this.empty.bind(this))
            }

            comp.nextPageReuest = function(obj, resolve, reject){
                load(obj.page, resolve.bind(comp), reject.bind(comp))
            }

            comp.onRight = function(){
                Lampa.Select.show({
                    title: T('variants'),
                    items: VARIANTS.map(function(v){
                        return { title: v.title, variant: v }
                    }),
                    onSelect: function(item){
                        Lampa.Select.close()
                        pushVariant(item.variant)
                    },
                    onBack: function(){
                        Lampa.Controller.toggle('content')
                    }
                })
            }

            return comp
        }

        //---------- экран «Топ трекеров»

        function serverUrl(){
            var url = (Lampa.Storage.field('top_server_url') || '').trim()

            if(url && !/^https?:\/\//i.test(url)) url = 'http://' + url

            return url.replace(/\/+$/, '')
        }

        // строка запроса из настроек плагина
        function trackersQuery(sort){
            var minqMap = { '720p+': '720', '1080p+': '1080', '2160p+': '2160' }
            var minq    = minqMap[Lampa.Storage.field('top_min_quality')] || ''
            var junk    = Lampa.Storage.field('top_no_cam') === false ? '0' : '1'
            var audio   = Lampa.Storage.field('top_dub_only') === true ? 'dub' : 'all'
            var pages   = sort === 'top' ? 6 : 2 // классика меняется редко — копаем глубже

            return '/top?cat=video&pages=' + pages +
                '&sort=' + (sort || 'seeds') +
                (minq ? '&minq=' + minq : '') +
                '&junk=' + junk +
                '&audio=' + audio
        }

        function normTitle(s){
            return (s || '').toLowerCase()
                .replace(/[«»"'`!?:.,()\[\]{}–—|]/g, ' ')
                .replace(/\s+/g, ' ')
                .trim()
        }

        // выбрать из результатов TMDB лучший матч для фильма
        function pickBest(results, item){
            var best = null, bestScore = -1

            for(var i = 0; i < results.length; i++){
                var r = results[i]

                if(r.media_type !== 'movie' && r.media_type !== 'tv') continue

                var year = parseInt(((r.release_date || r.first_air_date || '') + '').slice(0, 4), 10) || 0
                var dy   = item.year && year ? Math.abs(year - item.year) : -1

                // фильм не того года — мимо; сезонная раздача с большим допуском
                if(item.year && year && !item.season && dy > 1) continue

                var rTitle = normTitle(r.title || r.name || '')
                var oTitle = normTitle(item.orig)
                var ruTitle = normTitle(item.ru)

                var score = 0

                if(oTitle && rTitle === oTitle) score += 50
                else if(ruTitle && rTitle === ruTitle) score += 40
                else if((oTitle && (rTitle.indexOf(oTitle) === 0 || oTitle.indexOf(rTitle) === 0)) ||
                        (ruTitle && (rTitle.indexOf(ruTitle) === 0 || ruTitle.indexOf(rTitle) === 0))) score += 25

                if(dy === 0) score += 30
                else if(dy === 1) score += 20
                else if(dy > 1 && dy <= 6) score += 5

                score += Math.min(20, Math.round(r.popularity || 0))

                if(score > bestScore){
                    bestScore = score
                    best = r
                }
            }

            // слабый матч считаем промахом
            return bestScore >= 25 ? best : null
        }

        function matchOne(item, cb){
            var query = item.orig || item.ru || ''

            if(!query) return cb(null)

            Lampa.Api.sources.tmdb.get('search/multi', { query: query }, function(json){
                cb(pickBest(json.results || [], item))
            }, function(){
                cb(null)
            }, 60 * 60 * 24)
        }

        // map с ограничением параллельности, порядок сохраняем
        function mapLimit(arr, limit, fn, done){
            var out = new Array(arr.length)
            var i = 0, active = 0, finished = 0

            function next(){
                while(active < limit && i < arr.length){
                    (function(idx){
                        active++

                        fn(arr[idx], function(res){
                            out[idx] = res
                            active--
                            finished++

                            if(finished === arr.length) done(out)
                            else next()
                        })
                    })(i++)
                }
            }

            next()
        }

        function TrackersScreen(object){
            var comp = new Lampa.InteractionCategory(object)
            var net = new Lampa.Reguest()
            var sort = object.top_sort || 'seeds'

            comp.create = function(){
                var base = serverUrl()

                if(!base){
                    Lampa.Noty.show(T('need_server'), { time: 6000 })
                    Lampa.Settings.create('top')

                    this.activity.loader(false)
                    this.activity.toggle()

                    return
                }

                this.activity.loader(true)

                net.timeout(15000)

                net.silent(base + trackersQuery(sort), function(json){
                    var items = (json && json.items) || []

                    mapLimit(items.slice(0, MATCH_LIMIT), 4, matchOne, function(matched){
                        var results = []
                        var seen = {}

                        for(var i = 0; i < matched.length; i++){
                            var el = matched[i]

                            if(!el) continue

                            var key = el.media_type + ':' + el.id

                            if(seen[key]) continue // сервер уже схлопнул раздачи — первый и есть топовый
                            seen[key] = true

                            el.source = 'tmdb'
                            el.top = items[i]

                            results.push(el)
                        }

                        if(!results.length) comp.empty()
                        else{
                            comp.build({ results: results, total_pages: 1 })

                            Lampa.Noty.show(T('trackers_matched') + ' ' + results.length + '/' + items.length)
                        }
                    })
                }, function(){
                    Lampa.Noty.show(T('server_fail'), { style: 'error' })

                    comp.empty()
                })
            }

            comp.onRight = function(){
                Lampa.Select.show({
                    title: T('trackers_sort'),
                    items: [
                        { title: T('sort_seeds'), sort: 'seeds' },
                        { title: T('sort_top'),   sort: 'top' }
                    ],
                    onSelect: function(item){
                        Lampa.Select.close()

                        Lampa.Activity.push({
                            url: '',
                            title: T('menu_trackers') + ' · ' + item.title,
                            component: 'top_trackers',
                            page: 1,
                            top_sort: item.sort
                        })
                    },
                    onBack: function(){
                        Lampa.Controller.toggle('content')
                    }
                })
            }

            return comp
        }

        //---------- «Мой фильтр»: Lampa не помнит применённый фильтр каталога,
        //---------- запоминаем на событии activity и открываем одним нажатием

        Lampa.Listener.follow('activity', function(e){
            if(e.type !== 'create' || e.component !== 'category_full' || !e.object) return

            var url = e.object.url || ''

            if(url.indexOf('discover/') === 0){
                Lampa.Storage.set('top_last_filter', {
                    url: url,
                    source: e.object.source || 'tmdb'
                })
            }
        })

        function openMyFilter(){
            var saved = Lampa.Storage.get('top_last_filter', null)

            if(!saved || !saved.url){
                Lampa.Noty.show(T('my_filter_empty'), { time: 6000 })

                return
            }

            Lampa.Activity.push({
                url: saved.url,
                title: T('menu_myfilter'),
                component: 'category_full',
                source: saved.source,
                card_type: true,
                page: 1
            })
        }

        //---------- регистрация экранов

        if(!Lampa.Component.get('top_screen')) Lampa.Component.add('top_screen', TopScreen)
        if(!Lampa.Component.get('top_trackers')) Lampa.Component.add('top_trackers', TrackersScreen)

        //---------- меню

        var ico_top = '<svg viewBox="0 0 36 36" fill="none" stroke="white" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">' +
            '<line x1="7" y1="21" x2="7" y2="31"/><line x1="18" y1="9" x2="18" y2="31"/><line x1="29" y1="15" x2="29" y2="31"/>' +
            '</svg>'

        var ico_trackers = '<svg viewBox="0 0 36 36" fill="none" stroke="white" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">' +
            '<path d="M8 8 v10 a10 10 0 0 0 20 0 v-10"/><line x1="8" y1="8" x2="8" y2="13"/><line x1="28" y1="8" x2="28" y2="13"/><line x1="13" y1="8" x2="13" y2="11"/><line x1="23" y1="8" x2="23" y2="11"/>' +
            '</svg>'

        var ico_filter = '<svg viewBox="0 0 36 36" fill="none" stroke="white" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">' +
            '<path d="M6 9 h24 l-9 11 v9 l-6 -3 v-6 z"/>' +
            '</svg>'

        Lampa.Menu.addButton(ico_top, T('menu_top'), function(){
            pushVariant(VARIANTS[lastVariantIndex()])
        })

        Lampa.Menu.addButton(ico_trackers, T('menu_trackers'), function(){
            Lampa.Activity.push({
                url: '',
                title: T('menu_trackers'),
                component: 'top_trackers',
                page: 1
            })
        })

        Lampa.Menu.addButton(ico_filter, T('menu_myfilter'), openMyFilter)

        //---------- «Топ» вместо главной

        if(Lampa.Storage.field('top_as_home') === true){
            try{
                Lampa.Activity.replace({
                    url: '',
                    title: VARIANTS[lastVariantIndex()].title,
                    component: 'top_screen',
                    page: 1,
                    top_method: VARIANTS[lastVariantIndex()].method,
                    top_params: VARIANTS[lastVariantIndex()].params || null
                })
            }
            catch(e){}
        }

        //---------- настройки

        Lampa.SettingsApi.addComponent({
            component: 'top',
            icon: ico_top,
            name: T('settings_name')
        })

        Lampa.SettingsApi.addParam({
            component: 'top',
            param: {
                name: 'top_server_url',
                type: 'input',
                default: '',
                placeholder: 'http://192.168.1.2:8355'
            },
            field: {
                name: T('settings_server'),
                description: T('settings_server_desc')
            }
        })

        Lampa.SettingsApi.addParam({
            component: 'top',
            param: {
                name: 'top_as_home',
                type: 'trigger',
                default: false
            },
            field: {
                name: T('settings_as_home'),
                description: T('settings_as_home_desc')
            }
        })

        Lampa.SettingsApi.addParam({
            component: 'top',
            param: {
                name: 'top_min_quality',
                type: 'select',
                values: ['Любое', '720p+', '1080p+', '2160p+'],
                default: 'Любое'
            },
            field: {
                name: T('settings_min_quality')
            }
        })

        Lampa.SettingsApi.addParam({
            component: 'top',
            param: {
                name: 'top_no_cam',
                type: 'trigger',
                default: true
            },
            field: {
                name: T('settings_no_cam'),
                description: T('settings_no_cam_desc')
            }
        })

        Lampa.SettingsApi.addParam({
            component: 'top',
            param: {
                name: 'top_dub_only',
                type: 'trigger',
                default: false
            },
            field: {
                name: T('settings_dub_only')
            }
        })

        //---------- словарь

        Lampa.Lang.add({
            top_menu_top:          { ru: 'Топ',                    en: 'Top' },
            top_menu_trackers:     { ru: 'Топ трекеров',           en: 'Tracker top' },
            top_menu_myfilter:     { ru: 'Мой фильтр',             en: 'My filter' },
            top_variants:          { ru: 'Что показать',           en: 'What to show' },
            top_trackers_sort:     { ru: 'Сортировка топа',        en: 'Top sorting' },
            top_sort_seeds:        { ru: 'По сидам · сейчас',      en: 'By seeders · now' },
            top_sort_top:          { ru: 'Классика · за всё время (NNM)', en: 'All-time classics (NNM)' },
            top_need_server:       { ru: 'Укажите адрес сервера tracker-top в настройках', en: 'Set tracker-top server address in settings' },
            top_server_fail:       { ru: 'Сервер топа недоступен', en: 'Top server unreachable' },
            top_trackers_matched:  { ru: 'Совпало с TMDB:',        en: 'Matched on TMDB:' },
            top_my_filter_empty:   { ru: 'Примените фильтр в разделе «Фильтр» — я его запомню', en: 'Apply a filter in the Filter section — I will remember it' },
            top_settings_name:     { ru: 'Топ',                    en: 'Top' },
            top_settings_server:   { ru: 'Адрес сервера топа',     en: 'Top server address' },
            top_settings_server_desc: { ru: 'tracker-top: https://… (см. репо); сейчас micro-tracker.koi-uaru.ts.net', en: 'tracker-top: https://… (see repo)' },
            top_settings_as_home:  { ru: '«Топ» вместо главной',   en: 'Top as home screen' },
            top_settings_as_home_desc: { ru: 'при запуске открывается последний вариант «Топа»', en: 'open last used Top variant on start' },
            top_settings_min_quality: { ru: 'Мин. качество (трекеры)', en: 'Min quality (trackers)' },
            top_settings_no_cam:   { ru: 'Скрывать CAM/TS',        en: 'Hide CAM/TS' },
            top_settings_no_cam_desc: { ru: 'камрипы и «звук с TS» не попадают в топ; фильмы только с такими раздачами скрываются целиком', en: 'camrips and TS-sound stay out; films with only such releases are hidden' },
            top_settings_dub_only: { ru: 'Только дубляж (трекеры)', en: 'Dub only (trackers)' }
        })
    }

    if(window.appready) init()
    else Lampa.Listener.follow('app', function(e){
        if(e.type === 'ready') init()
    })
})()
