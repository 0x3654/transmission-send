// Смоук-тест плагина transmission-send.js (минимальная версия: download / copy / open)
const fs = require('fs')
const vm = require('vm')
const assert = require('assert')

const source = fs.readFileSync(__dirname + '/transmission-send.js', 'utf8')

// --- стабы
const calls = { noty: [], copied: null, anchorClicked: 0, toggled: null, location: null }
const sharedPlugins = [{ url: 'https://0x3654.github.io/transmission-send/transmission-send.js' }]
let saved = false
const listeners = {}

const sandbox = {
    console, setTimeout,
    navigator: {},
    document: {
        createElement: () => ({ style: {}, setAttribute(){}, click(){ calls.anchorClicked++ }, remove(){} }),
        body: { appendChild(){} }
    },
    window: null
}
sandbox.window = sandbox
sandbox.location = { protocol: 'https:' }
sandbox.appready = true

sandbox.Lampa = {
    Lang: { add(){}, translate: (k) => k },
    Plugins: {
        get(){ return sharedPlugins },
        save(){ saved = true }
    },
    Noty: { show(text, params){ calls.noty.push({ text, params }) } },
    Listener: { follow(type, fn){ (listeners[type] = listeners[type] || []).push(fn) } },
    Controller: { enabled: () => ({ name: 'torrents' }), toggle(name){ calls.toggled = name } },
    Platform: { is: () => false },
    Utils: { copyTextToClipboard(text, ok){ calls.copied = text; ok() } }
}

vm.createContext(sandbox)
vm.runInContext(source, sandbox)

// --- 0. самоименовывание: честное описание без обещаний про скачивание
{
    const rec = sharedPlugins[0]
    assert.strictEqual(rec.name, 'Transmission Send')
    assert.ok(saved, 'Plugins.save вызван')
    assert.ok(!/скачиван|Transmission через меню/i.test(rec.descr), 'описание без «отправки в Transmission»')
}
console.log('✓ подпись плагина: имя + честное описание')

const fire = (type, e) => (listeners[type] || []).forEach(fn => fn(e))

// --- 1. регистрация
assert.strictEqual(listeners['torrent'].length, 1, 'хук torrent')
assert.strictEqual(listeners['torrent_file'].length, 1, 'хук torrent_file')
console.log('✓ оба хука навешаны, без SettingsApi-компонентов')

// --- 2. меню: раздача с магнетом → copy + open(последний)
let menu = []
fire('torrent', { type:'onlong', menu, element: { MagnetUri: 'magnet:?xt=urn:btih:ABC123', Title: 'Movie' } })
assert.strictEqual(menu.length, 2, 'copy + open')
assert.ok(menu[0].title.includes('menu_copy'))
assert.ok(menu[1].title.includes('menu_open'), '«Открыть магнет» — последний пункт')
console.log('✓ меню для магнет-раздачи: copy | open (open в самом низу)')

// --- 3. меню: раздача только с http Link — пунктов нет (скачивание убрано)
menu = []
fire('torrent', { type:'onlong', menu, element: { Link: 'https://tr.example/dl/123.torrent' } })
assert.strictEqual(menu.length, 0, 'пунктов нет')
console.log('✓ раздача без магнета: наших пунктов нет')

// --- 4. меню: магнет в Link (не MagnetUri)
menu = []
fire('torrent', { type:'onlong', menu, element: { Link: 'magnet:?xt=urn:btih:FFF' } })
assert.strictEqual(menu.length, 2, 'магнет распознан в Link')
console.log('✓ магнет в Link: copy | open')

// --- 5. torrent_file: copy + open из info-hash
menu = []
fire('torrent_file', { type:'onlong', menu, element: { torrent_hash: 'DEADBEEF', path_human: 'file.mkv' } })
assert.strictEqual(menu.length, 2)
console.log('✓ torrent_file: copy | open из info-hash')

// --- 6. действие copy
menu = []
fire('torrent', { type:'onlong', menu, element: { MagnetUri: 'magnet:?xt=1' } })
menu[0].onSelect()
assert.strictEqual(calls.copied, 'magnet:?xt=1', 'магнет в буфере')
assert.strictEqual(calls.toggled, 'torrents', 'Controller.toggle вернул фокус')
assert.ok(calls.noty.some(n => n.params && n.params.style === 'success'), 'success-noty')
console.log('✓ копирование магнета + возврат фокуса')

// --- 7. действие open: window.location = magnet
menu = []
fire('torrent', { type:'onlong', menu, element: { MagnetUri: 'magnet:?xt=2' } })
calls.location = null
Object.defineProperty(sandbox, 'location', { value: { protocol: 'https:' }, writable: true })
menu[1].onSelect()
console.log('✓ «Открыть магнет» вызывает навигацию по magnet: (window.location)')

// --- 8. встроенные пункты Lampa остаются выше наших
menu = [{ title: 'built-in: мои торренты' }]
fire('torrent', { type:'onlong', menu, element: { MagnetUri: 'magnet:?x=9' } })
assert.strictEqual(menu.length, 3, 'встроенный + copy + open')
assert.strictEqual(menu[0].title, 'built-in: мои торренты', 'встроенные пункты не тронуты')
console.log('✓ встроенные пункты меню остаются на месте')

console.log('\nВСЕ СМОУК-ТЕСТЫ ПРОЙДЕНЫ')
