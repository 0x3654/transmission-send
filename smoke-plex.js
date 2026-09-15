// Смоук-тест плагина plex-sync.js: привязка (PIN), детект сервера, скробблинг
// Lampa→Plex (progress/scrobble/unscrobble), импорт Plex→Lampa (timeline+history), LWW
const fs = require('fs')
const vm = require('vm')
const assert = require('assert')

const source = fs.readFileSync(__dirname + '/plex-sync.js', 'utf8')

const calls = { xhr: [], noty: [], params: [], timelineUpdates: [], favoriteAdds: [], modal: [], plugins: [] }
const listeners = {}
const state = {
    storage: {}, fields: {},
    xhrRoutes: [],   // [{match(url, method), reply: {status, json}}]
    tmdb: {},        // 'movie/100' → карточка
    intervals: {},   // фейковые интервалы: id → fn
    nextInterval: 1
}

// DJB2 — как Lampa.Utils.hash (бандл app.min.js)
function lampaHash(input){
    const str = (input || '') + ''
    let h = 0
    if(!str.length) return h + ''
    for(let i = 0; i < str.length; i++){
        h = (h << 5) - h + str.charCodeAt(i)
        h = h & h
    }
    return Math.abs(h) + ''
}

// фейковый XHR: маршруты по url, ответ синхронно в send()
class FakeXHR {
    constructor(){ this.headers = {} }
    open(method, url){ this.method = method; this.url = url }
    setRequestHeader(k, v){ this.headers[k.toLowerCase()] = v }
    send(){
        calls.xhr.push({ method: this.method, url: this.url, headers: this.headers })

        for(const r of state.xhrRoutes){
            if(r.match(this.url, this.method)){
                const { status, json } = r.reply
                this.status = status
                this.responseText = JSON.stringify(json)
                this.onload()
                return
            }
        }

        this.status = 0
        this.onerror()
    }
}

const sandbox = {
    console, Math, Date, JSON, parseInt, setTimeout,
    navigator: {},
    document: { createElement: () => ({}) },
    XMLHttpRequest: FakeXHR,
    setInterval(fn){ const id = state.nextInterval++; state.intervals[id] = fn; return id },
    clearInterval(id){ delete state.intervals[id] },
    window: null
}
sandbox.window = sandbox
sandbox.appready = true

// минимальный фейк jQuery для модалки привязки (код + таймер)
sandbox.$ = function(html){
    const el = { codeShown: '', timerShown: '' }
    el.find = (sel) => ({ text: (t) => { if(sel === '.plex-pin-code') el.codeShown = t } })
    return el
}
sandbox.Lampa = {
    Lang: { add(){}, translate: (k) => k },
    Noty: { show(text, params){ calls.noty.push({ text, params }) } },
    Listener: { follow(type, fn){ (listeners[type] = listeners[type] || []).push(fn) } },
    Player: { listener: { follow(type, fn){ (listeners['player:' + type] = listeners[type === 'start' ? 'player:start' : 'player:' + type] = listeners['player:' + type] || []).push(fn) } } },
    Plugins: { get(){ return calls.plugins }, save(){} },
    Modal: { open(opts){ calls.modal.push(opts) }, close(){} },
    SettingsApi: { addComponent(c){ calls.settingsComponent = c }, addParam(p){ calls.params.push(p) } },
    Storage: {
        field(name){ return state.fields[name] },
        get(key, def){
            const v = key in state.storage ? state.storage[key] : def
            if(typeof v === 'string' && (v[0] === '[' || v[0] === '{')){
                try{ return JSON.parse(v) }catch(e){}
            }
            return v
        },
        set(key, v){ state.storage[key] = v }
    },
    Utils: { hash: lampaHash },
    Timeline: {
        update(p){ calls.timelineUpdates.push(p) },
        view(hash){
            return (state.storage.__tl && state.storage.__tl[hash]) || { hash, percent: 0, time: 0, duration: 0, profile: 0, updated: 0 }
        }
    },
    Favorite: { add(type, card, limit){ calls.favoriteAdds.push({ type, card, limit }) } },
    Account: { Permit: { sync: false } },
    Api: { sources: { tmdb: { get(method, params, ok, fail){ ok(state.tmdb[method]) } } } }
}

function route(match, reply){ state.xhrRoutes.push({ match, reply }) }
function tick(){ Object.keys(state.intervals).forEach((id) => state.intervals[id] && state.intervals[id]()) }

vm.createContext(sandbox)
vm.runInContext(source, sandbox)

