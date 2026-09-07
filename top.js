/*
    Top — плагин Lampa (lampa.mx)

    Два пункта в меню:
      • «Топ»           — популярное/лучшее по TMDB (тренды за день/неделю, топ по рейтингу,
                          новинки); переключение варианта — кнопка вправо или через выбор
      • «Топ трекеров»  — топ раздач NNMClub по сидам, обогащённый постерами TMDB.
                          Данные от маленького сервера tracker-top (см. tracker-top/ в репо),
                          адрес задаётся в настройках плагина.

    Установка: Настройки → Расширения → «+» → URL этого файла.
*/

(function(){
    'use strict'

    var FLAG = '__lampa_top'

    if(window[FLAG]) return
    window[FLAG] = true

    // сколько раздач обогащать запросами к TMDB (лимит на экран)
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

        function pushVariant(v){
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

        function normTitle(s){
            return (s || '').toLowerCase()
                .replace(/[«»"'`!?:.,()\[\]{}–—|]/g, ' ')
                .replace(/\s+/g, ' ')
                .trim()
        }

        // выбрать из результатов TMDB лучший матч для раздачи
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

                net.silent(base + '/top?cat=video&pages=2', function(json){
                    var items = (json && json.items) || []

                    mapLimit(items.slice(0, MATCH_LIMIT), 4, matchOne, function(matched){
                        var results = []
                        var seen = {}

                        for(var i = 0; i < matched.length; i++){
                            var el = matched[i]

                            if(!el) continue

                            var key = el.media_type + ':' + el.id

                            if(seen[key]) continue // сервер уже отсортирован по сидам — первый и есть топовый
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
            pushVariant(VARIANTS[0])
        })

        Lampa.Menu.addButton(ico_trackers, T('menu_trackers'), function(){
            Lampa.Activity.push({
                url: '',
                title: T('menu_trackers'),
                component: 'top_trackers',
                page: 1
            })
        })

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
            },
            onChange: function(){
                Lampa.Activity.replace({})
            }
        })

        //---------- словарь

        Lampa.Lang.add({
            top_menu_top:          { ru: 'Топ',                    en: 'Top' },
            top_menu_trackers:     { ru: 'Топ трекеров',           en: 'Tracker top' },
            top_variants:          { ru: 'Что показать',           en: 'What to show' },
            top_need_server:       { ru: 'Укажите адрес сервера tracker-top в настройках', en: 'Set tracker-top server address in settings' },
            top_server_fail:       { ru: 'Сервер топа недоступен', en: 'Top server unreachable' },
            top_trackers_matched:  { ru: 'Совпало с TMDB:',        en: 'Matched on TMDB:' },
            top_settings_name:     { ru: 'Топ',                    en: 'Top' },
            top_settings_server:   { ru: 'Адрес сервера топа',     en: 'Top server address' },
            top_settings_server_desc: { ru: 'tracker-top: https://… (micro/VPN), см. репо', en: 'tracker-top: https://… (see repo)' }
        })
    }

    if(window.appready) init()
    else Lampa.Listener.follow('app', function(e){
        if(e.type === 'ready') init()
    })
})()
