// tracker-top — топ раздач трекеров (NNMClub, RUTOR) → JSON для плагина Lampa «Топ трекеров».
//
// GET /top?src=both|nnm|rut&cat=video|all&pages=1..3&sort=seeds|top&minq=720|1080|2160&audio=all|dub|no_ts&junk=0|1
// GET /healthz
//
// Кэш в памяти TTL секунд (TTL, 600 по умолчанию). CORS: *. Зависимости: golang.org/x/text (cp1251).
// Переносимость: зеркала задаются NNM_BASE / RUTOR_BASE — micro, VPN-сервер, что угодно.
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const cacheVer = "v10" // версия логики фильтров: смена инвалидирует кэш на томе

var (
	nnmBase   = env("NNM_BASE", "https://nnmclub.to")
	rutorBase = env("RUTOR_BASE", "https://rutor.info")
	ttl       = envInt("TTL", 600)
	rev       = "unknown" // проставляется сборкой: -ldflags -X main.rev=…
)

// Item — нормализованная раздача; все источники приводятся к этому виду
type Item struct {
	ID        int    `json:"id"`
	Ru        string `json:"ru"`
	Orig      string `json:"orig"`
	Year      int    `json:"year,omitempty"`
	Season    bool   `json:"season"`
	Title     string `json:"title"`
	Category  string `json:"category,omitempty"`
	ForumID   int    `json:"forum_id,omitempty"`
	Seeders   int    `json:"seeders"`
	Leechers  int    `json:"leechers"`
	Completed int    `json:"completed,omitempty"`
	Size      int64  `json:"size"`
	SizeText  string `json:"size_text,omitempty"`
	Added     int64  `json:"added,omitempty"`
	URL       string `json:"url"`
	Download  string `json:"download,omitempty"`
	Magnet    string `json:"magnet,omitempty"`
	Source    string `json:"source"`
	Quality   string `json:"quality"` // 2160 | 1080 | 720 | sd
	Dub       bool   `json:"dub"`
	Mvo       bool   `json:"mvo"`                // многоголосая озвучка (MVO, «| P» у rutor)
	SubOnly   bool   `json:"sub_only,omitempty"` // только субтитры («| Sub»), русского звука нет
	TsSound   bool   `json:"ts_sound"`
	Cam       bool   `json:"cam"` // камрип/телефильм-скринка
	Voice     string `json:"voice,omitempty"`
}

type Payload struct {
	Source     string `json:"source"`
	FetchedAt  string `json:"fetched_at"`
	Cached     bool   `json:"cached"`
	Page       int    `json:"page,omitempty"`        // постраничный срез: 1..TotalPages
	TotalPages int    `json:"total_pages,omitempty"` // при Page > 0
	Items      []Item `json:"items"`
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func param(q map[string][]string, k, def string) string {
	if v, ok := q[k]; ok && len(v) > 0 && v[0] != "" {
		return v[0]
	}
	return def
}

// hasCyrillic — есть ли в строке русские буквы
func hasCyrillic(s string) bool {
	for _, r := range s {
		if (r >= 'А' && r <= 'я') || r == 'Ё' || r == 'ё' {
			return true
		}
	}
	return false
}

// filterRussian: ru=1 — раздача обязана нести русский контент: кириллица
// в названии И не «субтитры без озвучки» (rutor «| Sub») — иначе фильм
// без русского звука проникает в топ
func filterRussian(items []Item, ru string) []Item {
	if ru != "1" {
		return items
	}
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if it.SubOnly {
			continue
		}
		if hasCyrillic(it.Ru) || hasCyrillic(it.Orig) {
			out = append(out, it)
		}
	}
	return out
}

// qualityRank: sd=0 < 720 < 1080 < 2160
func qualityRank(q string) int {
	switch q {
	case "2160":
		return 3
	case "1080":
		return 2
	case "720":
		return 1
	}
	return 0
}

// filterVoice — озвучка: выбранный список через запятую («Дубляж» — особое имя).
// Применяется к релизам ДО дедупа: фильм без подходящей раздачи исчезает целиком,
// представитель выбирается среди подходящих.
func filterVoice(items []Item, voices string) []Item {
	if voices == "" {
		return items
	}
	want := map[string]bool{}
	for _, v := range strings.Split(voices, ",") {
		if v = strings.TrimSpace(v); v != "" {
			want[v] = true
		}
	}
	if len(want) == 0 {
		return items
	}

	out := make([]Item, 0, len(items))
	for _, it := range items {
		if want[it.Voice] || (want["Дубляж"] && it.Dub) || (want["Многоголосый"] && it.Mvo) {
			out = append(out, it)
		}
	}
	return out
}

