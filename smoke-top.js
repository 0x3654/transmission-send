// Смоук-тест плагина top.js: регистрация, «Топ» (TMDB+окна дат), «Топ трекеров»
// (настройки: качество/озвучки/CAM/просмотренные), пресеты «Мой фильтр», «Топ» вместо главной
const fs = require('fs')
const vm = require('vm')
const assert = require('assert')

const source = fs.readFileSync(__dirname + '/top.js', 'utf8')

const calls = { menu: [], components: {}, params: [], push: [], replace: [], selects: [], noty: [],
                settings: [], tmdb: [], urls: [], inputs: [] }
const listeners = {}
const state = { storage: {}, fields: {}, tmdbResponse: null, serverJson: null, findJson: null, favorite: {} }

function InteractionCategory(object){
    this.object = object
    this.activity = { loader(){}, toggle(){} }
    // фейковый DOM: карточка с .card__view (appendChild/querySelector) —
    // trackCards вешает туда плашки качества и «просмотрено»
    const body = { children: [], removeChild(c){ this.children = this.children.filter(x => x !== c) } }
    this.append = (data) => {
        (data && data.results || []).forEach((r) => {
            const view = {
                children: [],
                appendChild(c){ this.children.push(c) },
                querySelector(sel){ return this.children.find(c => (' ' + (c.className || '') + ' ').includes(sel)) || null }
            }
            body.children.push({ id: r.id, querySelector: (sel) => sel === '.card__view' ? view : null })
        })
        this.built = (this.built || []).concat((data && data.results) || [])
    }
    this.topDomIds = () => body.children.map(c => c.id)
    this.build = (data) => { this.append(data) } // как в реальном классе: build зовёт append
    this.empty = () => { this.emptied = true }
    this.render = () => ({ querySelector: (sel) => (sel === '.category-full' ? body : null) })
    this.topDomCards = () => body.children
}

function Reguest(){
    this.timeout = () => {}
    this.silent = (url, ok, fail) => {
        calls.urls.push(url)
        if(url.includes('/feed')) {
            if(state.feedJson) ok(state.feedJson)
            else fail({})
        }
        else if(url.includes('/findbatch')) {
            calls.findBatches = calls.findBatches || []
            calls.findBatches.push(url)
            if(state.findBatchJson) ok(state.findBatchJson)
            else fail({})
        }
        else if(url.includes('/top')) {
            const m = url.match(/[&?]page=(\d+)/)
            const pg = m ? +m[1] : 0
            if(pg > 0 && state.serverJson && state.serverJson.items){
                const slice = 2
                const all = state.serverJson.items
                ok({ page: pg, total_pages: Math.ceil(all.length / slice), items: all.slice((pg - 1) * slice, pg * slice) })
            } else if(state.serverJson) ok(state.serverJson)
            else fail({})
        }
        else if(url.includes('/find')) {
            if(state.findJson) ok(state.findJson)
            else fail({})
        }
        else if(state.serverJson){
            const m = url.match(/[&?]page=(\d+)/)
            const pg = m ? +m[1] : 0
            if(pg > 0 && state.serverJson.items){
                const slice = 2 // мелкий срез для теста
                const all = state.serverJson.items
                ok({ page: pg, total_pages: Math.ceil(all.length / slice), items: all.slice((pg - 1) * slice, pg * slice) })
            } else ok(state.serverJson)
        }
        else fail({})
    }
}

const sandbox = {
    console, setTimeout,
    navigator: {},
    document: {
        createElement: () => ({ style: {}, textContent: '', className: '', children: [], appendChild(c){ this.children.push(c) }, setAttribute(){}, remove(){}, click(){} }),
        head: { appendChild(){} },
        body: { appendChild(){} }
    },
    window: null
}
sandbox.window = sandbox
sandbox.appready = true

