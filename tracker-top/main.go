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
	TsSound   bool   `json:"ts_sound"`
	Cam       bool   `json:"cam"` // камрип/телефильм-скринка
	Voice     string `json:"voice,omitempty"`
}

type Payload struct {
	Source    string `json:"source"`
	FetchedAt string `json:"fetched_at"`
	Cached    bool   `json:"cached"`
	Items     []Item `json:"items"`
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
		if want[it.Voice] || (want["Дубляж"] && it.Dub) {
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

// dedupeFilms — дубликаты раздач одного фильма (одинаковое ru/orig + год) схлопывает
// в одну позицию: сиды суммируются, представителем становится лучшая раздача
// (максимальное качество, затем размер). Мы отдаём фильмы для поиска, а не ленту раздач.
func dedupeFilms(items []Item) []Item {
	type slot struct {
		it Item
	}

	var order []*slot
	byKey := map[string]*slot{}

	for _, it := range items {
		key := strings.ToLower(it.Ru+"|"+it.Orig) + "|" + strconv.Itoa(it.Year)
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

// betterRelease: не-камрип лучше камрипа, дальше выше качество, при равенстве — больше размер
func betterRelease(a, b Item) bool {
	if a.Cam != b.Cam {
		return !a.Cam
	}
	if qualityRank(a.Quality) != qualityRank(b.Quality) {
		return qualityRank(a.Quality) > qualityRank(b.Quality)
	}
	return a.Size > b.Size
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

func getTop(src, cat string, pages int, junk bool, voices, sort string) (Payload, bool, error) {
	key := src + "|" + cat + "|" + strconv.Itoa(pages) + "|junk:" + strconv.FormatBool(junk) +
		"|voice:" + voices + "|" + sort

	cacheMu.Lock()
	if e, ok := cache[key]; ok && time.Since(e.ts) < time.Duration(ttl)*time.Second {
		cacheMu.Unlock()
		return e.payload, true, nil
	}
	cacheMu.Unlock()

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
		return Payload{}, false, errs[0]
	}

	if junk {
		items = filterJunk(items)
	}
	items = filterVoice(items, voices)

	// мы показываем фильмы для поиска, а не ленту раздач: дубликаты раздач
	// схлопываются в одну позицию, популярность суммируется
	items = dedupeFilms(items)

	sortItems(items, sort)

	p := Payload{Source: src, FetchedAt: time.Now().UTC().Format(time.RFC3339), Items: items}

	cacheMu.Lock()
	cache[key] = cacheEntry{ts: time.Now(), payload: p}
	cacheMu.Unlock()

	return p, false, nil
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

type cacheEntry struct {
	ts      time.Time
	payload Payload
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	body, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(code)
	w.Write(body)
}

func main() {
	port := env("PORT", "8355")
	log.Printf("tracker-top %s on :%s, nnm=%s rutor=%s ttl=%ds", rev, port, nnmBase, rutorBase, ttl)

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "rev": rev, "nnm": nnmBase, "rutor": rutorBase})
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
		minq := param(q, "minq", "") // "" | 720 | 1080 | 2160
		voices := param(q, "voice", "")
		audio := param(q, "audio", "all")
		junk := param(q, "junk", "1") != "0"
		sortBy := param(q, "sort", "seeds")
		if sortBy != "seeds" && sortBy != "top" {
			writeJSON(w, 400, map[string]string{"error": "sort must be one of seeds|top"})
			return
		}

		payload, cached, err := getTop(src, cat, pages, junk, voices, sortBy)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		payload.Cached = cached
		payload.Items = filterItems(payload.Items, minq, audio)

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