func filterItems(items []Item, minq, audio string) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if minq != "" && minq != "any" && qualityRank(it.Quality) < qualityRank(minq) {
			continue
		}
		if audio == "dub" && !it.Dub {
			continue
		}
		if audio == "no_ts" && it.TsSound {
			continue
		}
		out = append(out, it)
	}
	return out
}

// dedupeFilms — дубликаты раздач одного фильма схлопывает в одну позицию:
// сиды суммируются, представителем становится лучшая раздача (не-камрип,
// максимальное качество, затем размер). Ключ — ru + год: варианты написания
// оригинального названия («Knightfall, Part 1» vs «Knightfall - Part 1»)
// и отсутствующий/добавленный перевод не должны плодить дубли.
func dedupeFilms(items []Item) []Item {
	type slot struct {
		it Item
	}

	var order []*slot
	byKey := map[string]*slot{}

	for _, it := range items {
		key := strings.ToLower(it.Ru) + "|" + strconv.Itoa(it.Year)
		if s, ok := byKey[key]; ok {
			s.it.Seeders += it.Seeders
			s.it.Leechers += it.Leechers
			s.it.Dub = s.it.Dub || it.Dub

			if betterRelease(it, s.it) {
				it.Seeders = s.it.Seeders
				it.Leechers = s.it.Leechers
				s.it = it
			}
		} else {
			byKey[key] = &slot{it: it}
			order = append(order, byKey[key])
		}
	}

	out := make([]Item, 0, len(order))
	for _, s := range order {
		out = append(out, s.it)
	}
	return out
}

// hasRuVoice — русский звук: только явные маркеры — дубляж, многоголоска
// или известная студия. «RU» в тех-скобках NNM («[EN / RU, EN Sub]») —
// это субтитры, не дорожка
func hasRuVoice(it Item) bool {
	return it.Dub || it.Mvo || it.Voice != ""
}

// betterRelease: раздача с русским звуком лучше беззвучной, не-камрип лучше
// камрипа, дальше выше качество, при равенстве — больше размер
func betterRelease(a, b Item) bool {
	ra, rb := hasRuVoice(a), hasRuVoice(b)
	if ra != rb {
		return ra
	}
	if a.Cam != b.Cam {
		return !a.Cam
	}
	if qualityRank(a.Quality) != qualityRank(b.Quality) {
		return qualityRank(a.Quality) > qualityRank(b.Quality)
	}
	return a.Size > b.Size
}

// filterBlocked — всегда: раздачи из стоп-списка (ultradox и пр.)
func filterBlocked(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if blockedName(it.Title) || blockedName(it.Ru) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// filterJunk вычищает камрипы и «звук с TS» ДО дедупа: фильм, у которого
// нет ни одной нормальной раздачи, исчезает из топа целиком
func filterJunk(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if it.Cam || it.TsSound {
			continue
		}
		out = append(out, it)
	}
	return out
}

// filterDead — раздачи без сидов: качать нельзя (фейковые BDRemux
// невышедших фильмов живут именно так — 0 сидов)
func filterDead(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if it.Seeders > 0 {
			out = append(out, it)
		}
	}
	return out
}

// sortItems: seeds — кто раздаёт сейчас; top — завершённость за всё время
func sortItems(items []Item, sortBy string) {
	key := func(it Item) int {
		if sortBy == "top" {
			return it.Completed
		}
		return it.Seeders
	}
	sort.SliceStable(items, func(i, j int) bool { return key(items[i]) > key(items[j]) })
}