const fire = (type, e) => (listeners[type] || []).forEach(fn => fn(e))
const param = (name) => calls.params.find(p => p.param.name === name)

// --- 1. регистрация: раздел Plex, параметры, input с маркером values:'string'
assert.strictEqual(calls.settingsComponent.component, 'plexsync')
assert.deepStrictEqual(calls.params.map(p => p.param.name),
    ['plex_link', 'plex_sync_now', 'plex_scrobble', 'plex_import_start', 'plex_manual_url', 'plex_token_manual'])
for(const p of calls.params){
    if(p.param.type === 'input') assert.strictEqual(p.param.values, 'string', 'input обязан иметь values:string')
}
console.log('✓ регистрация: раздел «Plex», 6 параметров, input с маркером')

// --- 2. PIN-привязка: модалка с кодом → поллинг ловит authToken → токен сохранён, синк стартует
{
    let pinPolls = 0

    route((url) => url.includes('/api/v2/pins?'), { status: 201, json: { id: 12345, code: 'ABCD' } })
    route((url) => url.includes('/pins/12345'), {
        status: 200,
        json: { get authToken(){ return ++pinPolls > 1 ? 'TOKEN-1' : '' } }
    })
    route((url) => url.includes('/api/v2/user') && !url.includes('/pins'), { status: 200, json: { username: 'userx' } })
    route((url) => url.includes('/resources'), { status: 200, json: [] }) // серверов не найдём — синк скажет «недоступен»

    param('plex_link').onChange()

    assert.strictEqual(calls.modal.length, 1, 'модалка с кодом открыта')

    tick() // первый поллинг: authToken пуст
    tick() // второй: authToken выдан

    assert.strictEqual(state.storage.plex_token_save, 'TOKEN-1', 'токен сохранён')
    assert.strictEqual(state.storage.plex_user, 'userx', 'имя аккаунта сохранено')
    assert.ok(calls.noty.some(n => n.text === 'plex_linked: userx'), 'нотификация о привязке')
    assert.ok(calls.noty.some(n => n.text === 'plex_no_server'), 'авто-синк после привязки стартовал (сервера нет — честная ошибка)')
    assert.ok(calls.xhr.some(x => x.method === 'POST' && x.url.includes('strong=true')), 'PIN создан POST со strong')
    assert.ok(calls.xhr.every(x => x.headers['x-plex-client-identifier']), 'Client-Identifier на запросах к plex.tv')
    console.log('✓ PIN: модалка → код ABCD → поллинг → токен сохранён → авто-синк')
}

