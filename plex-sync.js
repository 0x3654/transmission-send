/*
    Plex Sync — плагин Lampa (lampa.mx)

    Синхронизация статуса просмотра с аккаунтом Plex:

      • Привязка аккаунта кодом с plex.tv/link (OAuth PIN); сервер аккаунта
        определяется сам (или адрес вручную — LAN/tsdproxy)
      • Lampa → Plex: досмотрели — отметилось на сервере (scrobble), частичный
        просмотр — позиция (progress), сняли отметку — unscrobble
      • Plex → Lampa: «Синхронизировать» переносит просмотренное и позиции
        в таймлайн Lampa (прогресс на карточках) и в «Историю просмотров»

    Настройки: Настройки → Plex.
    Установка: Настройки → Расширения → «+» → URL этого файла.

    Матчинг — по TMDB: Lampa вся на TMDB, айтемы Plex (агенты Plex Movie/Series)
    несут guid tmdb://<id>. Если у аккаунта включён «Sync Watch State», отметка
    на одном сервере сама разъедется по остальным серверам аккаунта.
*/

(function(){
    'use strict'

    var FLAG = '__lampa_plexsync'

    if(window[FLAG]) return
    window[FLAG] = true

    var TV      = 'https://plex.tv/api/v2' // облако Plex (CORS открыт — проверено)
    var WATCHED = 90                       // %, с которого считаем просмотренным
    var PAGE    = 500                      // страница обхода библиотеки

    function init(){
        var Lampa = window.Lampa

        // имя в списке расширений — из каталога cub не придёт, дописываем сами
        ;(function selfName(){
            try{
                var url   = 'https://0x3654.github.io/transmission-send/plex-sync.js'
                var list  = Lampa.Plugins.get()
                var named = false

                for(var i = 0; i < list.length; i++){
                    if((list[i].url || '') === url && list[i].name !== 'Plex Sync — статус просмотра'){
                        list[i].name   = 'Plex Sync — статус просмотра'
                        list[i].author = '@0x3654'
                        list[i].descr  = 'Синхронизация просмотра с аккаунтом Plex'
                        named = true
                    }
                }

                if(named) Lampa.Plugins.save()
            }
            catch(e){}
        })()

        //---------- словарь

        Lampa.Lang.add({
            plex_settings_name:      { ru: 'Plex',                        en: 'Plex' },
            plex_link:               { ru: 'Привязать аккаунт Plex',      en: 'Link Plex account' },
            plex_link_descr:         { ru: 'код вводится на plex.tv/link', en: 'code is entered at plex.tv/link' },
            plex_code_title:         { ru: 'Привязка Plex',               en: 'Plex linking' },
            plex_code_enter:         { ru: 'Откройте <b>plex.tv/link</b> на любом устройстве и введите код', en: 'Open <b>plex.tv/link</b> on any device and enter the code' },
            plex_linked:             { ru: 'Plex: аккаунт привязан',      en: 'Plex: account linked' },
            plex_no_server:          { ru: 'Plex: сервер недоступен',     en: 'Plex: server unreachable' },
            plex_manual_url:         { ru: 'Адрес сервера (пусто — авто)', en: 'Server address (empty = auto)' },
            plex_manual_url_descr:   { ru: 'например, http://192.168.1.2:32400; авто — адрес сервера аккаунта (LAN первым)', en: 'e.g. http://192.168.1.2:32400; auto = account server address' },
            plex_token_manual:       { ru: 'Токен X-Plex-Token (вручную)', en: 'X-Plex-Token (manual)' },
            plex_token_manual_descr: { ru: 'запасной способ привязки вместо кода', en: 'fallback linking method instead of the code' },
            plex_scrobble:           { ru: 'Отмечать просмотр в Plex',    en: 'Scrobble watching to Plex' },
            plex_scrobble_descr:     { ru: 'досмотрели в Lampa — отметилось на сервере Plex', en: 'watched in Lampa gets marked on the Plex server' },
            plex_import_start:       { ru: 'Импорт при запуске',          en: 'Import on start' },
            plex_import_start_descr: { ru: 'тихо подтягивать статусы из Plex при старте Lampa', en: 'silently pull Plex statuses on Lampa start' },
            plex_sync_now:           { ru: 'Синхронизировать сейчас',     en: 'Sync now' },
            plex_sync_done:          { ru: 'Plex: импорт — фильмов: %m, серий: %e (из найденного %s)', en: 'Plex: import — movies: %m, episodes: %e (of %s matched)' },
            plex_sync_empty:         { ru: 'Plex: в библиотеке не нашлось просмотров с TMDB-айдишниками', en: 'Plex: no watched items with TMDB ids found' },
            plex_not_linked:         { ru: 'Plex: сначала привяжите аккаунт', en: 'Plex: link an account first' },
            plex_walk:               { ru: 'Plex: читаю библиотеку…',     en: 'Plex: reading library…' }
        })

        function T(name){
            return Lampa.Lang.translate('plex_' + name)
        }

        function field(name){
            return String(Lampa.Storage.field(name))
        }

        function dbg(obj){
            try{ Lampa.Storage.set('plex_dbg', obj) }catch(e){}
        }

        // map с ограничением параллельности, порядок сохраняем (как в top.js);
        // пустой массив — done сразу (иначе повиснем)
        function mapLimit(arr, limit, fn, done){
            if(!arr.length) return done([])

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

        //---------- HTTP (XHR — работает на всех платформах Lampa)

        function xhr(method, url, headers, ok, fail, timeout){
            var x = new XMLHttpRequest()

            x.open(method, url, true)
            x.timeout = timeout || 15000

            if(headers) for(var k in headers) x.setRequestHeader(k, headers[k])

            x.onload = function(){
                var json = null

                try{ json = JSON.parse(x.responseText) }catch(e){}

                if(x.status >= 200 && x.status < 300) ok(json, x.responseText)
                else fail(json, x.status)
            }
            x.onerror   = function(){ fail(null, 0) }
            x.ontimeout = function(){ fail(null, -1) }

            x.send()
        }

        function qs(params){
            var out = []

            for(var k in params){
                if(params[k] == null) continue
                out.push(encodeURIComponent(k) + '=' + encodeURIComponent(params[k]))
            }

            return out.join('&')
        }

        //---------- plex.tv (облако)

        function clientId(){
            var id = Lampa.Storage.get('plex_client_id', '')

            if(!id){
                id = 'lampa-plexsync-' + Date.now() + '-' + Math.round(Math.random() * 1e8)
                Lampa.Storage.set('plex_client_id', id)
            }

            return id
        }

        function tvHeaders(withToken){
            var h = {
                'Accept': 'application/json',
                'X-Plex-Client-Identifier': clientId(),
                'X-Plex-Product': 'Lampa Plex Sync'
            }

            if(withToken) h['X-Plex-Token'] = token()

            return h
        }

        // токен: из привязки кодом (plex_token_save) либо ручной из настроек
        function token(){
            var t = Lampa.Storage.get('plex_token_save', '')
            if(!t) t = field('plex_token_manual').trim()
            if(t === 'undefined' || t === 'null') t = ''
            return t
        }

        function saveToken(t){
            Lampa.Storage.set('plex_token_save', t)
            server = null // сервер определять заново
        }

        function pinCreate(cb){
            xhr('POST', TV + '/pins?' + qs({ strong: true }), tvHeaders(false), function(j){
                cb(j || null)
            }, function(){ cb(null) })
        }

        function pinGet(id, cb){
            xhr('GET', TV + '/pins/' + id, tvHeaders(false), function(j){
                cb(j || null)
            }, function(){ cb(null) })
        }

        function accountInfo(cb){
            xhr('GET', TV + '/user', tvHeaders(true), function(j){
                cb((j && j.username) || '')
            }, function(){ cb('') })
        }

        //---------- сервер аккаунта

        var server = null // {name, url, token}

        function manualUrl(){
            var url = (Lampa.Storage.field('plex_manual_url') || '').trim()

            if(url){
                if(!/^https?:\/\//i.test(url)) url = 'http://' + url
                url = url.replace(/\/+$/, '')
            }

            return url
        }

        function probe(url, accessToken, cb){
            xhr('GET', url + '/identity?' + qs({ 'X-Plex-Token': accessToken }), { Accept: 'application/json' }, function(json){
                cb(!!(json && json.MediaContainer))
            }, function(){ cb(false) }, 6000)
        }

        function detectServer(cb){
            if(server) return cb(server)
            if(!token()) return cb(null)

            // ручной адрес приоритетен; токен аккаунта работает на своём сервере
            var manual = manualUrl()

            if(manual){
                return probe(manual, token(), function(ok){
                    cb(ok ? (server = { name: 'Plex', url: manual, token: token() }) : null)
                })
            }

            var cached = Lampa.Storage.get('plex_srv', '')

            if(cached && cached.url && Date.now() - (cached.ts || 0) < 86400000){
                return probe(cached.url, cached.token, function(ok){
                    if(ok) cb(server = cached)
                    else{
                        Lampa.Storage.set('plex_srv', '')
                        detectServer(cb)
                    }
                })
            }

            xhr('GET', TV + '/resources?' + qs({ includeHttps: 1, includeRelay: 1 }), tvHeaders(true), function(json){
                var candidates = [] // {url, token, name, local}

                ;(json || []).forEach(function(r){
                    if(r.product !== 'Plex Media Server') return

                    ;(r.connections || []).forEach(function(c){
                        if(!/^https?:\/\//.test(c.uri)) return
                        candidates.push({ url: c.uri.replace(/\/+$/, ''), token: r.accessToken, name: r.name, local: !!c.local })
                    })
                })

                candidates.sort(function(a, b){ return (a.local === b.local) ? 0 : (a.local ? -1 : 1) })

                function tryNext(i){
                    if(i >= candidates.length) return cb(null)

                    var c = candidates[i]

                    probe(c.url, c.token, function(ok){
                        if(ok){
                            server = c
                            Lampa.Storage.set('plex_srv', { name: c.name, url: c.url, token: c.token, ts: Date.now() })
                            cb(server)
                        }
                        else tryNext(i + 1)
                    })
                }

                tryNext(0)
            }, function(){ cb(null) })
        }

        // запрос к серверу Plex: токен в query — запрос «простой», без preflight,
        // единственный заголовок Accept из CORS-safelist
        function pms(method, path, params, ok, fail, timeout){
            detectServer(function(srv){
                if(!srv) return fail('server')

                params = params || {}
                params['X-Plex-Token'] = srv.token

                xhr(method, srv.url + path + '?' + qs(params), { Accept: 'application/json' }, ok, fail, timeout)
            })
        }

        //---------- секции и матчинг по TMDB

        var sections = null // [{id, type, title}]

        function loadSections(cb){
            if(sections) return cb(sections)

            pms('GET', '/library/sections', {}, function(json){
                var out = []

                ;(((json || {}).MediaContainer || {}).Directory || []).forEach(function(d){
                    if(d.Type === 'movie' || d.Type === 'show') out.push({ id: d.Key, type: d.Type, title: d.Title })
                })

                sections = out
                cb(out)
            }, function(){ cb([]) })
        }

        var guidCache = {} // 'movie123' | 'show456' → ratingKey | null

        // guid-фильтр: /library/sections/{id}/all?guid=tmdb://123 — точный матчинг
        function findByGuid(kind, tmdb, cb){
            if(!tmdb) return cb(null)

            var key = kind + tmdb

            if(guidCache.hasOwnProperty(key)) return cb(guidCache[key] || null)

            loadSections(function(secs){
                var pool = secs.filter(function(s){ return s.type === kind })
                var i = 0

                function next(){
                    if(i >= pool.length){
                        guidCache[key] = null // негативный кэш до перезапуска
                        return cb(null)
                    }

                    var sec = pool[i++]

                    pms('GET', '/library/sections/' + sec.id + '/all', { guid: 'tmdb://' + tmdb }, function(json){
                        var meta = ((((json || {}).MediaContainer || {}).Metadata) || [])[0]
                        var rk = meta ? meta.ratingKey : null

                        if(rk) guidCache[key] = rk

                        cb(rk || null)
                    }, function(){ next() })
                }

                next()
            })
        }

        var leavesCache = {} // showRk → [{rk, s, e}]

        // эпизоды шоу одним запросом: кэшируем соответствие сезон/эпизод → ratingKey
        function findEpisode(showTmdb, season, episode, cb){
            findByGuid('show', showTmdb, function(showRk){
                if(!showRk) return cb(null)

                if(leavesCache[showRk]) return cb(pickLeaf(showRk, season, episode))

                pms('GET', '/library/metadata/' + showRk + '/allLeaves', { includeGuids: 1 }, function(json){
                    var leaves = []

                    ;((((json || {}).MediaContainer || {}).Metadata) || []).forEach(function(m){
                        leaves.push({ rk: m.ratingKey, s: parseInt(m.parentIndex, 10), e: parseInt(m.index, 10) })
                    })

                    leavesCache[showRk] = leaves

                    cb(pickLeaf(showRk, season, episode))
                }, function(){ cb(null) })
            })
        }

        function pickLeaf(showRk, season, episode){
            var leaves = leavesCache[showRk] || []

            for(var i = 0; i < leaves.length; i++){
                if(leaves[i].s === season && leaves[i].e === episode) return leaves[i].rk
            }

            return null
        }

        //---------- исходящие: Lampa → Plex

        var meta = {} // hash таймлайна → {kind, tmdb, season, episode}

        var importing = false // пока тянем из Plex — свои события не отправляем

        function outbox(){
            return Lampa.Storage.get('plex_out', {})
        }

        function onPlayerStart(data){
            if(!data || !data.timeline) return

            var card = data.card || data.movie || {}
            var ep   = (data.movie && data.movie !== card) ? data.movie : null
            var m    = { kind: '', tmdb: 0, season: 0, episode: 0 }

            // эпизод: TMDB-объект эпизода несёт season_number/episode_number/show_id
            var s = (ep && ep.season_number != null) ? ep : ((card.season_number != null) ? card : null)

            if(s && s.episode_number != null){
                m.kind    = 'episode'
                m.season  = parseInt(s.season_number, 10)
                m.episode = parseInt(s.episode_number, 10)
                m.tmdb    = parseInt(s.show_id, 10) || ((s !== card) ? parseInt(card.id, 10) : 0)
            }
            else if(card.release_date || card.title){
                m.kind = 'movie'
                m.tmdb = parseInt(card.id, 10)
            }
            // карточка шоу без номера эпизода — контекст эпизода не поймали, пропускаем

            // отладка: последний payload старта — для живой проверки формы данных
            dbg({
                kind: m.kind, tmdb: m.tmdb, s: m.season, e: m.episode,
                keys: Object.keys(data).join(','),
                card: card ? [card.id, card.title || card.name || '', card.original_title || card.original_name || '', card.season_number, card.episode_number, card.show_id].join('|') : '',
                movie: ep ? [ep.id, ep.name || ep.title || '', ep.season_number, ep.episode_number, ep.show_id].join('|') : ''
            })

            if((m.kind === 'movie' || m.kind === 'episode') && m.tmdb){
                m.hash = String(data.timeline.hash)
                meta[m.hash] = m
            }
        }

        function onTimelineChange(e){
            if(importing) return
            if(!e || e.target !== 'timeline' || e.reason !== 'update') return

            var hash = e.data && String(e.data.hash)
            var road = e.data && e.data.road

            if(!hash || !road) return

            var m = meta[hash]

            if(!m) return // контекст не известен — не наш случай

            var out = outbox()

            out[hash] = {
                kind: m.kind, tmdb: m.tmdb, season: m.season, episode: m.episode,
                percent: road.percent || 0,
                time: Math.round(road.time || 0),
                duration: Math.round(road.duration || 0),
                ts: Date.now()
            }

            Lampa.Storage.set('plex_out', out)
        }

        function resolveOut(item, cb){
            if(item.kind === 'movie') findByGuid('movie', item.tmdb, cb)
            else findEpisode(item.tmdb, item.season, item.episode, cb)
        }

        function flushOutbox(cb){
            if(!token()) return (cb || function(){})()

            var out   = outbox()
            var keys  = Object.keys(out)
            var marks = Lampa.Storage.get('plex_scrobbled', {})

            if(!keys.length) return (cb || function(){})()

            // подчищаем память скробблов старше 90 дней
            for(var k in marks){
                if(Date.now() - marks[k] > 90 * 86400000) delete marks[k]
            }

            mapLimit(keys, 2, function(hash, next){
                var item = out[hash]

                resolveOut(item, function(rk){
                    if(!rk){ // нет в библиотеке Plex — уходит молча
                        delete out[hash]
                        return next()
                    }

                    function done(){
                        delete out[hash]
                        next()
                    }

                    if(item.percent >= WATCHED && !marks[hash]){
                        pms('POST', '/:/scrobble', { key: rk, identifier: 'com.plexapp.plugins.library' }, function(){
                            marks[hash] = Date.now()
                            done()
                        }, done)
                    }
                    else if(item.percent === 0 && marks[hash]){
                        pms('POST', '/:/unscrobble', { key: rk, identifier: 'com.plexapp.plugins.library' }, function(){
                            delete marks[hash]
                            done()
                        }, done)
                    }
                    else if(item.time > 0){
                        pms('POST', '/:/progress', {
                            key: rk,
                            identifier: 'com.plexapp.plugins.library',
                            time: item.time * 1000, // Plex хранит миллисекунды
                            state: 'stopped'
                        }, done, done)
                    }
                    else done()
                })
            }, function(){
                Lampa.Storage.set('plex_out', out)
                Lampa.Storage.set('plex_scrobbled', marks)
                ;(cb || function(){})()
            })
        }

        //---------- импорт: Plex → Lampa

        // хеш таймлайна Lampa: Utils.hash(original_title) — фильм,
        // Utils.hash(season + (season>10?':':'') + episode + original_name) — серия
        function hashMovie(title){
            return Lampa.Utils.hash(title || '')
        }

        function hashEpisode(season, episode, name){
            return Lampa.Utils.hash([season, season > 10 ? ':' : '', episode, name || ''].join(''))
        }

        // tmdb-гуиды айтема: фильм → tmdb://id; эпизод (новые агенты) → tmdb://show/s/e
        function tmdbGuids(item){
            var found = [] // строки tmdb://…

            ;((item && item.Guid) || []).forEach(function(g){
                if(/^tmdb:\/\/\d+(\/\d+\/\d+)?$/.test(g.id || '')) found.push(g.id)
            })

            return found
        }

        // paged обход секции (type: 1 — фильмы, 4 — эпизоды)
        function walk(sec, type, ready){
            var all = [], start = 0

            function page(){
                pms('GET', '/library/sections/' + sec.id + '/all', {
                    type: type,
                    includeGuids: 1,
                    'X-Plex-Container-Start': start,
                    'X-Plex-Container-Size': PAGE
                }, function(json){
                    var mc = (json || {}).MediaContainer || {}
                    var md = mc.Metadata || []

                    all = all.concat(md)
                    start += md.length

                    if(md.length >= PAGE && start < (mc.totalSize || 0)) page()
                    else ready(all)
                }, function(){ ready(all) }, 30000)
            }

            page()
        }

        // карточка и оригинальное название через TMDB Lampa (кэш на неделю)
        function tmdbGet(kind, id, cb){
            if(!id) return cb(null)

            Lampa.Api.sources.tmdb.get(kind + '/' + id, {}, function(json){
                cb(json || null)
            }, function(){ cb(null) }, 604800)
        }

        // LWW: локальная отметка новее входящей — не затираем
        function applyTimeline(hash, percent, time, duration, updated){
            var cur = Lampa.Timeline.view(hash)

            if(cur && cur.percent && cur.updated && cur.updated > updated) return false

            Lampa.Timeline.update({
                hash: hash,
                percent: percent,
                time: time,
                duration: duration,
                profile: 0,
                updated: updated,
                received: true
            })

            return true
        }

        // «История просмотров»/«Продолжить просмотр» строятся из Favorite 'history';
        // при активном облачном аккаунте CUB Favorite уходит в облако — тогда не трогаем
        function canHistory(){
            try{ return !(Lampa.Account && Lampa.Account.Permit && Lampa.Account.Permit.sync) }
            catch(e){ return true }
        }

        function importAll(done){
            importing = true

            var stat = { movies: 0, episodes: 0, seen: 0 }
            var movies = []  // {tmdb, viewed, offset, duration, viewedAt, fallbackTitle}
            var shows = {}   // showTmdb → {eps: [...], lastAt}
            var historyAdd = []

            detectServer(function(srv){
                if(!srv){
                    importing = false
                    return done('server', stat)
                }

                loadSections(function(secs){
                    var queue = secs.slice(0)

                    function nextSection(){
                        if(!queue.length) return process()

                        var sec = queue.shift()

                        walk(sec, sec.type === 'show' ? 4 : 1, function(items){
                            items.forEach(function(item){
                                var guids = tmdbGuids(item)
                                if(!guids.length) return

                                var viewedAt = (parseInt(item.lastViewedAt, 10) || 0) * 1000
                                var dur      = (parseInt(item.duration, 10) || 0) / 1000
                                var viewed   = parseInt(item.viewCount, 10) > 0
                                var offset   = (parseInt(item.viewOffset, 10) || 0) / 1000

                                if(!viewed && !offset) return

                                stat.seen++

                                if(sec.type === 'movie'){
                                    movies.push({
                                        tmdb: parseInt(/^tmdb:\/\/(\d+)/.exec(guids[0])[1], 10),
                                        viewed: viewed, offset: offset, duration: dur,
                                        viewedAt: viewedAt, fallbackTitle: item.originalTitle || item.title
                                    })
                                }
                                else{
                                    var sm = /^tmdb:\/\/(\d+)\/(\d+)\/(\d+)$/.exec(guids[0])

                                    if(!sm) return // эпизод с guid tmdb://show (без s/e) — не матчим

                                    var showTmdb = parseInt(sm[1], 10)

                                    if(!shows[showTmdb]) shows[showTmdb] = { eps: [], lastAt: 0 }

                                    shows[showTmdb].eps.push({
                                        s: parseInt(sm[2], 10), e: parseInt(sm[3], 10),
                                        viewed: viewed, offset: offset, duration: dur, viewedAt: viewedAt
                                    })

                                    if(viewedAt > shows[showTmdb].lastAt) shows[showTmdb].lastAt = viewedAt
                                }
                            })

                            nextSection()
                        })
                    }

                    function process(){
                        mapLimit(movies, 3, function(mv, nextM){
                            tmdbGet('movie', mv.tmdb, function(card){
                                var orig = card ? card.original_title : mv.fallbackTitle
                                var hash = hashMovie(orig)

                                if(mv.viewed){
                                    if(applyTimeline(hash, 100, mv.duration, mv.duration, mv.viewedAt || Date.now())) stat.movies++
                                }
                                else{
                                    applyTimeline(
                                        hash,
                                        mv.duration ? Math.min(95, Math.round(mv.offset / mv.duration * 100)) : 0,
                                        mv.offset, mv.duration, mv.viewedAt || Date.now()
                                    )
                                }

                                if(card && mv.viewedAt) historyAdd.push({ card: card, viewedAt: mv.viewedAt })

                                nextM()
                            })
                        }, function(){
                            var ids = Object.keys(shows)

                            mapLimit(ids, 3, function(id, nextS){
                                var show = shows[id]

                                tmdbGet('tv', parseInt(id, 10), function(card){
                                    var orig = card ? card.original_name : ''

                                    if(!orig) return nextS() // без имени не посчитать хеш серии

                                    show.eps.forEach(function(ep){
                                        var hash = hashEpisode(ep.s, ep.e, orig)

                                        if(ep.viewed){
                                            if(applyTimeline(hash, 100, ep.duration, ep.duration, ep.viewedAt || Date.now())) stat.episodes++
                                        }
                                        else if(ep.offset > 0){
                                            applyTimeline(
                                                hash,
                                                ep.duration ? Math.min(95, Math.round(ep.offset / ep.duration * 100)) : 0,
                                                ep.offset, ep.duration, ep.viewedAt || Date.now()
                                            )
                                        }
                                    })

                                    if(card && show.lastAt) historyAdd.push({ card: card, viewedAt: show.lastAt })

                                    nextS()
                                })
                            }, function(){
                                // «История просмотров»: свежие сверху, лимит Lampa сама режет до 100
                                historyAdd.sort(function(a, b){ return b.viewedAt - a.viewedAt })

                                if(canHistory()){
                                    historyAdd.forEach(function(h){
                                        try{ Lampa.Favorite.add('history', h.card, 100) }catch(e){}
                                    })
                                }

                                importing = false
                                done(null, stat)
                            })
                        })
                    }

                    nextSection()
                })
            })
        }

        //---------- привязка аккаунта (код plex.tv/link)

        function linkAccount(){
            var box = $(
                '<div class="about" style="text-align:center">' +
                    '<div class="plex-pin-code" style="font-size:2.2em;letter-spacing:.25em;font-weight:300">— — — —</div>' +
                    '<div style="margin-top:1.4em;opacity:.8">' + T('code_enter') + '</div>' +
                    '<div style="margin-top:1.4em;opacity:.4" class="plex-pin-timer"></div>' +
                '</div>'
            )

            var timer = null, poller = null, left = 300

            function stop(){
                clearInterval(timer)
                clearInterval(poller)
            }

            Lampa.Modal.open({
                title: T('code_title'),
                html: box,
                size: 'medium',
                onBack: function(){
                    stop()
                    Lampa.Modal.close()
                }
            })

            function countdown(){
                box.find('.plex-pin-timer').text(Math.floor(left / 60) + ':' + ('0' + (left % 60)).slice(-2))
            }

            pinCreate(function(pin){
                if(!pin || !pin.id){
                    stop()
                    Lampa.Modal.close()
                    return Lampa.Noty.show(T('no_server'), { style: 'error', time: 6000 })
                }

                box.find('.plex-pin-code').text(pin.code)
                countdown()

                timer = setInterval(function(){
                    left--
                    left > 0 ? countdown() : stop()
                }, 1000)

                poller = setInterval(function(){
                    if(left <= 0) return

                    pinGet(pin.id, function(j){
                        if(j && j.authToken){
                            stop()
                            Lampa.Modal.close()

                            saveToken(j.authToken)

                            accountInfo(function(username){
                                Lampa.Storage.set('plex_user', username || '')
                                Lampa.Noty.show(T('linked') + (username ? ': ' + username : ''), { time: 5000 })
                            })

                            // привязали — сразу тянем статусы (сценарий «привязал и получил»)
                            syncNow()
                        }
                    })
                }, 3000)
            })
        }

        //---------- «Синхронизировать сейчас»: импорт + отправка накопленного

        function syncNow(){
            if(!token()) return Lampa.Noty.show(T('not_linked'), { time: 5000 })

            Lampa.Noty.show(T('walk'), { time: 300000 })

            importAll(function(err, stat){
                if(err === 'server') return Lampa.Noty.show(T('no_server'), { style: 'error', time: 6000 })

                if(!stat.seen) Lampa.Noty.show(T('sync_empty'), { time: 8000 })
                else Lampa.Noty.show(
                    T('sync_done').replace('%m', stat.movies).replace('%e', stat.episodes).replace('%s', stat.seen),
                    { time: 7000 }
                )

                flushOutbox()
            })
        }

        //---------- события

        Lampa.Player.listener.follow('start', onPlayerStart)

        Lampa.Listener.follow('state:changed', onTimelineChange)

        //---------- настройки

        var ico_plex = '<svg viewBox="0 0 36 36" fill="none" stroke="white" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">' +
            '<path d="M14 7 L27 18 L14 29"/>' +
            '</svg>'

        Lampa.SettingsApi.addComponent({
            component: 'plexsync',
            icon: ico_plex,
            name: T('settings_name')
        })

        Lampa.SettingsApi.addParam({
            component: 'plexsync',
            param: {
                name: 'plex_link',
                type: 'button'
            },
            field: {
                name: T('link'),
                description: T('link_descr')
            },
            onChange: function(){
                linkAccount()
            }
        })

        Lampa.SettingsApi.addParam({
            component: 'plexsync',
            param: {
                name: 'plex_sync_now',
                type: 'button'
            },
            field: {
                name: T('sync_now'),
                description: '—'
            },
            onRender: function(item){
                var user = Lampa.Storage.get('plex_user', '')
                var srv  = Lampa.Storage.get('plex_srv', '')

                item.find('.settings-param__descr').eq(0).text(
                    token() ? ((user || 'ok') + (srv && srv.name ? ' · ' + srv.name : '')) : '—'
                )
            },
            onChange: function(){
                syncNow()
            }
        })

        Lampa.SettingsApi.addParam({
            component: 'plexsync',
            param: {
                name: 'plex_scrobble',
                type: 'trigger',
                default: true
            },
            field: {
                name: T('scrobble'),
                description: T('scrobble_descr')
            }
        })

        Lampa.SettingsApi.addParam({
            component: 'plexsync',
            param: {
                name: 'plex_import_start',
                type: 'trigger',
                default: false
            },
            field: {
                name: T('import_start'),
                description: T('import_start_descr')
            }
        })

        Lampa.SettingsApi.addParam({
            component: 'plexsync',
            param: {
                name: 'plex_manual_url',
                type: 'input',
                values: 'string',
                default: '',
                placeholder: 'http://192.168.1.2:32400'
            },
            field: {
                name: T('manual_url'),
                description: T('manual_url_descr')
            }
        })

        Lampa.SettingsApi.addParam({
            component: 'plexsync',
            param: {
                name: 'plex_token_manual',
                type: 'input',
                values: 'string',
                default: '',
                placeholder: 'X-Plex-Token'
            },
            field: {
                name: T('token_manual'),
                description: T('token_manual_descr')
            },
            onChange: function(){
                // ручной токен валидируем сразу;token() подхватит и без этого
                var t = field('plex_token_manual').trim()

                if(!t) return

                xhr('GET', TV + '/user', {
                    Accept: 'application/json',
                    'X-Plex-Client-Identifier': clientId(),
                    'X-Plex-Token': t
                }, function(json){
                    saveToken(t)
                    Lampa.Storage.set('plex_user', (json && json.username) || '')
                    Lampa.Noty.show(T('linked'), { time: 4000 })
                }, function(){
                    Lampa.Noty.show(T('no_server'), { style: 'error', time: 6000 })
                })
            }
        })

        //---------- автозапуск

        if(token()){
            // тихая досылка накопленного + желаемый импорт при старте
            setTimeout(function(){
                flushOutbox(function(){
                    if(field('plex_import_start') === 'true') importAll(function(){})
                })
            }, 8000)

            setInterval(function(){
                if(Object.keys(outbox()).length) flushOutbox()
            }, 5 * 60 * 1000)
        }
    }

    if(window.appready) init()
    else Lampa.Listener.follow('app', function(e){
        if(e.type === 'ready') init()
    })
})()
