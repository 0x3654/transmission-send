// Смоук-тест плагина transmission-send.js со стабами Lampa API
const fs = require('fs')
const vm = require('vm')
const assert = require('assert')

const source = fs.readFileSync(__dirname + '/transmission-send.js', 'utf8')

// --- стабы
const calls = { noty: [], fetch: [], copied: null, anchorClicked: 0, toggled: null }
const listeners = {}
const storage = {}
const fetchBodies = []
let fetchScript = [] // очередь ответов

const sandbox = {
    console, setTimeout,
    btoa: (s) => Buffer.from(s, 'binary').toString('base64'),
    navigator: {},
    document: {
        createElement: () => ({ style: {}, setAttribute(){}, click(){ calls.anchorClicked++ }, remove(){} }),
        body: { appendChild(){} }
    },
    fetch: (url, opts) => {
        calls.fetch.push({ url, headers: opts.headers })
        fetchBodies.push(opts.body)
        return Promise.resolve(fetchScript.shift())
    },
    window: null
}
sandbox.window = sandbox
sandbox.window.location = { protocol: 'https:' }
sandbox.window.appready = true

sandbox.Lampa = {
    Lang: { add(){}, translate: (k) => k },
    Storage: { field: (k) => storage[k] },
    SettingsApi: { components: [], params: [], addComponent(d){ this.components.push(d) }, addParam(d){ this.params.push(d) } },
    Noty: { show(text, params){ calls.noty.push({ text, params }) } },
    Listener: { follow(type, fn){ (listeners[type] = listeners[type] || []).push(fn) } },
    Controller: { enabled: () => ({ name: 'torrents' }), toggle(name){ calls.toggled = name } },
    Platform: { is: () => false, macOS: () => false, desktop: () => false },
    Utils: { copyTextToClipboard(text, ok){ calls.copied = text; ok() } }
}

vm.createContext(sandbox)
vm.runInContext(source, sandbox)

const fire = (type, e) => (listeners[type] || []).forEach(fn => fn(e))

// --- 1. регистрация
assert.strictEqual(sandbox.Lampa.SettingsApi.components.length, 1, 'addComponent')
assert.strictEqual(sandbox.Lampa.SettingsApi.params.length, 5, 'addParam x5')
assert.strictEqual(listeners['torrent'].length, 1, 'хук torrent')
assert.strictEqual(listeners['torrent_file'].length, 1, 'хук torrent_file')
console.log('✓ регистрация: компонент, 5 параметров, оба хука')

// --- 2. меню: раздача с магнетом (без .torrent Link)
let menu = []
fire('torrent', { type:'onlong', menu, element: { MagnetUri: 'magnet:?xt=urn:btih:ABC123', Title: 'Movie 1080p' } })
assert.strictEqual(menu.length, 2, 'copy + add (download скрыт: нет http Link; open скрыт: не macOS)')
assert.ok(menu[0].title.includes('menu_copy') && menu[1].title.includes('menu_add'))
assert.ok(menu[1].subtitle.includes('subtitle_no_rpc'), 'подзаголовок сообщает что RPC не настроен')
console.log('✓ меню для раздачи с магнетом: copy | add')

// --- 3. меню: раздача только с http Link (.torrent) — магнет построить нельзя
menu = []
fire('torrent', { type:'onlong', menu, element: { Link: 'https://tr.example/dl/123.torrent', Title: 'X' } })
assert.strictEqual(menu.length, 1, 'только download')
assert.ok(menu[0].title.includes('menu_download'))
console.log('✓ меню для раздачи только с .torrent-Link: download')

// --- 4. macOS: добавляется «Открыть магнет»
sandbox.Lampa.Platform.macOS = () => true
menu = []
fire('torrent', { type:'onlong', menu, element: { MagnetUri: 'magnet:?x=1' } })
assert.strictEqual(menu.length, 3, 'copy + open + add')
sandbox.Lampa.Platform.macOS = () => false
console.log('✓ на macOS добавлен пункт «Открыть магнет»')

// --- 5. torrent_file: пункты из info-hash
menu = []
fire('torrent_file', { type:'onlong', menu, element: { torrent_hash: 'DEADBEEF', path_human: 'file.mkv' } })
assert.strictEqual(menu.length, 2, 'copy + add')
console.log('✓ torrent_file: пункты из info-hash')