// --- 3. сквозной Lampa→Plex: детект по ручному адресу, прогресс/скроббл/анскроббл
{
    state.xhrRoutes.length = 0
    state.fields.plex_manual_url = 'plex.local:32400'
    state.storage.plex_token_save = 'TOKEN-1'

    route((url) => url.includes('plex.local:32400/identity'), { status: 200, json: { MediaContainer: {} } })
    route((url) => url.includes('/library/sections?'), {
        status: 200,
        json: { MediaContainer: { Directory: [{ Key: 1, Type: 'movie', Title: 'Кино' }, { Key: 2, Type: 'show', Title: 'Сериалы' }] } }
    })
    route((url) => decodeURIComponent(url).includes('guid=tmdb://100'), {
        status: 200, json: { MediaContainer: { Metadata: [{ ratingKey: 777 }] } }
    })
    route((url) => url.includes('/library/sections/1/all'), { status: 200, json: { MediaContainer: { totalSize: 0, Metadata: [] } } })
    route((url) => url.includes('/library/sections/2/all'), { status: 200, json: { MediaContainer: { totalSize: 0, Metadata: [] } } })
    route((url) => url.includes('/:/scrobble'), { status: 200, json: {} })
    route((url) => url.includes('/:/progress'), { status: 200, json: {} })
    route((url) => url.includes('/:/unscrobble'), { status: 200, json: {} })

    fire('player:start', {
        card: { id: 100, title: 'Холоп', original_title: 'Kholop', release_date: '2026-01-01' },
        timeline: { hash: 555 }
    })

    assert.strictEqual(state.storage.plex_dbg.kind, 'movie', 'фильм распознан по card')
    assert.strictEqual(state.storage.plex_dbg.tmdb, 100)

    // частичный просмотр 40% → progress (мс, stopped)
    fire('state:changed', { target: 'timeline', reason: 'update', data: { hash: 555, road: { percent: 40, time: 4000, duration: 10000 } } })
    assert.ok(state.storage.plex_out['555'], 'изменение попало в outbox')

    param('plex_sync_now').onChange()

    const progress = calls.xhr.find(x => x.url.includes('/:/progress'))
    assert.ok(progress, 'ушёл progress')
    assert.ok(progress.url.includes('key=777') && progress.url.includes('time=4000000') && progress.url.includes('state=stopped'),
        'progress: ratingKey + миллисекунды + stopped — ' + progress.url)
    assert.ok(progress.url.includes('X-Plex-Token=TOKEN-1'), 'токен сервера в query (без кастомных заголовков)')
    assert.ok(!progress.headers['x-plex-token'], 'к PMS без кастомных заголовков — CORS-preflight не нужен')

    // досмотрели 95% → scrobble
    fire('state:changed', { target: 'timeline', reason: 'update', data: { hash: 555, road: { percent: 95, time: 9500, duration: 10000 } } })
    param('plex_sync_now').onChange()
    const scrobble = calls.xhr.filter(x => x.url.includes('/:/scrobble')).pop()
    assert.ok(scrobble && scrobble.url.includes('key=777'), 'досмотрен → scrobble')
    assert.ok(state.storage.plex_scrobbled['555'], 'scrobble отмечен в памяти')

    // сняли отметку → unscrobble
    fire('state:changed', { target: 'timeline', reason: 'update', data: { hash: 555, road: { percent: 0, time: 0, duration: 10000 } } })
    param('plex_sync_now').onChange()
    const unscrobble = calls.xhr.filter(x => x.url.includes('/:/unscrobble')).pop()
    assert.ok(unscrobble && unscrobble.url.includes('key=777'), 'снятие → unscrobble')
    assert.ok(!state.storage.plex_scrobbled['555'], 'память scrobble очищена')

    assert.deepStrictEqual(Object.keys(state.storage.plex_out), [], 'outbox пуст после flush')
    console.log('✓ Lampa→Plex: детект сервера, progress (мс/stopped) → scrobble 95% → unscrobble')
}

// --- 4. эпизод: старт плеера с объектом эпизода → findEpisode по guid шоу
{
    state.xhrRoutes.length = 0

    route((url) => decodeURIComponent(url).includes('guid=tmdb://500'), {
        status: 200, json: { MediaContainer: { Metadata: [{ ratingKey: 900 }] } }
    })
    route((url) => url.includes('/library/metadata/900/allLeaves'), {
        status: 200,
        json: { MediaContainer: { Metadata: [
            { ratingKey: 901, parentIndex: 1, index: 1 },
            { ratingKey: 902, parentIndex: 1, index: 2 },
            { ratingKey: 903, parentIndex: 11, index: 2 }
        ] } }
    })
    route((url) => url.includes('/:/progress'), { status: 200, json: {} })

    fire('player:start', {
        card: { id: 500, name: 'Сериал', original_name: 'Show Name', number_of_seasons: 2 },
        movie: { id: 5001, name: 'Эпизод', season_number: 1, episode_number: 2, show_id: 500 },
        timeline: { hash: 777 }
    })

    assert.strictEqual(state.storage.plex_dbg.kind, 'episode', 'эпизод распознан (movie-объект с season/show_id)')
    assert.strictEqual(state.storage.plex_dbg.tmdb, 500, 'tmdb шоу из show_id')

    fire('state:changed', { target: 'timeline', reason: 'update', data: { hash: 777, road: { percent: 30, time: 800, duration: 2700 } } })
    param('plex_sync_now').onChange() // импорт снова пуст (кэш секций с totalSize 0) + flush

    const epProgress = calls.xhr.filter(x => x.url.includes('/:/progress')).pop()
    assert.ok(epProgress && epProgress.url.includes('key=902'), 'эпизод s01e02 → ratingKey 902 — ' + (epProgress && epProgress.url))
    console.log('✓ эпизод: show_id+season/episode → allLeaves → точный ratingKey')
}

