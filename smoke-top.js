// Смоук-тест плагина top.js: регистрация, «Топ» (TMDB), «Топ трекеров» (матчинг)
const fs = require('fs')
const vm = require('vm')
const assert = require('assert')

const source = fs.readFileSync(__dirname + '/top.js', 'utf8')

// --- стабы
const calls = { menu: [], components: {}, params: [], push: [], selects: [], noty: [],
                settings: [], tmdb: [], urls: [] }
const state = { storage: {}, tmdbResponse: null, tmdbJson: null, serverJson: null }

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
    Listener: { follow(){} },
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
    Storage: { field(name){ return state.storage[name] } },
    Activity: { push(a){ calls.push.push(a) } },
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

// --- 1. регистрация
assert.ok(calls.components['top_screen'], 'компонент top_screen')
assert.ok(calls.components['top_trackers'], 'компонент top_trackers')
assert.strictEqual(calls.menu.length, 2, 'два пункта меню')
assert.strictEqual(calls.menu[0].title, 'top_menu_top')
assert.strictEqual(calls.menu[1].title, 'top_menu_trackers')
assert.strictEqual(calls.settingsComponent.component, 'top')
assert.strictEqual(calls.params[0].param.name, 'top_server_url')
console.log('✓ регистрация: 2 экрана, 2 пункта меню, настройки (адрес сервера)')

// --- 2. «Топ»: меню пушит дефолтный вариант
calls.menu[0].cb()
assert.strictEqual(calls.push[0].component, 'top_screen')
assert.strictEqual(calls.push[0].top_method, 'trending/movie/week')
console.log('✓ меню «Топ» открывает trending/movie/week')

// --- 3. TopScreen: загрузка + пагинация
state.tmdbResponse = { results: [{ id: 1, title: 'A', media_type: 'movie' }], total_pages: 7 }
let comp = new calls.components['top_screen']({ page: 1, top_method: 'trending/movie/week', top_params: null })
comp.create()
assert.strictEqual(calls.tmdb[0].method, 'trending/movie/week')
assert.strictEqual(calls.tmdb[0].params.page, 1)
assert.strictEqual(comp.built.results.length, 1)
assert.strictEqual(comp.built.total_pages, 7)

let resolved = null
comp.nextPageReuest({ page: 3 }, (json) => { resolved = json }, () => {})
assert.strictEqual(calls.tmdb[1].params.page, 3, 'пагинация передаёт page')
assert.strictEqual(resolved.results[0].id, 1)
console.log('✓ TopScreen: запрос и пагинация nextPageReuet')

// --- 4. «Топ»: discover-вариант тащит свои параметры
state.tmdbResponse = { results: [], total_pages: 1 }
comp = new calls.components['top_screen']({ page: 1, top_method: 'discover/movie',
    top_params: { sort_by: 'vote_average.desc', 'vote_count.gte': 2000 } })
comp.create()
assert.strictEqual(calls.tmdb[2].params['vote_count.gte'], 2000)
console.log('✓ TopScreen: discover с параметрами варианта')

// --- 5. «Топ»: переключение варианта через onRight → Select → push
comp.onRight()
assert.strictEqual(calls.selects.length, 1, 'Select открыт')
assert.strictEqual(calls.selects[0].items.length, 8, '8 вариантов')
calls.selects[0].onSelect(calls.selects[0].items[4]) // «Фильмы · лучшее»
assert.strictEqual(calls.push[calls.push.length - 1].top_method, 'discover/movie')
console.log('✓ onRight открывает выбор вариантов, push нового экрана')

// --- 6. «Топ трекеров» без адреса сервера → подсказка + настройки
comp = new calls.components['top_trackers']({ page: 1 })
comp.create()
assert.ok(calls.noty.some(n => n.text === 'top_need_server'), 'нотификация про сервер')
assert.strictEqual(calls.settings[0], 'top', 'открыты настройки плагина')
assert.strictEqual(calls.urls.length, 0, 'запросов не было')
console.log('✓ без адреса сервера: подсказка и экран настроек')

// --- 7. «Топ трекеров»: матчинг TMDB
state.storage.top_server_url = 'http://10.1.1.1:8355'
state.serverJson = { items: [
    { ru: 'Холоп 3', orig: '', year: 2026, season: false, seeders: 1698 },
    { ru: 'Джентльмены', orig: 'The Gentlemen', year: 2026, season: true, seeders: 1680 },
    { ru: 'Сборник софта', orig: '', year: 2021, season: false, seeders: 900 },
    { ru: 'Холоп 3', orig: '', year: 2026, season: false, seeders: 500 } // дубль раздачи
] }
state.tmdbResponse = { results: [
    { id: 100, title: 'Холоп 3', release_date: '2026-01-01', media_type: 'movie', popularity: 50 },
    { id: 200, name: 'The Gentlemen', first_air_date: '2024-01-01', media_type: 'tv', popularity: 80 },
    { id: 300, title: 'Носители', release_date: '2007-01-01', media_type: 'movie', popularity: 10 } // слабый матч для «софта»
] }
comp = new calls.components['top_trackers']({ page: 1 })
comp.create()

assert.ok(calls.urls[0].endsWith('/top?cat=video&pages=2'), 'запрос к серверу')
assert.strictEqual(comp.built.results.length, 2, 'холоп + джентльмены, софт отсеян')
assert.strictEqual(comp.built.results[0].id, 100, 'первый — по порядку сидов')
assert.strictEqual(comp.built.results[0].media_type, 'movie')
assert.strictEqual(comp.built.results[1].id, 200)
assert.ok(comp.built.results[0].top && comp.built.results[0].top.seeders === 1698, 'данные раздачи приклеены')
assert.ok(calls.noty.some(n => String(n.text).includes('2/4')), 'нотификация 2/4')
console.log('✓ матчинг: год-фильтр, дедупликация, счётчик совпадений')

// --- 8. «Топ трекеров»: сервер недоступен
state.serverJson = null
comp = new calls.components['top_trackers']({ page: 1 })
comp.create()
assert.ok(comp.emptied, 'пустой экран')
assert.ok(calls.noty.some(n => n.params && n.params.style === 'error'), 'ошибка нотификацией')
console.log('✓ сервер недоступен: empty + error-noty')

console.log('\nВСЕ СМОУК-ТЕСТЫ ПРОЙДЕНЫ')