sandbox.Lampa = {
    Lang: { add(){}, translate: (k) => k },
    Noty: { show(text, params){ calls.noty.push({ text, params }) } },
    Listener: { follow(type, fn){ (listeners[type] = listeners[type] || []).push(fn) } },
    Component: {
        add(name, cls){ calls.components[name] = cls },
        get(name){ return calls.components[name] }
    },
    Menu: { addButton(svg, title, cb){ calls.menu.push({ title, cb }) } },
    SettingsApi: {
        addComponent(c){ calls.settingsComponent = c },
        addParam(p){ calls.params.push(p) }
    },
    Settings: { create(name){ calls.settings.push(name) } },
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
    Activity: {
        push(a){ calls.push.push(a) },
        replace(a){ calls.replace.push(a) },
        active(){ return { component: 'top_screen' } }
    },
    Select: { show(opts){ calls.selects.push(opts) }, close(){} },
    Input: { edit(opts, cb){ calls.inputs.push({ opts, cb }) } },
    Platform: { tv: () => false },
    Favorite: {
        get(params){ return state.favorite[params.type] || [] }
    },
    Controller: { toggle(){} },
    Reguest,
    InteractionCategory,
    Api: {
        sources: {
            tmdb: {
                get(method, params, ok, fail){
                    calls.tmdb.push({ method, params })
                    if(state.tmdbResponse) ok(state.tmdbResponse)
                    else fail({})
                }
            }
        }
    }
}

vm.createContext(sandbox)
vm.runInContext(source, sandbox)

const fire = (type, e) => (listeners[type] || []).forEach(fn => fn(e))

// --- 1. регистрация + формат параметров (input обязан иметь values:'string')
assert.ok(calls.components['top_screen'] && calls.components['top_trackers'])
assert.strictEqual(calls.menu.length, 2)
assert.deepStrictEqual(calls.params.map(p => p.param.name),
    ['top_server_url', 'top_as_home', 'top_min_quality', 'top_voice_1', 'top_voice_2', 'top_hide_watched', 'top_trackers_only', 'top_hide_series', 'top_ru_titles', 'top_no_cam'])
assert.strictEqual(calls.params[0].param.values, 'string', 'input обязан иметь values:string (иначе краш настроек Lampa)')
{
    const sel = calls.params[2].param.values
    assert.strictEqual(Object.keys(sel).sort().join(','), '1080,2160,720,any', 'select — объект ключ→название')
}
console.log('✓ регистрация: 2 экрана, 2 пункта меню (Топ · TMDB, Топ · трекеры), параметры с маркером input')

// --- 2. варианты с окном дат: 14 дней подставляет primary_release_date.gte
{
    const comp = new calls.components['top_screen']({ page: 1, top_method: 'discover/movie',
        top_params: { sort_by: 'popularity.desc', 'vote_count.gte': 50,
                      'primary_release_date.gte': new Date(Date.now() - 14 * 86400000).toISOString().slice(0, 10) } })
    state.tmdbResponse = { results: [], total_pages: 1 }
    comp.create()
    const p = calls.tmdb[0].params
    assert.ok(p['primary_release_date.gte'], 'дата окна подставлена')
    const days = (Date.now() - new Date(p['primary_release_date.gte'] + 'T00:00:00Z')) / 86400000
    assert.ok(days >= 13.9 && days <= 15.1, 'окно ~14 дней: ' + days)
}
console.log('✓ варианты 14/30 дней: окно дат считается при открытии')

// --- 3. «Топ»: меню пушит последний вариант, пагинация
state.storage.top_last_variant = '7'
calls.menu[0].cb()
assert.strictEqual(calls.push[calls.push.length - 1].title, 'Топ · TMDB', 'заголовок экрана — как в меню')
assert.strictEqual(calls.push[calls.push.length - 1].top_method, 'discover/movie')
let comp = new calls.components['top_screen']({ page: 1, top_method: 'trending/movie/week', top_params: null })
state.tmdbResponse = { results: [{ id: 1, title: 'А', media_type: 'movie' }], total_pages: 7 }
comp.create()
let resolved = null
comp.nextPageReuest({ page: 3 }, (json) => { resolved = json }, () => {})
assert.strictEqual(calls.tmdb[calls.tmdb.length - 1].params.page, 3)
assert.strictEqual(resolved.results[0].id, 1)
console.log('✓ TopScreen: последний вариант из Storage + пагинация')

