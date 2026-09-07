// Смоук-тест плагина top.js: регистрация, «Топ» (TMDB), «Топ трекеров» (настройки+матчинг),
// «Мой фильтр», «Топ» вместо главной
const fs = require('fs')
const vm = require('vm')
const assert = require('assert')

const source = fs.readFileSync(__dirname + '/top.js', 'utf8')

// --- стабы
const calls = { menu: [], components: {}, params: [], push: [], replace: [], selects: [], noty: [],
                settings: [], tmdb: [], urls: [] }
const listeners = {}
const state = { storage: {}, fields: {}, tmdbResponse: null, serverJson: null }

function InteractionCategory(object){
    this.object = object
    this.activity = { loader(){}, toggle(){} }
    this.build = (data) => { this.built = data }
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
        get(key, def){ return key in state.storage ? state.storage[key] : def },
        set(key, v){ state.storage[key] = v }
    },
    Activity: {
        push(a){ calls.push.push(a) },
        replace(a){ calls.replace.push(a) }
    },
    Select: { show(opts){ calls.selects.push(opts) }, close(){} },
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

// --- 1. регистрация
assert.ok(calls.components['top_screen'], 'компонент top_screen')
assert.ok(calls.components['top_trackers'], 'компонент top_trackers')
assert.strictEqual(calls.menu.length, 3, 'три пункта меню')
assert.strictEqual(calls.menu[0].title, 'top_menu_top')
assert.strictEqual(calls.menu[1].title, 'top_menu_trackers')
assert.strictEqual(calls.menu[2].title, 'top_menu_myfilter')
assert.strictEqual(calls.settingsComponent.component, 'top')
assert.deepStrictEqual(calls.params.map(p => p.param.name),
    ['top_server_url', 'top_as_home', 'top_min_quality', 'top_no_cam', 'top_dub_only'])
console.log('✓ регистрация: 2 экрана, 3 пункта меню, 5 параметров настроек')

// --- 2. «Топ»: меню пушит дефолтный вариант, выбор запоминается
state.storage.top_last_variant = '4' // «Фильмы · лучшее»
calls.menu[0].cb()
assert.strictEqual(calls.push[0].top_method, 'discover/movie', 'последний вариант из Storage')
assert.strictEqual(calls.push[0].top_params['vote_count.gte'], 2000)
console.log('✓ меню «Топ» открывает последний использованный вариант')

// --- 3. TopScreen: загрузка + пагинация
state.tmdbResponse = { results: [{ id: 1, title: 'A', media_type: 'movie' }], total_pages: 7 }
let comp = new calls.components['top_screen']({ page: 1, top_method: 'trending/movie/week', top_params: null })
comp.create()
assert.strictEqual(calls.tmdb[0].method, 'trending/movie/week')
assert.strictEqual(comp.built.results.length, 1)

let resolved = null
comp.nextPageReuest({ page: 3 }, (json) => { resolved = json }, () => {})
assert.strictEqual(calls.tmdb[1].params.page, 3, 'пагинация передаёт page')
assert.strictEqual(resolved.results[0].id, 1)
console.log('✓ TopScreen: запрос и пагинация nextPageReuest')

// --- 4. «Топ трекеров»: запрос собирается из настроек
state.fields.top_server_url = 'http://10.1.1.1:8355'
state.fields.top_min_quality = '1080p+'
state.fields.top_no_cam = true
state.fields.top_dub_only = false
state.serverJson = { items: [
    { ru: 'Холоп 3', orig: '', year: 2026, season: false },
    { ru: 'Сборник софта', orig: '', year: 2021, season: false }
] }
state.tmdbResponse = { results: [
    { id: 100, title: 'Холоп 3', release_date: '2026-01-01', media_type: 'movie', popularity: 50 }
] }
comp = new calls.components['top_trackers']({ page: 1 })
comp.create()