func getTop(src, cat string, pages int, junk bool, voices, ru, sort string) (Payload, bool, error) {
	key := cacheVer + "|" + src + "|" + cat + "|" + strconv.Itoa(pages) + "|junk:" + strconv.FormatBool(junk) +
		"|voice:" + voices + "|ru:" + ru + "|" + sort

	cacheMu.Lock()
	if e, ok := cache[key]; ok {
		if time.Since(e.ts) < time.Duration(ttl)*time.Second {
			cacheMu.Unlock()
			return e.payload, true, nil
		}
		// stale-while-revalidate: старое отдаём сразу, свежее собираем фоном —
		// первый запрос к медленному трекеру не блокирует экран; busy не даёт
		// параллельных сборок, листание со второй страницы едет из живого кэша
		if !e.busy {
			e.busy = true
			cache[key] = e
			cacheMu.Unlock()
			go func() {
				_, payload, err := buildTopItems(src, cat, pages, junk, voices, ru, sort)
				cacheMu.Lock()
				if entry, ok2 := cache[key]; ok2 && err == nil {
					entry.payload = payload
					entry.ts = time.Now()
					entry.busy = false
					cache[key] = entry
					diskDirty = true
				} else if ok2 {
					entry.busy = false
					cache[key] = entry
				}
				cacheMu.Unlock()
			}()
			return e.payload, true, nil
		}
		cacheMu.Unlock()
		return e.payload, true, nil
	}
	cacheMu.Unlock()

	_, payload, err := buildTopItems(src, cat, pages, junk, voices, ru, sort)
	if err != nil {
		return Payload{}, false, err
	}

	cacheMu.Lock()
	cache[key] = cacheEntry{ts: time.Now(), payload: payload}
	cacheMu.Unlock()
	diskDirty = true

	return payload, false, nil
}

// buildTopItems — сборка топа без кэша: для getTop и фонового обновления SWR
func buildTopItems(src, cat string, pages int, junk bool, voices, ru, sort string) ([]Item, Payload, error) {
	var items []Item
	var errs []error

	// у NNM есть сортировки «за всё время» (o=6 завершённость); у RUTOR — только по раздающим
	nnmOrder := 10
	if sort == "top" {
		nnmOrder = 6
		src = "nnm"
	}

	if src == "both" || src == "nnm" {
		nnmItems, err := nnmTop(cat, pages, nnmOrder)
		if err != nil {
			log.Printf("nnm: %v", err)
			errs = append(errs, err)
		}
		items = append(items, nnmItems...)
	}

	if src == "both" || src == "rut" {
		rutItems, err := rutorTop(cat)
		if err != nil {
			log.Printf("rutor: %v", err)
			errs = append(errs, err)
		}
		items = append(items, rutItems...)
	}

	// единственный источник упал — ошибка наружу; при «both» отдаём то, что дали
	if len(errs) > 0 && len(items) == 0 {
		return nil, Payload{}, errs[0]
	}

	if junk {
		items = filterJunk(items)
	}
	items = filterBlocked(items)
	items = filterDead(items)
	items = filterVoice(items, voices)
	items = filterRussian(items, ru)

	// мы показываем фильмы для поиска, а не ленту раздач: дубликаты раздач
	// схлопываются в одну позицию, популярность суммируется
	items = dedupeFilms(items)

	sortItems(items, sort)

	p := Payload{Source: src, FetchedAt: time.Now().UTC().Format(time.RFC3339), Items: items}

	return items, p, nil
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

type cacheEntry struct {
	ts      time.Time
	payload Payload
	busy    bool // фоновое обновление уже идёт (stale-while-revalidate)
}

// ---------- персистентный кэш на томе: рестарт не теряет найденные
// раздачи и топы — первый запрос после старта не бьёт по трекерам

type diskTop struct {
	Key     string  `json:"key"`
	Payload Payload `json:"payload"`
	TS      int64   `json:"ts"`
}

type diskFind struct {
	Key   string `json:"key"`
	Item  Item   `json:"item"`
	Found bool   `json:"found"`
	TS    int64  `json:"ts"`
}

type diskCache struct {
	Top  []diskTop  `json:"top"`
	Find []diskFind `json:"find"`
}

var diskDirty bool

func diskPath() string {
	dir := os.Getenv("DATA_DIR")
	if dir == "" {
		return ""
	}
	return dir + "/cache.json"
}

func saveDiskCache() {
	path := diskPath()
	if path == "" {
		return
	}

	cacheMu.Lock()
	findCacheMu.Lock()

	var dc diskCache
	for k, e := range cache {
		dc.Top = append(dc.Top, diskTop{Key: k, Payload: e.payload, TS: e.ts.Unix()})
	}
	for k, e := range findCache {
		dc.Find = append(dc.Find, diskFind{Key: k, Item: e.item, Found: e.found, TS: e.ts.Unix()})
	}

	findCacheMu.Unlock()
	cacheMu.Unlock()

	body, err := json.Marshal(&dc)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		log.Printf("disk cache write: %v", err)
		return
	}
	os.Rename(tmp, path)
	diskDirty = false
}