// память батча персистится в Storage
assert.deepStrictEqual(state.storage.top_batch_found, undefined)
// --- 3a. «Топ · TMDB» через сервер: /feed, качество сразу, фолбэк при отказе
state.fields.top_server_url = 'http://10.1.1.1:8355'
state.favorite = { history: [{ id: 999, title: 'Старый', release_date: '2020-01-01' }] }
state.feedJson = { page: 1, total_pages: 3, results: [
    { id: 900, title: 'Серверная карточка', release_date: '2026-01-01', media_type: 'movie', quality: '4K', voice: 'Дубляж' },
    { id: 901, title: 'Ещё серверная', release_date: '2026-01-01', media_type: 'movie', quality: '1080p', voice: 'Red Head Sound' }
] }
{
    const tmdbCalls0 = calls.tmdb.length
    const c0 = new calls.components['top_screen']({ page: 1, top_variant: 'movie_week', top_method: 'trending/movie/week', top_params: null })
    c0.create()
    assert.deepStrictEqual(c0.built.map(r => r.id), [900, 901], 'страница из /feed')
    assert.strictEqual(calls.tmdb.length, tmdbCalls0, 'прямой TMDB не зовётся')
    const card = c0.topDomCards()[0]
    const view = card.querySelector('.card__view')
    const q = view.children.find(c => (c.className || '').includes('card__quality'))
    assert.ok(q && q.children[0].textContent === '4K', 'плашка качества сразу (4K)')
    const vb = view.children.find(c => (c.className || '').includes('top-badge--voice'))
    assert.ok(vb && vb.textContent === 'ДБ', 'плашка перевода справа: Дубляж → ДБ')
    const card2 = c0.topDomCards()[1]
    const vb2 = card2.querySelector('.card__view').children.find(c => (c.className || '').includes('top-badge--voice'))
    assert.ok(vb2 && /^Red Head ?$/.test(vb2.textContent), 'студия сокращается: ' + (vb2 && vb2.textContent))
    assert.ok(calls.urls.some(u => u.includes('/feed?variant=movie_week&page=1')), '/feed с ключом варианта')
    assert.ok(calls.urls.some(u => u.includes('exclude=')), 'просмотренные id уходят серверу (страница после фильтра)')
}
// фолбэк: сервер упал — прямой TMDB
state.feedJson = null
{
    const c0b = new calls.components['top_screen']({ page: 1, top_variant: 'movie_week', top_method: 'trending/movie/week', top_params: null })
    c0b.create()
    assert.ok(c0b.built.length >= 1, 'фолбэк на прямой TMDB отработал')
}
state.feedJson = { page: 1, total_pages: 1, results: [] }
console.log('✓ «Топ · TMDB» через сервер /feed: качество сразу, прямой TMDB не тратится, фолбэк работает')