// --- 6. действие download: anchor click + возврат фокуса
menu = []
fire('torrent', { type:'onlong', menu, element: { Link: 'https://tr.example/dl/1.torrent' } })
menu[0].onSelect()
assert.strictEqual(calls.anchorClicked, 1, 'клик по <a download>')
assert.strictEqual(calls.toggled, 'torrents', 'Controller.toggle вернул фокус')
console.log('✓ скачивание: anchor click + возврат фокуса')

// --- 7. действие copy (фолбэк через Lampa.Utils)
menu = []
fire('torrent', { type:'onlong', menu, element: { MagnetUri: 'magnet:?xt=1' } })
menu[0].onSelect()
assert.strictEqual(calls.copied, 'magnet:?xt=1', 'магнет ушёл в буфер')
console.log('✓ копирование магнета работает')

// --- 8. RPC: mixed content (https страница → http url)
storage.transmission_rpc_url = 'http://192.168.1.10:9091'
menu = []
fire('torrent', { type:'onlong', menu, element: { MagnetUri: 'magnet:?x=1' } })
menu[menu.length - 1].onSelect()
assert.ok(calls.noty[calls.noty.length - 1].text.includes('err_mixed'), 'именованная ошибка mixed content')
assert.ok(calls.noty[calls.noty.length - 1].text.includes('err_hint'), 'с подсказкой')
console.log('✓ RPC http с https-страницы → err_mixed + подсказка')

// --- 9. RPC: успешный 409-handshake
storage.transmission_rpc_url = 'https://192.168.1.10:9091/transmission/rpc/'
storage.transmission_rpc_user = 'user'
storage.transmission_rpc_password = 'pass'
storage.transmission_labels = 'lampa, film'

fetchScript = [
    { status: 409, headers: { get: () => 'SESSION42' } },
    { status: 200, json: () => Promise.resolve({ result: 'success', arguments: { 'torrent-added': { name: 'Ubuntu ISO' } } }) }
]

calls.noty = []
menu = []
fire('torrent', { type:'onlong', menu, element: { MagnetUri: 'magnet:?x=2', Title: 'T' } })
menu[menu.length - 1].onSelect()

setTimeout(() => {
    assert.strictEqual(calls.fetch.length, 2, 'два запроса: 409 → повтор с session-id')
    assert.strictEqual(calls.fetch[0].url, 'https://192.168.1.10:9091/transmission/rpc', 'нормализация URL')
    assert.ok(calls.fetch[0].headers['Authorization'].startsWith('Basic '), 'basic auth')
    assert.strictEqual(calls.fetch[1].headers['X-Transmission-Session-Id'], 'SESSION42', 'session-id передан')
    const body = JSON.parse(fetchBodies[0])
    assert.strictEqual(body.method, 'torrent-add')
    assert.strictEqual(body.arguments.filename, 'magnet:?x=2')
    assert.deepStrictEqual(body.arguments.labels, ['lampa', 'film'], 'labels распарсены')
    const ok = calls.noty.find(n => n.params && n.params.style === 'success')
    assert.ok(ok && ok.text.includes('Ubuntu ISO'), 'успешная Noty с именем')
    console.log('✓ RPC: 409-handshake, auth, labels, success-noty')

    // --- 10. RPC: дубликат
    fetchScript = [{ status: 200, json: () => Promise.resolve({ result: 'duplicate torrent', arguments: {} }) }]
    calls.noty = []
    menu = []
    fire('torrent', { type:'onlong', menu, element: { MagnetUri: 'magnet:?x=3' } })
    menu[menu.length - 1].onSelect()
    setTimeout(() => {
        const last = calls.noty[calls.noty.length - 1]
        assert.ok(last.text.includes('dup'), 'noty про дубликат')
        assert.ok(!last.text.includes('err_hint'), 'без подсказки про фолбэк')
        console.log('✓ RPC: дубликат обработан')

        // --- 11. RPC: CORS-сеть (fetch reject)
        sandbox.fetch = () => Promise.reject(new TypeError('Failed to fetch'))
        calls.noty = []
        menu = []
        fire('torrent', { type:'onlong', menu, element: { MagnetUri: 'magnet:?x=4' } })
        menu[menu.length - 1].onSelect()
        setTimeout(() => {
            const last = calls.noty[calls.noty.length - 1]
            assert.ok(last.text.includes('err_network'), 'именованная ошибка сети')
            console.log('✓ RPC: сетевая ошибка → err_network')
            console.log('\nВСЕ СМОУК-ТЕСТЫ ПРОЙДЕНЫ ▶', calls.noty.length, 'noty всего')
        }, 10)
    }, 10)
}, 10)