func loadDiskCache() {
	path := diskPath()
	if path == "" {
		return
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var dc diskCache
	if err := json.Unmarshal(body, &dc); err != nil {
		log.Printf("disk cache read: %v", err)
		return
	}

	cacheMu.Lock()
	for _, t := range dc.Top {
		cache[t.Key] = cacheEntry{ts: time.Unix(t.TS, 0), payload: t.Payload}
	}
	cacheMu.Unlock()

	findCacheMu.Lock()
	for _, f := range dc.Find {
		findCache[f.Key] = findCacheEntry{ts: time.Unix(f.TS, 0), item: f.Item, found: f.Found}
	}
	findCacheMu.Unlock()

	log.Printf("disk cache: %d топов, %d раздач", len(dc.Top), len(dc.Find))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	body, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(code)
	w.Write(body)
}

// warmCache — фоновый прогрев ходовых ключей: первый запрос пользователя
// не должен ждать холодную сборку (она занимает десятки секунд)
var (
	warmVariants = []string{"movie_week", "tv_week", "movie_30", "movie_day", "tv_30", "movie_best", "tv_best", "movie_14", "tv_day", "movie_2025", "tv_2025"}
	warmIdx      = 0
)

func warmCache() {
	for {
		// топы
		for _, k := range [][2]string{{"both", "seeds"}, {"nnm", "seeds"}, {"nnm", "top"}} {
			if _, _, err := getTop(k[0], "video", 2, true, "", "1", k[1]); err != nil {
				log.Printf("warm top %s/%s: %v", k[0], k[1], err)
			}
			time.Sleep(10 * time.Second) // не занимаем NNM-семафор вечно
		}
		// «Топ · TMDB»: по одному варианту за цикл — все прогреются по кругу,
		// find-кэш (12 ч) греется вместе с ними
		if n := len(warmVariants); n > 0 {
			v := warmVariants[warmIdx%n]
			warmIdx++
			if _, _, err := buildFeed(v, 1, 20, "", "", true, true, nil); err != nil {
				log.Printf("warm feed %s: %v", v, err)
			}
		}
		time.Sleep(time.Duration(ttl) * time.Second / 2)
	}
}

func main() {
	port := env("PORT", "8355")
	log.Printf("tracker-top %s on :%s, nnm=%s rutor=%s ttl=%ds", rev, port, nnmBase, rutorBase, ttl)

	loadDiskCache()

	// автосохранение: раз в минуту, если были обновления
	go func() {
		for {
			time.Sleep(time.Minute)
			if diskDirty {
				saveDiskCache()
			}
		}
	}()

	go warmCache()

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "rev": rev, "nnm": nnmBase, "rutor": rutorBase})
	})

	// батч-проверка наличия раздач: один запрос на страницу «Топ · TMDB».
	// GET: ?items=[{"query","year","type"},…]; POST: тот же JSON в теле
	findbatch := func(w http.ResponseWriter, r *http.Request) {
		var raw []byte

		if items := r.URL.Query().Get("items"); items != "" {
			raw = []byte(items)
		} else {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			raw = b
		}

		var req []struct {
			Query string `json:"query"`
			Orig  string `json:"orig"`
			Year  int    `json:"year"`
			Type  string `json:"type"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad json: " + err.Error()})
			return
		}

		q := r.URL.Query()
		minq := param(q, "minq", "")
		voices := param(q, "voice", "")
		junk := param(q, "junk", "1") != "0"
		ru := param(q, "ru", "1") != "0"

		found := make([]bool, len(req))
		quality := make([]string, len(req))
		sem := make(chan struct{}, 2) // бережём NNM: 503 при лавине
		var wg sync.WaitGroup
		for i := range req {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				it, ok := findWithCache2(req[i].Query, req[i].Orig, req[i].Year, req[i].Type, minq, voices, junk, ru)
				found[i] = ok
				if ok {
					quality[i] = it.Quality
				}
			}(i)
		}
		wg.Wait()

		writeJSON(w, 200, map[string]any{"found": found, "quality": quality})
	}

	mux.HandleFunc("GET /findbatch", findbatch)
	mux.HandleFunc("POST /findbatch", findbatch)

	// страница «Топ · TMDB» целиком: список + фильтры раздач + качество
	mux.HandleFunc("GET /feed", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		variant := param(q, "variant", "movie_week")
		page := 1
		if n, e := strconv.Atoi(param(q, "page", "1")); e == nil && n > 0 && n <= 100 {
			page = n
		}
		feedSize := 20
		if n, e := strconv.Atoi(param(q, "slice", "20")); e == nil && n > 0 && n <= 50 {
			feedSize = n
		}

		// просмотренные от клиента: id через запятую — сервер исключает,
		// страница собирается ПОСЛЕ фильтра и всегда полная
		exclude := map[int]bool{}
		for _, part := range strings.Split(param(q, "exclude", ""), ",") {
			if n, e := strconv.Atoi(strings.TrimSpace(part)); e == nil {
				exclude[n] = true
			}
		}

		results, total, err := buildFeed(variant, page, feedSize,
			param(q, "minq", ""), param(q, "voice", ""),
			param(q, "junk", "1") != "0", param(q, "ru", "1") != "0", exclude)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, 200, map[string]any{
			"page":        page,
			"total_pages": total,
			"results":     results,
		})
	})

	// есть ли у фильма раздача под наши фильтры — для «Топ · TMDB»
	mux.HandleFunc("GET /find", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		query := param(q, "query", "")
		if query == "" {
			writeJSON(w, 400, map[string]string{"error": "query required"})
			return
		}
		year, _ := strconv.Atoi(param(q, "year", "0"))

		it, found := findWithCache2(query, param(q, "orig", ""), year, param(q, "type", "movie"),
			param(q, "minq", ""), param(q, "voice", ""),
			param(q, "junk", "1") != "0", param(q, "ru", "1") != "0")

		writeJSON(w, 200, map[string]any{"found": found, "item": it})
	})

	mux.HandleFunc("/top", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		src := param(q, "src", "both")
		if src != "both" && src != "nnm" && src != "rut" {
			writeJSON(w, 400, map[string]string{"error": "src must be one of both|nnm|rut"})
			return
		}
		cat := param(q, "cat", "video")
		pages := 1
		if n, err := strconv.Atoi(param(q, "pages", "1")); err == nil {
			// классика меняется редко — даём копать глубже
			limit := 3
			if param(q, "sort", "seeds") == "top" {
				limit = 10
			}
			if n > limit {
				n = limit
			}
			if n > 0 {
				pages = n
			}
		}
		page := 0
		if n, e := strconv.Atoi(param(q, "page", "0")); e == nil && n > 0 {
			page = n
		}
		slice := 20
		if v := param(q, "slice", ""); v != "" {
			if n, e := strconv.Atoi(v); e == nil && n > 0 && n <= 100 {
				slice = n
			}
		}
		minq := param(q, "minq", "") // "" | 720 | 1080 | 2160
		voices := param(q, "voice", "")
		ru := param(q, "ru", "1") // 1 — скрывать названия без русских букв
		audio := param(q, "audio", "all")
		junk := param(q, "junk", "1") != "0"
		sortBy := param(q, "sort", "seeds")
		if sortBy != "seeds" && sortBy != "top" {
			writeJSON(w, 400, map[string]string{"error": "sort must be one of seeds|top"})
			return
		}

		payload, cached, err := getTop(src, cat, pages, junk, voices, ru, sortBy)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		payload.Cached = cached
		payload.Items = filterItems(payload.Items, minq, audio)

		// клиент листает базу постранично: page=N — срез; первая страница
		// заодно запускает фоновое обновление базы (внутри getTop)
		if page > 0 {
			total := (len(payload.Items) + slice - 1) / slice
			lo := (page - 1) * slice
			if lo > len(payload.Items) {
				lo = len(payload.Items)
			}
			hi := lo + slice
			if hi > len(payload.Items) {
				hi = len(payload.Items)
			}
			payload.Page = page
			payload.TotalPages = total
			payload.Items = payload.Items[lo:hi]
		}

		writeJSON(w, 200, payload)
	})

	// CORS preflight и прочее
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET")
			w.WriteHeader(204)
			return
		}
		writeJSON(w, 404, map[string]string{"error": "not found"})
	})

	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig
		srv.Close()
		os.Exit(0)
	}()

	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