// --- 3a2. авто-добор: просмотренные вырезали страницу — клиент тянет следующую
state.favorite = { history: [
    { id: 10, title: 'Просмотренный А', release_date: '2026-01-01' },
    { id: 11, title: 'Просмотренный Б', release_date: '2026-01-01' }
] }
state.feedJson = null
state.storage.top_batch_found = undefined
state.tmdbResponse = { results: [
    { id: 10, title: 'Просмотренный А', release_date: '2026-01-01', media_type: 'movie' },
    { id: 11, title: 'Просмотренный Б', release_date: '2026-01-01', media_type: 'movie' },
    { id: 12, title: 'Свежий', release_date: '2026-01-01', media_type: 'movie' }
], total_pages: 2 }
state.favorite = { history: [
    { id: 10, title: 'Просмотренный А', release_date: '2026-01-01' },
    { id: 11, title: 'Просмотренный Б', release_date: '2026-01-01' }
] }
state.fields.top_hide_watched = 'true'
{
    let page2done = false
    const origTmdb = state.tmdbResponse
    // страница 2 придёт по второму tmdb-запросу
    const c = new calls.components['top_screen']({ page: 1, top_method: 'trending/movie/week', top_params: null })
    let calls2 = 0
    const realGet = sandbox.Lampa.Api.sources.tmdb.get
    sandbox.Lampa.Api.sources.tmdb.get = (m, p, ok, f) => {
        calls2++
        if(p.page >= 2) ok({ results: [
            { id: 20, title: 'Добор один', release_date: '2026-01-01', media_type: 'movie' },
            { id: 21, title: 'Добор два', release_date: '2026-01-01', media_type: 'movie' },
            { id: 22, title: 'Добор три', release_date: '2026-01-01', media_type: 'movie' },
            { id: 23, title: 'Добор четыре', release_date: '2026-01-01', media_type: 'movie' },
            { id: 24, title: 'Добор пять', release_date: '2026-01-01', media_type: 'movie' },
            { id: 25, title: 'Добор шесть', release_date: '2026-01-01', media_type: 'movie' },
            { id: 26, title: 'Добор семь', release_date: '2026-01-01', media_type: 'movie' },
            { id: 27, title: 'Добор восемь', release_date: '2026-01-01', media_type: 'movie' },
            { id: 28, title: 'Добор девять', release_date: '2026-01-01', media_type: 'movie' }
        ], total_pages: 2 })
        else realGet(m, p, ok, f)
    }
    c.create()
    sandbox.Lampa.Api.sources.tmdb.get = realGet
    assert.ok(calls2 >= 2, 'авто-добор страницы 2 (' + calls2 + ')')
    assert.deepStrictEqual(c.built.map(r => r.id), [12, 20, 21, 22, 23, 24, 25, 26, 27, 28], 'экран заполнен: свежие + добор')
}
state.favorite = {}
console.log('✓ авто-добор: просмотренные вырезали — клиент докачал страницу 2 до заполнения')

// --- 3b. (прямой TMDB-путь: фолбэк — /feed недоступен)
state.feedJson = null

