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

        // Lampa не умеет брать имя/описание из кода плагина — только из каталога cub.
        // Дописываем сами в свою запись списка расширений.
        ;(function selfName(){
            try{
                var url    = 'https://0x3654.github.io/transmission-send/top.js'
                var list   = Lampa.Plugins.get()
                var named  = false

                for(var i = 0; i < list.length; i++){
                    if((list[i].url || '') === url && list[i].name !== 'Топ — топы и фильтры'){
                        list[i].name   = 'Топ — топы и фильтры'
                        list[i].author = '@0x3654'
                        list[i].descr  = 'Топ TMDB, топ трекеров NNMClub/RUTOR, «Мой фильтр»'
                        named = true
                    }
                }

                if(named) Lampa.Plugins.save()
            }
            catch(e){}
        })()


        //---------- словарь

        Lampa.Lang.add({
            top_menu_top:          { ru: 'Топ · TMDB',             en: 'Top · TMDB' },
            top_menu_trackers:     { ru: 'Топ · трекеры',          en: 'Top · trackers' },
            top_variants:          { ru: 'Что показать',           en: 'What to show' },
            top_trackers_sort:     { ru: 'Сортировка топа',        en: 'Top sorting' },
            top_sort_seeds:        { ru: 'По сидам · сейчас',      en: 'By seeders · now' },
            top_sort_top:          { ru: 'Классика · за всё время (NNM)', en: 'All-time classics (NNM)' },
            top_need_server:       { ru: 'Укажите адрес сервера tracker-top в настройках', en: 'Set tracker-top server address in settings' },
            top_server_fail:       { ru: 'Сервер топа недоступен', en: 'Top server unreachable' },
            top_trackers_matched:  { ru: 'Совпало с TMDB:',        en: 'Matched on TMDB:' },
            top_settings_name:     { ru: 'Топ',                    en: 'Top' },
            top_settings_server:   { ru: 'Адрес сервера топа',     en: 'Top server address' },
            top_settings_server_desc: { ru: 'tracker-top: https://… (см. репо); сейчас micro-tracker.koi-uaru.ts.net', en: 'tracker-top: https://… (see repo)' },
            top_settings_as_home:  { ru: '«Топ» вместо главной',   en: 'Top as home screen' },
            top_settings_as_home_desc: { ru: 'при запуске открывается последний вариант «Топа»', en: 'open last used Top variant on start' },
            top_settings_min_quality: { ru: 'Мин. качество (трекеры)', en: 'Min quality (trackers)' },
            top_settings_no_cam:   { ru: 'Скрывать CAM/TS',        en: 'Hide CAM/TS' },
            top_settings_russian_only: { ru: 'Только русские названия', en: 'Russian titles only' },
            top_settings_russian_only_desc: { ru: 'скрывать раздачи совсем без русских букв в названии', en: 'hide releases with no cyrillic in title' },
            top_settings_no_cam_desc: { ru: 'камрипы и «звук с TS» не попадают в топ; фильмы только с такими раздачами скрываются целиком', en: 'camrips and TS-sound stay out; films with only such releases are hidden' },
            top_settings_voice_1:  { ru: 'Озвучка 1 (трекеры)', en: 'Voice 1 (trackers)' },
            top_settings_voice_2:  { ru: 'Озвучка 2 (трекеры)', en: 'Voice 2 (trackers)' },
            top_filters:            { ru: 'Фильтры',                en: 'Filters' },
            top_apply:              { ru: 'Показать',               en: 'Show' },
            top_yes:                { ru: 'да',                     en: 'yes' },
            top_no:                 { ru: 'нет',                    en: 'no' },
            top_settings_hide_watched: { ru: 'Скрывать просмотренные', en: 'Hide watched' },
            top_settings_hide_watched_desc: { ru: 'только в «Топе» и «Топе трекеров», по истории Lampa', en: 'only in Top screens, uses Lampa history' },
        })

        function T(name){
            return Lampa.Lang.translate('top_' + name)
        }

        //---------- варианты «Топа» (TMDB)

        var VARIANTS = [
            { title: 'Фильмы · за неделю',        method: 'trending/movie/week' },
            { title: 'Фильмы · за день',          method: 'trending/movie/day' },
            { title: 'Фильмы · топ 14 дней',      method: 'discover/movie', windowDays: 14, dateKey: 'primary_release_date', params: { sort_by: 'popularity.desc', 'vote_count.gte': 50 } },
            { title: 'Фильмы · топ 30 дней',      method: 'discover/movie', windowDays: 30, dateKey: 'primary_release_date', params: { sort_by: 'popularity.desc', 'vote_count.gte': 50 } },
            { title: 'Сериалы · за неделю',       method: 'trending/tv/week' },
            { title: 'Сериалы · за день',         method: 'trending/tv/day' },
            { title: 'Сериалы · топ 30 дней',     method: 'discover/tv',    windowDays: 30, dateKey: 'first_air_date', params: { sort_by: 'popularity.desc', 'vote_count.gte': 20 } },
            { title: 'Фильмы · лучшее',           method: 'discover/movie', params: { sort_by: 'vote_average.desc', 'vote_count.gte': 2000 } },
            { title: 'Сериалы · лучшее',          method: 'discover/tv',    params: { sort_by: 'vote_average.desc', 'vote_count.gte': 1500 } },
            { title: 'Фильмы · новинки 2025+',    method: 'discover/movie', params: { sort_by: 'popularity.desc', 'primary_release_date.gte': '2025-01-01', 'vote_count.gte': 100 } },
            { title: 'Сериалы · новинки 2025+',   method: 'discover/tv',    params: { sort_by: 'popularity.desc', 'first_air_date.gte': '2025-01-01', 'vote_count.gte': 30 } }
        ]

        //---------- лист фильтров «как в торрентах»: строки с вложенным выбором

        var QUALITY = { any: 'Любое', '720': '720p и выше', '1080': '1080p и выше (вкл. 4K)', '2160': '4K' }

        var VOICES = {
            any: 'Любая',
            'Дубляж': 'Дубляж',
            'Многоголосый': 'Многоголосый',
            'LostFilm': 'LostFilm',
            'Кубик в Кубе': 'Кубик в Кубе',
            'HDrezka Studio': 'HDrezka Studio',
            'Red Head Sound': 'Red Head Sound',
            'Jaskier': 'Jaskier',
            'NewStudio': 'NewStudio'
        }

        function field(name){
            return String(Lampa.Storage.field(name))
        }

        function yesNo(name){
            return field(name) === 'true' ? T('yes') : T('no')
        }

        // rows: {title, values, key} | {title, toggle} | {title, go} | {title, variants}
        function filterSheet(rows, apply){
            Lampa.Select.show({
                title: T('filters'),
                items: rows,
                onBack: function(){
                    Lampa.Controller.toggle('content')
                },
                onSelect: function(item){
                    Lampa.Select.close()

                    if(item.go) return apply()

                    if(item.variants){
                        Lampa.Select.show({
                            title: item.title,
                            items: item.variants.map(function(v){
                                return { title: v.title, variant: v }
                            }),
                            onBack: function(){
                                filterSheet(rows, apply)
                            },
                            onSelect: function(chosen){
                                Lampa.Select.close()
                                pushVariant(chosen.variant)
                            }
                        })

                        return
                    }

                    if(item.toggle){
                        Lampa.Storage.set(item.toggle, field(item.toggle) === 'true' ? 'false' : 'true')

                        return filterSheet(rows, apply)
                    }

                    if(item.key){
                        Lampa.Select.show({
                            title: item.title.split(':')[0],
                            items: Object.keys(item.values).map(function(k){
                                return { title: item.values[k], value: k, selected: k === field(item.key) }
                            }),
                            onBack: function(){
                                filterSheet(rows, apply)
                            },
                            onSelect: function(chosen){
                                Lampa.Select.close()
                                Lampa.Storage.set(item.key, chosen.value)

                                filterSheet(rows, apply)
                            }
                        })
                    }
                }
            })
        }

        function daysAgoISO(days){
            return new Date(Date.now() - days * 86400000).toISOString().slice(0, 10)
        }

        // параметры варианта с учётом окна дат
        function variantParams(v){
            var params = {}

            if(v.params){
                for(var key in v.params) params[key] = v.params[key]
            }
            if(v.windowDays && v.dateKey) params[v.dateKey + '.gte'] = daysAgoISO(v.windowDays)

            return params
        }

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
                top_params: variantParams(v)
            })
        }

        //---------- экран «Топ» (TMDB)

        function TopScreen(object){
            var comp = new Lampa.InteractionCategory(object)

            hideWatched(comp)

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
                var cur = VARIANTS[lastVariantIndex()]

                filterSheet([
                    {
                        title: T('variants') + ': ' + cur.title,
                        variants: VARIANTS
                    },
                    { title: T('settings_hide_watched') + ': ' + yesNo('top_hide_watched'), toggle: 'top_hide_watched' }
                ])
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
            // Lampa хранит значения строками ('true'/'false', ключи select)
            var field = function(name){ return String(Lampa.Storage.field(name)) }
            var minq  = field('top_min_quality')
            var junk  = field('top_no_cam') === 'false' ? '0' : '1'
            var pages = sort === 'top' ? 6 : 2 // классика меняется редко — копаем глубже

            if(minq === 'any' || minq === 'null' || minq === 'undefined') minq = ''

            // две озвучки на выбор (складываются в один список)
            var voices = []

            ;['top_voice_1', 'top_voice_2'].forEach(function(name){
                var v = field(name)

                if(v && v !== 'any' && voices.indexOf(v) === -1) voices.push(v)
            })

            var ru = field('top_russian_only') === 'false' ? '0' : '1'

            return '/top?cat=video&pages=' + pages +
                '&sort=' + (sort || 'seeds') +
                (minq ? '&minq=' + minq : '') +
                '&junk=' + junk +
                '&ru=' + ru +
                (voices.length ? '&voice=' + encodeURIComponent(voices.join(',')) : '')
        }

        //---------- «скрыть просмотренные»: история/просмотрено Lampa, только в наших экранах

        // «История просмотров» + «Просмотрено» (+ «Смотрю»/«Брошено» — тоже
        // «уже в орбите»). Ключей два: id и нормализованное «название|год» —
        // второй ловит расхождения типа карточки (история хранит сериал,
        // поиск сматчил фильм) и варианты названий
        function watchedSet(){
            var set = {}

            ;['history', 'viewed', 'look', 'thrown'].forEach(function(type){
                var items = []

                try{
                    items = Lampa.Favorite.get({ type: type }) || []
                }
                catch(e){}

                items.forEach(function(card){
                    if(!card || card.id == null) return

                    set[(card.name ? 'tv' : 'movie') + ':' + card.id] = true

                    var title = card.title || card.name || ''
                    var year  = ((card.release_date || card.first_air_date || '') + '').slice(0, 4)

                    if(title) set[normTitle(title) + '|' + year] = true
                })
            })

            return set
        }

        function hideWatched(comp){
            var origBuild = comp.build.bind(comp)

            comp.build = function(data){
                if(String(Lampa.Storage.field('top_hide_watched')) === 'true' && data && data.results){
                    var watched = watchedSet()

                    data.results = data.results.filter(function(el){
                        if(watched[(el.name ? 'tv' : 'movie') + ':' + el.id]) return false

                        var title = el.title || el.name || ''
                        var year  = ((el.release_date || el.first_air_date || '') + '').slice(0, 4)

                        if(title && watched[normTitle(title) + '|' + year]) return false

                        return true
                    })
                }

                return origBuild(data)
            }
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
            var sort = object.top_sort || field('top_trackers_sort') || 'seeds'

            hideWatched(comp)

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

                            // нативный бейдж качества на карточке (card__quality,
                            // настройка Lampa «Отметки качества»)
                            el.quality = { '2160': '4K', '1080': '1080p', '720': '720p', sd: 'SD' }[items[i].quality] || ''

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
                var SORTS = { seeds: T('sort_seeds'), top: T('sort_top') }

                filterSheet([
                    {
                        title: T('trackers_sort') + ': ' + (SORTS[sort] || SORTS.seeds),
                        values: SORTS,
                        key: 'top_trackers_sort'
                    },
                    { title: T('settings_min_quality') + ': ' + (QUALITY[field('top_min_quality')] || QUALITY.any), values: QUALITY, key: 'top_min_quality' },
                    { title: T('settings_voice_1') + ': ' + (VOICES[field('top_voice_1')] || VOICES.any), values: VOICES, key: 'top_voice_1' },
                    { title: T('settings_voice_2') + ': ' + (VOICES[field('top_voice_2')] || VOICES.any), values: VOICES, key: 'top_voice_2' },
                    { title: T('settings_hide_watched') + ': ' + yesNo('top_hide_watched'), toggle: 'top_hide_watched' },
                    { title: T('settings_no_cam') + ': ' + yesNo('top_no_cam'), toggle: 'top_no_cam' },
                    { title: T('settings_russian_only') + ': ' + yesNo('top_russian_only'), toggle: 'top_russian_only' },
                    { title: '↻ ' + T('apply'), go: true }
                ], function(){
                    Lampa.Storage.set('top_trackers_sort', field('top_trackers_sort') || 'seeds')

                    Lampa.Activity.push({
                        url: '',
                        title: T('menu_trackers'),
                        component: 'top_trackers',
                        page: 1,
                        top_sort: field('top_trackers_sort') || 'seeds'
                    })
                })
            }

            return comp
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



        //---------- «Топ» вместо главной

        if(String(Lampa.Storage.field('top_as_home')) === 'true'){
            var homeVariant = VARIANTS[lastVariantIndex()]

            try{
                Lampa.Activity.replace({
                    url: '',
                    title: homeVariant.title,
                    component: 'top_screen',
                    page: 1,
                    top_method: homeVariant.method,
                    top_params: variantParams(homeVariant)
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
                values: 'string', // обязательный маркер для input в Lampa
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

        var VOICES = {
            any: 'Любая',
            'Дубляж': 'Дубляж',
            'Многоголосый': 'Многоголосый',
            'LostFilm': 'LostFilm',
            'Кубик в Кубе': 'Кубик в Кубе',
            'HDrezka Studio': 'HDrezka Studio',
            'Red Head Sound': 'Red Head Sound',
            'Jaskier': 'Jaskier',
            'NewStudio': 'NewStudio'
        }

        Lampa.SettingsApi.addParam({
            component: 'top',
            param: {
                name: 'top_min_quality',
                type: 'select',
                values: { any: 'Любое', '720': '720p и выше', '1080': '1080p и выше (вкл. 4K)', '2160': '4K' },
                default: 'any'
            },
            field: {
                name: T('settings_min_quality')
            }
        })

        Lampa.SettingsApi.addParam({
            component: 'top',
            param: {
                name: 'top_voice_1',
                type: 'select',
                values: VOICES,
                default: 'any'
            },
            field: {
                name: T('settings_voice_1')
            }
        })

        Lampa.SettingsApi.addParam({
            component: 'top',
            param: {
                name: 'top_voice_2',
                type: 'select',
                values: VOICES,
                default: 'any'
            },
            field: {
                name: T('settings_voice_2')
            }
        })

        Lampa.SettingsApi.addParam({
            component: 'top',
            param: {
                name: 'top_hide_watched',
                type: 'trigger',
                default: true // «не хочу видеть просмотренные» — базовое ожидание
            },
            field: {
                name: T('settings_hide_watched'),
                description: T('settings_hide_watched_desc')
            }
        })

        Lampa.SettingsApi.addParam({
            component: 'top',
            param: {
                name: 'top_russian_only',
                type: 'trigger',
                default: true
            },
            field: {
                name: T('settings_russian_only'),
                description: T('settings_russian_only_desc')
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

    }

    if(window.appready) init()
    else Lampa.Listener.follow('app', function(e){
        if(e.type === 'ready') init()
    })
})()
