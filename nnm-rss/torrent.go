package main

// DHT-резолвер: голый btih из ленты → настоящий .torrent (BEP9 ut_metadata).
// Зачем: nasctl и подобные RSS-клиенты берут <link> только как http(s)-.torrent,
// magnet в link игнорируют (пустой диалог добавления); эталон lostfilmfeed
// отдаёт именно .torrent-ссылки. Метаданные хэша неизменны — файлы кэшируются
// навсегда в DATA_DIR/torrents (том /data).

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

var (
	torrentsDir     string
	selfURL         = env("PUBLIC_URL", "") // http(s)-база сервиса для link на .torrent
	torrentFetchMu  sync.Mutex
	torrentFetching = map[string]bool{}
)

// публичные трекеры вписываем в собранный .torrent: магниты лент — btih без tr=
var publicTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.demonii.com:1337/announce",
	"udp://open.tracker.cl:6969/announce",
	"udp://explodie.org:6969/announce",
}

func initTorrents() {
	torrentsDir = filepath.Join(env("DATA_DIR", "data"), "torrents")
	os.MkdirAll(torrentsDir, 0o755)
}

func torrentPath(hash string) string {
	return filepath.Join(torrentsDir, strings.ToLower(hash)+".torrent")
}

func torrentReady(hash string) bool {
	if hash == "" {
		return false
	}
	st, err := os.Stat(torrentPath(hash))
	return err == nil && st.Size() > 0
}

// prefetchTorrent — фон; к следующему опросу ленты файл появится и link станет .torrent.
// Префетчей мало параллельно: каждый — отдельный torrent-клиент с сокетами.
var prefetchSem = make(chan struct{}, 3)

// waitForTorrent — короткое ожидание готовности файла при сборке ленты:
// свежий айтем чаще всего уезжает клиенту уже с .torrent-ссылкой (nasctl
// заполняет поле URL в «New Task» только http-ссылками, магнет игнорирует)
func waitForTorrent(hash string, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if torrentReady(hash) {
			return
		}
		time.Sleep(400 * time.Millisecond)
	}
}

func prefetchTorrent(hash string) {
	if selfURL == "" || torrentReady(hash) {
		return
	}
	torrentFetchMu.Lock()
	if torrentFetching[hash] {
		torrentFetchMu.Unlock()
		return
	}
	torrentFetching[hash] = true
	torrentFetchMu.Unlock()
	go func() {
		defer func() {
			torrentFetchMu.Lock()
			delete(torrentFetching, hash)
			torrentFetchMu.Unlock()
		}()
		prefetchSem <- struct{}{}
		defer func() { <-prefetchSem }()
		fetchTorrentFile(hash)
	}()
}

// fetchTorrentFile — достаёт метаданные по DHT и сохраняет .torrent
func fetchTorrentFile(hash string) {
	hash = strings.ToLower(hash)
	tmp := filepath.Join(torrentsDir, "_tmp_"+hash[:8])
	os.MkdirAll(tmp, 0o755)
	// конфиг только через NewDefaultClientConfig: литерал без дефолтов паникует
	// в v1.58.0 (nil ListenHost — listenAll, nil DhtStartingNodes — NewAnacrolixDhtServer)
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = tmp
	cfg.ListenPort = 0
	cfg.NoDefaultPortForwarding = true
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		log.Printf("torrent client: %v", err)
		return
	}
	defer cl.Close()
	defer os.RemoveAll(tmp)
	t, err := cl.AddMagnet("magnet:?xt=urn:btih:" + hash)
	if err != nil {
		log.Printf("addmagnet %s: %v", hash[:8], err)
		return
	}
	select {
	case <-t.GotInfo():
	case <-time.After(2 * time.Minute):
		log.Printf("torrent %s: метаданные не получены (DHT-таймаут)", hash[:8])
		t.Drop()
		return
	}
	mi := t.Metainfo()
	t.Drop()
	mi.Announce = publicTrackers[0]
	for _, tr := range publicTrackers {
		mi.AnnounceList = append(mi.AnnounceList, []string{tr})
	}
	raw, err := bencode.Marshal(mi)
	if err != nil {
		log.Printf("marshal %s: %v", hash[:8], err)
		return
	}
	if err := os.WriteFile(torrentPath(hash), raw, 0o644); err != nil {
		log.Printf("write %s: %v", hash[:8], err)
		return
	}
	// файл готов — ленты пересоберутся при следующем опросе с .torrent-ссылками
	cacheMu.Lock()
	feedCache = map[string]feedCacheEntry{}
	cacheMu.Unlock()
	log.Printf("torrent %s: .torrent собран (%d байт)", hash[:8], len(raw))
}

// torrentFileValid — прочитать и проверить собранный файл (валидный bencode, info на месте)
func torrentFileValid(hash string) bool {
	raw, err := os.ReadFile(torrentPath(hash))
	if err != nil {
		return false
	}
	var mi metainfo.MetaInfo
	return bencode.Unmarshal(raw, &mi) == nil && len(mi.InfoBytes) > 0
}