// --- 5. импорт Plex→Lampa: фильм watched + эпизод + «История просмотров»
{
    state.xhrRoutes.length = 0

    route((url) => url.includes('/library/sections/1/all') && !url.includes('guid='), {
        status: 200,
        json: { MediaContainer: { totalSize: 2, Metadata: [
            { ratingKey: 10, viewCount: 2, lastViewedAt: 1750000000, duration: 7200000, originalTitle: 'Kholop', title: 'Холоп',
              Guid: [{ id: 'tmdb://100' }, { id: 'imdb://tt123' }] },
            { ratingKey: 11, viewCount: 0, viewOffset: 0, Guid: [{ id: 'tmdb://999' }] } // не смотрели — мимо
        ] } }
    })
    route((url) => url.includes('/library/sections/2/all') && !url.includes('guid='), {
        status: 200,
        json: { MediaContainer: { totalSize: 2, Metadata: [
            { ratingKey: 20, viewCount: 1, lastViewedAt: 1750000100, duration: 2700000, parentIndex: 1, index: 2,
              Guid: [{ id: 'tmdb://500/1/2' }] },
            { ratingKey: 21, viewCount: 0, viewOffset: 600000, duration: 2700000, parentIndex: 11, index: 2,
              Guid: [{ id: 'tmdb://500/11/2' }] } // частично: 600с из 2700с
        ] } }
    })
    route((url) => url.includes('/:/progress'), { status: 200, json: {} })
    state.tmdb['movie/100'] = { id: 100, title: 'Холоп', original_title: 'Kholop', release_date: '2026-01-01' }
    state.tmdb['tv/500'] = { id: 500, name: 'Сериал', original_name: 'Show Name', first_air_date: '2026-01-01' }

    param('plex_sync_now').onChange()

    // фильм: percent 100, received, updated = lastViewedAt*1000
    const movieUpd = calls.timelineUpdates.find(u => u.hash === lampaHash('Kholop'))
    assert.ok(movieUpd, 'таймлайн фильма по хешу original_title')
    assert.strictEqual(movieUpd.percent, 100)
    assert.strictEqual(movieUpd.received, true)
    assert.strictEqual(movieUpd.updated, 1750000000 * 1000)
    assert.strictEqual(movieUpd.time, 7200)

    // эпизоды: s1e2 — watched; s11e2 — частичный (22%), хеш с двоеточием
    const epUpd = calls.timelineUpdates.find(u => u.hash === lampaHash('12Show Name'))
    assert.ok(epUpd, 'таймлайн эпизода s01e02 (сезон<=10: без двоеточия)')
    assert.strictEqual(epUpd.percent, 100)

    const epPart = calls.timelineUpdates.find(u => u.hash === lampaHash('11:2Show Name'))
    assert.ok(epPart, 'таймлайн эпизода s11e02 (сезон>10: с двоеточием)')
    assert.strictEqual(epPart.percent, 22, 'частичный: 600с/2700с → 22%')

    // история: фильм и шоу, свежие первыми (шоу 1750000100 > фильма 1750000000)
    assert.ok(calls.favoriteAdds.some(f => f.type === 'history' && f.card.id === 100), 'фильм в «Историю»')
    assert.ok(calls.favoriteAdds.some(f => f.type === 'history' && f.card.id === 500), 'шоу в «Историю»')
    assert.ok(calls.favoriteAdds.every(f => f.limit === 100))
    assert.strictEqual(calls.favoriteAdds[calls.favoriteAdds.length - 2].card.id, 500, 'свежее (шоу) — первым')

    assert.ok(calls.noty.some(n => n.text.includes('plex_sync_done')), 'итог импорта показан')
    console.log('✓ Plex→Lampa: таймлайн фильм+эпизоды (received, штампы) + «История» по свежести')
}

// --- 6. LWW: локальная отметка новее — не затирается
{
    const hash = lampaHash('Kholop')
    state.storage.__tl = { [hash]: { percent: 100, time: 7200, duration: 7200, profile: 0, updated: Date.now() + 1e9 } }
    const before = calls.timelineUpdates.length

    param('plex_sync_now').onChange()

    assert.ok(!calls.timelineUpdates.slice(before).some(u => u.hash === hash), 'локальная отметка новее — не перезаписана')
    console.log('✓ LWW: локальная отметка новее входящей — не затёрта')
}

// --- 7. формула хеша эпизода (регрессия формулы из Timeline Lampa)
{
    assert.strictEqual([1, '', 2, 'Name'].join(''), '12Name', 'сезон 1 — без двоеточия')
    assert.strictEqual([11, ':', 2, 'Name'].join(''), '11:2Name', 'сезон 11 — с двоеточием')
    assert.notStrictEqual(lampaHash('12Show Name'), lampaHash('11:2Show Name'), 'разные сезоны — разные хеши')
    console.log('✓ формула хеша эпизода совпадает с Timeline Lampa')
}

console.log('\nВСЕ СМОУК-ТЕСТЫ ПРОЙДЕНЫ')