// --- 3b. «Топ · TMDB» неблокирующий фильтр раздач: экран строится сразу,
// батч фоном; при found=false экран пересобирается по памяти плагина
state.fields.top_server_url = 'http://10.1.1.1:8355'
state.fields.top_trackers_only = 'true'
state.fields.top_min_quality = 'any'
state.fields.top_no_cam = 'true'
state.fields.top_ru_titles = 'true'
state.fields.top_voice_1 = 'any'
state.fields.top_voice_2 = 'any'
const mkResults = () => ({ results: [
    { id: 1, title: 'Есть раздача', original_title: 'With Release', release_date: '2026-01-01', media_type: 'movie' },
    { id: 2, title: 'Нет раздачи', original_title: 'No Release', release_date: '2026-01-01', media_type: 'movie' },
    { id: 4, title: 'No Localization Here', original_title: 'No Loc', release_date: '2026-01-01', media_type: 'movie' }
], total_pages: 1 })
state.tmdbResponse = mkResults()
state.findBatchJson = { found: [true, true] }
{
    const c2 = new calls.components['top_screen']({ page: 1, top_method: 'trending/movie/week', top_params: null })
    c2.create()
    assert.deepStrictEqual(c2.built.map(r => r.id), [1, 2], 'экран построен мгновенно: ру-карточки показаны, латиница скрыта')
    assert.strictEqual(calls.replace.length, 0, 'все найдены — пересборка не нужна')
}
// страница задрейфовала: известные 1,2 + НОВАЯ карточка 5 без раздачи
const driftResults = () => ({ results: [
    { id: 1, title: 'Есть раздача', release_date: '2026-01-01', media_type: 'movie' },
    { id: 2, title: 'Нет раздачи', release_date: '2026-01-01', media_type: 'movie' },
    { id: 5, title: 'Свежий дрейф без раздачи', release_date: '2026-01-01', media_type: 'movie' }
], total_pages: 1 })
state.tmdbResponse = driftResults()
state.findBatchJson = { found: [false], quality: [''] } // батч спросит только неизвестный id 5
{
    const c3 = new calls.components['top_screen']({ page: 1, top_method: 'trending/movie/week', top_params: null })
    c3.create()
    assert.strictEqual(calls.replace.length, 0, 'моргания нет: экран НЕ перезапущен')
    assert.deepStrictEqual(c3.topDomIds(), [1, 2], 'карточка без раздачи удалена точечно из DOM построенного экрана')
    calls.replace.length = 0 // сбросим для следующих проверок
    const batchUrl = calls.findBatches[calls.findBatches.length - 1]
    assert.ok(batchUrl.startsWith('https://10.1.1.1:8355/findbatch?items='), 'батч GET на нужный эндпоинт')
    assert.ok(decodeURIComponent(batchUrl).includes('Свежий дрейф'), 'батч спросил только неизвестный id')
    // память записана в Storage с ключом фильтров
assert.ok(state.storage.top_batch_found && state.storage.top_batch_found.map && state.storage.top_batch_found.map[5] === false, 'batchFound персистится c ключом фильтров')

// повторная сборка (переоткрытие): память уже знает id 5 — фильтр без запроса
    const before = calls.findBatches.length
    state.tmdbResponse = driftResults()
    const c3b = new calls.components['top_screen']({ page: 1, top_method: 'trending/movie/week', top_params: null })
    c3b.create()
    assert.deepStrictEqual(c3b.built.map(r => r.id), [1, 2], 'повторная сборка отфильтрована по памяти (id 5 скрыт)')
    assert.strictEqual(calls.findBatches.length, before, 'нового батча не потребовалось')
    assert.strictEqual(calls.replace.length, 0, 'циклов пересборки нет')
}
// тумблер выключен — батч вообще не зовётся
state.fields.top_trackers_only = 'false'
state.tmdbResponse = mkResults()
{
    const before = calls.findBatches.length
    const c4 = new calls.components['top_screen']({ page: 1, top_method: 'trending/movie/week', top_params: null })
    c4.create()
    assert.deepStrictEqual(c4.built.map(r => r.id), [1, 2], 'латиница скрыта ру-фильтром, раздачи не проверяются')
    assert.strictEqual(calls.findBatches.length, before, 'без тумблера батч не зовётся')
}
state.fields.top_trackers_only = 'true'
console.log('✓ «Только с раздачами» неблокирующе: экран сразу, батч фоном, пересборка по памяти без цикла')

// --- 4. «Топ трекеров»: мусорный sort в Storage не ломает запрос
state.storage.top_trackers_sort = 'undefined'
{
    const c7 = new calls.components['top_trackers']({ page: 1 })
    c7.create()
    assert.ok(calls.urls[calls.urls.length - 1].includes('sort=seeds'), 'невалидный sort заменён на seeds')
}
delete state.storage.top_trackers_sort

// --- 4b. «Топ трекеров»: качество + ДВЕ озвучки + CAM в запросе
state.fields.top_min_quality = '1080'
state.fields.top_no_cam = 'true'
state.fields.top_voice_1 = 'Дубляж'
state.fields.top_voice_2 = 'LostFilm'
state.fields.top_hide_watched = 'false'
state.serverJson = { items: [
    { ru: 'Холоп 3', orig: '', year: 2026, season: false, quality: '2160', dub: true },
    { ru: 'Сборник софта', orig: '', year: 2021, season: false, quality: 'sd' }
] } // страница 1 = первые 2, total_pages = 1
state.tmdbResponse = { results: [
    { id: 100, title: 'Холоп 3', release_date: '2026-01-01', media_type: 'movie', popularity: 50 }
] }
comp = new calls.components['top_trackers']({ page: 1 })
comp.create()

