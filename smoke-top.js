// Смоук-тест плагина top.js: регистрация, «Топ» (TMDB+окна дат), «Топ трекеров»
// (настройки: качество/озвучки/CAM/просмотренные), пресеты «Мой фильтр», «Топ» вместо главной
const fs = require('fs')
const vm = require('vm')
const assert = require('assert')

const source = fs.readFileSync(__dirname + '/top.js', 'utf8')

const calls = { menu: [], components: {}, params: [], push: [], replace: [], selects: [], noty: [],
                settings: [], tmdb: [], urls: [], inputs: [] }
const listeners = {}
const state = { storage: {}, fields: {}, tmdbResponse: null, serverJson: null, favorite: {} }

function InteractionCategory(object){
    this.object = object
    this.activity = { loader(){}, toggle(){} }
    this.append = (data) => { this.built = (this.built || []).concat((data && data.results) || []) }
    this.build = (data) => { this.append(data) } // как в реальном классе: build зовёт append
    this.empty = () => { this.emptied = true }
}

function Reguest(){
    this.timeout = () => {}
    this.silent = (url, ok, fail) => {
        calls.urls.push(url)
        if(state.serverJson) ok(state.serverJson)
        else fail({})
    }
}

const sandbox = {
    console, setTimeout,
    navigator: {},
    document: { createElement: () => ({}) },
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
        active(){ return null }
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
    ['top_server_url', 'top_as_home', 'top_min_quality', 'top_voice_1', 'top_voice_2', 'top_hide_watched', 'top_russian_only', 'top_no_cam'])
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
state.tmdbResponse = { results: [{ id: 1, title: 'A', media_type: 'movie' }], total_pages: 7 }
comp.create()
let resolved = null
comp.nextPageReuest({ page: 3 }, (json) => { resolved = json }, () => {})
assert.strictEqual(calls.tmdb[calls.tmdb.length - 1].params.page, 3)
assert.strictEqual(resolved.results[0].id, 1)
console.log('✓ TopScreen: последний вариант из Storage + пагинация')

// --- 4. «Топ трекеров»: качество + ДВЕ озвучки + CAM в запросе
state.fields.top_server_url = 'http://10.1.1.1:8355'
state.fields.top_min_quality = '1080'
state.fields.top_no_cam = 'true'
state.fields.top_voice_1 = 'Дубляж'
state.fields.top_voice_2 = 'LostFilm'
state.fields.top_hide_watched = 'false'
state.serverJson = { items: [
    { ru: 'Холоп 3', orig: '', year: 2026, season: false, quality: '2160' },
    { ru: 'Сборник софта', orig: '', year: 2021, season: false, quality: 'sd' }
] }
state.tmdbResponse = { results: [
    { id: 100, title: 'Холоп 3', release_date: '2026-01-01', media_type: 'movie', popularity: 50 }
] }
comp = new calls.components['top_trackers']({ page: 1 })
comp.create()

{
    const url = calls.urls[calls.urls.length - 1]
    assert.ok(url.includes('minq=1080') && url.includes('junk=1'), 'качество и CAM-фильтр')
    assert.ok(url.includes('ru=1'), 'русский фильтр по умолчанию')
    const voice = decodeURIComponent((url.match(/voice=([^&]*)/) || [])[1] || '')
    assert.strictEqual(voice, 'Дубляж,LostFilm', 'две озвучки одной строкой')
}
assert.strictEqual(comp.built.length, 1, 'софт отсеян матчингом')
assert.strictEqual(comp.built[0].quality, '4K', 'бейдж качества на карточке')
console.log('✓ «Топ трекеров»: minq + junk + две озвучки + бейдж качества')

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
    { id: 100, title: 'Холоп 3', release_date: '2026-01-01', media_type: 'movie' },
    { id: 302, title: 'Второй странице тоже фильтр', release_date: '2026-01-01', media_type: 'movie' }
] }, true)
assert.ok(comp.built.every(r => r.id !== 100), 'просмотренный не пролез со страницы 2')
assert.ok(comp.built.some(r => r.id === 302), 'новый со страницы 2 добавлен')
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