assert.ok(calls.urls[0].includes('sort=seeds'), 'сортировка по умолчанию')
assert.ok(calls.urls[0].includes('minq=1080'), 'мин. качество из настроек')
assert.ok(calls.urls[0].includes('junk=1'), 'камрип-фильтр включён')
assert.ok(calls.urls[0].includes('audio=all'), 'дубляж не обязателен')
assert.strictEqual(comp.built.results.length, 1, 'софт отсеян матчингом')
console.log('✓ «Топ трекеров»: настройки качества/CAM/дубляжа уходят в запрос')

// --- 5. выключенные фильтры
state.fields.top_no_cam = false
state.fields.top_dub_only = true
comp = new calls.components['top_trackers']({ page: 1, top_sort: 'top' })
comp.create()
assert.ok(calls.urls[1].includes('junk=0') && calls.urls[1].includes('audio=dub') && calls.urls[1].includes('sort=top'))
console.log('✓ тумблеры: junk=0 / audio=dub / sort=top')

// --- 6. сортировка топа трекеров через onRight
comp.onRight()
assert.strictEqual(calls.selects.length, 1)
calls.selects[0].onSelect(calls.selects[0].items[1]) // «Классика»
assert.strictEqual(calls.push[calls.push.length - 1].top_sort, 'top')
console.log('✓ onRight: выбор сортировки топа трекеров')

// --- 7. «Мой фильтр»: запоминание применённого фильтра каталога
fire('activity', { type: 'create', component: 'category_full', object: { url: 'discover/movie?with_genres=28&vote_average.gte=7', source: 'tmdb' } })
assert.strictEqual(state.storage.top_last_filter.url, 'discover/movie?with_genres=28&vote_average.gte=7')
assert.strictEqual(state.storage.top_last_filter.source, 'tmdb')

fire('activity', { type: 'create', component: 'category_full', object: { url: 'movie/popular', source: 'tmdb' } })
assert.strictEqual(state.storage.top_last_filter.url, 'discover/movie?with_genres=28&vote_average.gte=7', 'не-discover не перезаписывает')
console.log('✓ «Мой фильтр»: запоминает discover-URL применённого фильтра')

// --- 8. «Мой фильтр»: открытие и пустое состояние
calls.menu[2].cb()
assert.strictEqual(calls.push[calls.push.length - 1].component, 'category_full')
assert.strictEqual(calls.push[calls.push.length - 1].url, 'discover/movie?with_genres=28&vote_average.gte=7')

delete state.storage.top_last_filter
calls.menu[2].cb()
assert.ok(calls.noty.some(n => n.text === 'top_my_filter_empty'), 'подсказка при пустом')
console.log('✓ «Мой фильтр»: открывает сохранённый, подсказывает при пустом')

// --- 9. «Топ» вместо главной: отдельный контекст с включённым тумблером
{
    const calls2 = { replace: [] }
    const sb = {
        console, navigator: {}, document: { createElement: () => ({}) }, window: null
    }
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
            field(name){ return name === 'top_as_home' ? true : undefined },
            get(key, def){ return key === 'top_last_variant' ? '2' : def },
            set(){}
        },
        Activity: { push(){}, replace(a){ calls2.replace.push(a) } }
    }
    vm.createContext(sb)
    vm.runInContext(source, sb)
    assert.strictEqual(calls2.replace.length, 1, 'Activity.replace при старте')
    assert.strictEqual(calls2.replace[0].top_method, 'trending/tv/week', 'последний вариант')
}
console.log('✓ «Топ» вместо главной: replace последнего варианта при запуске')

// --- 10. «Топ трекеров» без адреса сервера → подсказка + настройки
state.fields.top_server_url = ''
comp = new calls.components['top_trackers']({ page: 1 })
comp.create()
assert.ok(calls.noty.some(n => n.text === 'top_need_server'))
assert.strictEqual(calls.settings[0], 'top')
console.log('✓ без адреса сервера: подсказка и экран настроек')

console.log('\nВСЕ СМОУК-ТЕСТЫ ПРОЙДЕНЫ')