{
    const url = calls.urls[calls.urls.length - 1]
    assert.ok(url.includes('page=1'), 'первая страница запрошена явно')
    assert.ok(url.includes('minq=1080') && url.includes('junk=1'), 'качество и CAM-фильтр')
    assert.ok(url.includes('ru=1'), 'русский фильтр по умолчанию')
    const voice = decodeURIComponent((url.match(/voice=([^&]*)/) || [])[1] || '')
    assert.strictEqual(voice, 'Дубляж,LostFilm', 'две озвучки одной строкой')
}
assert.strictEqual(comp.built.length, 1, 'софт отсеян матчингом')
// плашка качества: DOM-стаб карточки получил .card__quality (класс нативный)
{
    const card = comp.topDomCards()[0]
    const view = card.querySelector('.card__view')
    const q = view.children.find(c => (c.className || '').includes('card__quality'))
    assert.ok(q, 'плашка качества на карточке')
    assert.strictEqual(q.children[0].textContent, '4K', 'текст плашки 4K (2160 → 4K)')
    const vb = view.children.find(c => (c.className || '').includes('top-badge--voice'))
    assert.ok(vb && vb.textContent === 'ДБ', 'плашка перевода у трекеров: dub → ДБ')
}
console.log('✓ «Топ трекеров»: minq + junk + две озвучки + бейдж качества')

// --- 4b2. постраничность: nextPageReuest тянет следующую страницу базы
state.serverJson = { items: [
    { ru: 'А', orig: '', year: 2026, season: false, quality: '1080' },
    { ru: 'Б', orig: '', year: 2026, season: false, quality: '1080' },
    { ru: 'В', orig: '', year: 2026, season: false, quality: '1080' }
] }
state.tmdbResponse = { results: [] } // ничто не сматчится — страница пустая, но запрос ушёл
{
    const cp = new calls.components['top_trackers']({ page: 1 })
    cp.create()
    let resolved2 = null
    cp.nextPageReuest({ page: 2 }, (json) => { resolved2 = json }, () => {})
    assert.ok(calls.urls[calls.urls.length - 1].includes('page=2'), 'вторая страница базы запрошена')
    assert.strictEqual(resolved2 && resolved2.total_pages, 2, 'total_pages от серверной базы')
}
console.log('✓ «Топ трекеров» постранично: база сервера листается, экран не сбрасывается')

// --- 4c. «Топ · трекеры»: единый «Только на русском» — латинская карточка скрыта
state.serverJson = { items: [
    { ru: 'Джек Ричер', orig: 'Reacher', year: 2026, season: true, quality: '1080' },
    { ru: 'Холоп 3', orig: '', year: 2026, season: false, quality: '2160' }
] }
state.tmdbResponse = { results: [
    { id: 500, name: 'Reacher', first_air_date: '2026-01-01', media_type: 'tv', popularity: 90 },       // нет ру-локализации
    { id: 100, title: 'Холоп 3', release_date: '2026-01-01', media_type: 'movie', popularity: 50 }
] }
{
    const c8 = new calls.components['top_trackers']({ page: 1 })
    c8.create()
    assert.deepStrictEqual(c8.built.map(r => r.id), [100], 'латинская карточка (Reacher) скрыта единым тумблером')
    assert.ok(calls.urls[calls.urls.length - 1].includes('ru=1'), 'серверный ru= от того же тумблера')
}
state.fields.top_ru_titles = 'false'
{
    const c9 = new calls.components['top_trackers']({ page: 1 })
    c9.create()
    assert.deepStrictEqual(c9.built.map(r => r.id).sort(), [100, 500], 'выключен — латинская карточка видна, ru=0 в запросе')
    assert.ok(calls.urls[calls.urls.length - 1].includes('ru=0'))
}
state.fields.top_ru_titles = 'true'

// --- 5. озвучки выключены → voice в запросе нет
state.fields.top_voice_1 = 'any'
state.fields.top_voice_2 = 'any'
comp = new calls.components['top_trackers']({ page: 1, top_sort: 'top' })
comp.create()
assert.ok(!calls.urls[calls.urls.length - 1].includes('voice='))
assert.ok(calls.urls[calls.urls.length - 1].includes('sort=top') && calls.urls[calls.urls.length - 1].includes('pages=6'))
console.log('✓ озвучки выключены; классика: sort=top, pages=6')

// --- 6. «скрыть просмотренные»: история просмотров/просмотрено/смотрю/брошено,
// двойной ключ: id и «название|год» (ловит tv/movie расхождения)
state.fields.top_hide_watched = 'true'
state.fields.top_trackers_only = 'false' // этот блок — про просмотренных, не про раздачи
state.favorite = {
    history: [{ id: 100, title: 'Холоп 3', release_date: '2026-01-01' }],
    viewed:  [{ id: 200, name: 'Сериал' }],
    look:    [{ id: 400, name: 'Minions & Monsters', first_air_date: '2026-07-01' }], // сериал в «Смотрю»
    thrown:  [{ id: 500, title: 'Брошенное', release_date: '2025-01-01' }]
}
state.tmdbResponse = { results: [
    { id: 100, title: 'Холоп 3', release_date: '2026-01-01', media_type: 'movie', popularity: 50 },
    { id: 300, title: 'Другой', release_date: '2026-01-01', media_type: 'movie', popularity: 50 },
    // сматчился как ФИЛЬМ, а в «Смотрю» лежит сериал — должен скрыться по имени
    { id: 301, title: 'Minions & Monsters', release_date: '2026-07-01', media_type: 'movie', popularity: 60 },
    { id: 302, title: 'Брошенное', release_date: '2025-01-01', media_type: 'movie', popularity: 40 }
] }
comp = new calls.components['top_screen']({ page: 1, top_method: 'trending/movie/week', top_params: null })
comp.create()
assert.deepStrictEqual(comp.built.map(r => r.id), [300], 'history/viewed/look/thrown и tv-movie кейс вырезаны')

// страница 2+ приходит через append напрямую — фильтр обязан работать и там
comp.append({ results: [
    { id: 100, title: 'Холоп 3', release_date: '2026-01-01', media_type: 'movie' }, // просмотренный
    { id: 303, title: 'Свежий со второй страницы', release_date: '2026-01-01', media_type: 'movie' }
] }, true)
assert.ok(comp.built.every(r => r.id !== 100), 'просмотренный не пролез со страницы 2')
assert.ok(comp.built.some(r => r.id === 303), 'новый со страницы 2 добавлен')
state.fields.top_trackers_only = 'true'
console.log('✓ «скрыть просмотренные»: 4 источника + имя-ключ ловит tv/movie')

// --- 8. «Топ» вместо главной (отдельный контекст с включённым тумблером)
{
    const calls2 = { replace: [] }
    const sb = { console, navigator: {}, document: { createElement: () => ({}) }, window: null }
    sb.window = sb
    sb.appready = true
    sb.Lampa = {
        Lang: { add(){}, translate: (k) => k },
        Noty: { show(){} },
        Listener: { follow(){} },
        Component: { add(){}, get(){ return null } },
        Menu: { addButton(){} },
        SettingsApi: { addComponent(){}, addParam(){} },
        Storage: {
            field(name){ return name === 'top_as_home' ? 'true' : undefined },
            get(key, def){ return key === 'top_last_variant' ? '4' : def },
            set(){}
        },
        Activity: { push(){}, replace(a){ calls2.replace.push(a) } }
    }
    vm.createContext(sb)
    vm.runInContext(source, sb)
    assert.strictEqual(calls2.replace.length, 1)
    assert.strictEqual(calls2.replace[0].top_method, 'trending/tv/week')
}
console.log('✓ «Топ» вместо главной: replace последнего варианта')

// --- 9. без адреса сервера → подсказка + настройки
state.fields.top_server_url = ''
comp = new calls.components['top_trackers']({ page: 1 })
comp.create()
assert.ok(calls.noty.some(n => n.text === 'top_need_server'))
assert.strictEqual(calls.settings[0], 'top')
console.log('✓ без адреса сервера: подсказка и экран настроек')

console.log('\nВСЕ СМОУК-ТЕСТЫ ПРОЙДЕНЫ')
