// nnm-rss — личные RSS-ленты NNMClub по образцу lostfilmfeed.byalex.dev.
// Личность юзера — passkey трекера (аналог TrackerId у lostfilm): главная
// публична, ввёл свой ключ → открылась твоя страница с подписками и историей,
// лента — /rss/<passkey>. Ни логинов, ни паролей, ни cookie-сессий трекера.
// Лента отдаёт магниты с персональным announce юзера — клиент аннонсит его
// ключом, статистика на трекере считается ему.
//
// GET  /                     — публичная главная + «Моя подписка» (веб)
// POST /api/open             — открыть свою страницу по passkey (кука)
// GET  /api/me               — мой профиль (подписки, история, лента)
// POST /api/subs, PATCH/DELETE /api/subs/{id}
// GET  /rss/{passkey}        — личная лента
// GET  /healthz
//
// Состояние: JSON в DATA_DIR (том /data), без внешней БД. Кэши в памяти TTL.
package main

import (
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

var rev = "unknown" // проставляется сборкой: -ldflags -X main.rev=…

//go:embed ui.html
var uiHTML []byte

var (
	ttl = envInt("TTL", 600)

	cacheMu      sync.Mutex
	rssCache     = map[string]rssCacheEntry{}  // родные ленты подписок: kind:id → body
	resolveCache = map[string]resolveEntry{}   // тема/пост → info-hash
	feedCache    = map[string]feedCacheEntry{} // готовый XML ленты: passkey → байты
)

type rssCacheEntry struct {
	ts   time.Time
	body string
}

type resolveEntry struct {
	ts   time.Time
	hash string
}

type feedCacheEntry struct {
	ts   time.Time
	body []byte
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

func writeJSON(w http.ResponseWriter, code int, v any) {
	body, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(code)
	w.Write(body)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func baseURL(r *http.Request) string {
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

var passkeyRe = regexp.MustCompile(`^[a-f0-9]{32}$`)

// currentProfile — профиль по куке pk (может быть nil: главная публична)
func currentProfile(r *http.Request) *Profile {
	c, err := r.Cookie("pk")
	if err != nil {
		return nil
	}
	pk := strings.ToLower(strings.TrimSpace(c.Value))
	if !passkeyRe.MatchString(pk) {
		return nil
	}
	for _, p := range state.Profiles {
		if p.Passkey == pk {
			return p
		}
	}
	return nil
}

// ---------- лента

type feedGuid struct {
	Value       string `xml:",chardata"`
	IsPermaLink bool   `xml:"isPermaLink,attr"`
}

type feedItem struct {
	Title   string   `xml:"title"`
	Link    string   `xml:"link"`
	Desc    string   `xml:"description"`
	Guid    feedGuid `xml:"guid"`
	PubDate string   `xml:"pubDate"`
}

type feedRSS struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel struct {
		Title       string     `xml:"title"`
		Link        string     `xml:"link"`
		Description string     `xml:"description"`
		Items       []feedItem `xml:"item"`
	} `xml:"channel"`
}

// cachedRSS — родная лента подписки (гость); одна на всех юзеров, кэш на TTL
func cachedRSS(s *Sub) (string, error) {
	key := s.Kind + ":" + strconv.Itoa(s.NNMID)
	cacheMu.Lock()
	if e, ok := rssCache[key]; ok && time.Since(e.ts) < time.Duration(ttl)*time.Second {
		cacheMu.Unlock()
		return e.body, nil
	}
	cacheMu.Unlock()

	body, err := fetchNNM(subRSSURL(s), "")
	if err != nil {
		return "", err
	}
	cacheMu.Lock()
	rssCache[key] = rssCacheEntry{ts: time.Now(), body: body}
	cacheMu.Unlock()
	return body, nil
}

// buildFeed — XML личной ленты: все включённые подписки, фильтры, новые сверху.
// Ссылки — магниты с персональным announce юзера (статистика на трекере его).
// Возвращает XML и items для истории «Моя подписка»; из кэша — hist == nil
func buildFeed(p *Profile) ([]byte, []HistItem, error) {
	cacheMu.Lock()
	if e, ok := feedCache[p.Passkey]; ok && time.Since(e.ts) < time.Duration(ttl)*time.Second {
		cacheMu.Unlock()
		return e.body, nil, nil
	}
	cacheMu.Unlock()

	type keyItem struct {
		it  feedItem
		at  time.Time
		gid string
	}
	byGUID := map[string]keyItem{}

	for _, s := range p.Subs {
		if !s.Enabled {
			continue
		}
		var inc, exc *regexp.Regexp
		if s.Filter != "" {
			if re, err := regexp.Compile(`(?i)` + s.Filter); err == nil {
				inc = re
			}
		}
		if s.Exclude != "" {
			if re, err := regexp.Compile(`(?i)` + s.Exclude); err == nil {
				exc = re
			}
		}

		body, err := cachedRSS(s)
		if err != nil {
			log.Printf("feed %.8s…/%s: %v", p.Passkey, s.Title, err)
			continue
		}

		for _, it := range parseRSSItems(body) {
			title := cleanTitle(it.Title)
			if title == "" {
				continue
			}
			if inc != nil && !inc.MatchString(title) {
				continue
			}
			if exc != nil && exc.MatchString(title) {
				continue
			}

			gid := it.Kind + strconv.Itoa(it.ID)
			if _, dup := byGUID[gid]; dup {
				continue
			}

			// topic → viewtopic.php?t=, post → viewtopic.php?p=
			param := map[string]string{"topic": "t", "post": "p"}[it.Kind]
			link := nnmBase + "/forum/viewtopic.php?" + param + "=" + strconv.Itoa(it.ID)

			byGUID[gid] = keyItem{
				it: feedItem{
					Title:   title,
					Link:    link, // магнит проставим после резолва info-hash
					Desc:    s.Title,
					Guid:    feedGuid{Value: "nnm-" + gid, IsPermaLink: false},
					PubDate: it.Date.UTC().Format(time.RFC1123Z),
				},
				at:  it.Date,
				gid: gid,
			}
		}
	}

	items := make([]keyItem, 0, len(byGUID))
	for _, v := range byGUID {
		items = append(items, v)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].at.After(items[j].at) }) // новые сверху
	if len(items) > 100 {
		items = items[:100]
	}

	// резолв info-hash'ей параллельно (первая сборка — до сотни страниц)
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for idx := range items {
		wg.Add(1)
		k := &items[idx]
		go func(k *keyItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			hash, err := resolveInfoHash(k2kind(k.gid), k2id(k.gid))
			if err != nil {
				log.Printf("resolve %s: %v", k.it.Title, err)
				return
			}
			if hash != "" {
				k.it.Link = magnetLink(hash, p.Passkey)
			}
		}(k)
	}
	wg.Wait()

	var f feedRSS
	f.Version = "2.0"
	f.Channel.Title = "NNM-Club — " + p.Passkey[:8] + "…"
	f.Channel.Link = nnmBase
	f.Channel.Description = "nnm-rss: подписки " + p.Passkey[:8] + "…"
	hist := make([]HistItem, 0, len(items))
	for _, v := range items {
		f.Channel.Items = append(f.Channel.Items, v.it)
		hist = append(hist, HistItem{
			GUID:  v.it.Guid.Value,
			Title: v.it.Title,
			URL:   v.it.Link,
			Date:  v.at,
		})
	}

	body, err := xml.Marshal(&f)
	if err != nil {
		return nil, nil, err
	}
	out := append([]byte(xml.Header), body...)

	cacheMu.Lock()
	feedCache[p.Passkey] = feedCacheEntry{ts: time.Now(), body: out}
	cacheMu.Unlock()
	return out, hist, nil
}

// gid вида "topic123" / "post456" → (kind, id)
func k2kind(gid string) string {
	if strings.HasPrefix(gid, "post") {
		return "post"
	}
	return "topic"
}

func k2id(gid string) int {
	n, _ := strconv.Atoi(strings.TrimLeftFunc(gid, func(r rune) bool { return !unicode.IsDigit(r) }))
	return n
}

// ---------- HTTP

func main() {
	port := env("PORT", "8356")
	loadState()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		n := len(state.Profiles)
		stateMu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true, "rev": rev, "profiles": n})
	})

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(uiHTML)
	})

	// ----- «вход»: открытие своей страницы по passkey (кука на год)

	mux.HandleFunc("POST /api/open", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Passkey string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, 400, "неправильный запрос")
			return
		}
		pk := strings.ToLower(strings.TrimSpace(in.Passkey))
		if !passkeyRe.MatchString(pk) {
			writeErr(w, 400, "passkey — 32 hex-символа из трекер-URL любого своего торрента (после «:2710/», перед «/announce»)")
			return
		}

		stateMu.Lock()
		created := false
		if profileByPassKeyExisting(pk) == nil {
			profileByPasskey(pk) // первый вход — создаём профиль
			created = true
			saveState()
		}
		stateMu.Unlock()

		http.SetCookie(w, &http.Cookie{
			Name:     "pk",
			Value:    pk,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   isHTTPS(r),
			MaxAge:   365 * 24 * 3600,
		})
		if created {
			log.Printf("новый профиль %.8s…", pk)
		}
		writeJSON(w, 200, map[string]string{"ok": "1"})
	})

	mux.HandleFunc("POST /api/close", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "pk", Value: "", Path: "/", MaxAge: -1})
		writeJSON(w, 200, map[string]string{"ok": "1"})
	})

	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		p := currentProfile(r)
		if p == nil {
			stateMu.Unlock()
			writeErr(w, 401, "passkey не указан")
			return
		}
		resp := map[string]any{
			"passkey":  p.Passkey,
			"feed_url": baseURL(r) + "/rss/" + p.Passkey,
			"subs":     p.Subs,
			"history":  p.History,
		}
		stateMu.Unlock()
		writeJSON(w, 200, resp)
	})

	// ----- подписки

	mux.HandleFunc("POST /api/subs", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ URL, Filter, Exclude string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, 400, "неправильный запрос")
			return
		}
		if err := validateRegexes(in.Filter, in.Exclude); err != nil {
			writeErr(w, 400, err.Error())
			return
		}

		kind, id := parseNNMURL(in.URL)
		if id == 0 {
			writeErr(w, 400, "не нашёл раздачу или раздел в ссылке — вставьте ссылку вида viewtopic.php?t=… или tracker.php?f=…")
			return
		}

		stateMu.Lock()
		defer stateMu.Unlock()
		p := currentProfile(r)
		if p == nil {
			writeErr(w, 401, "passkey не указан")
			return
		}
		for _, s := range p.Subs {
			if s.Kind == kind && s.NNMID == id {
				writeErr(w, 400, "уже в подписках")
				return
			}
		}

		title := strconv.Itoa(id)
		if kind == "forum" {
			if body, err := fetchNNM(subRSSURL(&Sub{Kind: kind, NNMID: id}), ""); err == nil {
				if ch := parseRSSChannel(body); ch != "" {
					title = ch
				}
			}
		} else if t, err := topicTitle(id); err == nil {
			title = t
		}

		s := &Sub{ID: randHex(8), Kind: kind, NNMID: id, Title: title,
			Filter: in.Filter, Exclude: in.Exclude, Enabled: true}
		p.Subs = append(p.Subs, s)
		invalidateFeed(p.Passkey)
		saveState()
		log.Printf("sub+ %.8s… %s:%d (%s)", p.Passkey, kind, id, title)
		writeJSON(w, 200, s)
	})

	mux.HandleFunc("PATCH /api/subs/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Filter, Exclude *string; Enabled *bool }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, 400, "неправильный запрос")
			return
		}
		f, e := ptrStr(in.Filter), ptrStr(in.Exclude)
		if err := validateRegexes(f, e); err != nil {
			writeErr(w, 400, err.Error())
			return
		}

		stateMu.Lock()
		defer stateMu.Unlock()
		p := currentProfile(r)
		if p == nil {
			writeErr(w, 401, "passkey не указан")
			return
		}
		s := p.sub(r.PathValue("id"))
		if s == nil {
			writeErr(w, 404, "подписка не найдена")
			return
		}
		if in.Filter != nil {
			s.Filter = f
		}
		if in.Exclude != nil {
			s.Exclude = e
		}
		if in.Enabled != nil {
			s.Enabled = *in.Enabled
		}
		invalidateFeed(p.Passkey)
		saveState()
		writeJSON(w, 200, s)
	})

	mux.HandleFunc("DELETE /api/subs/{id}", func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		defer stateMu.Unlock()
		p := currentProfile(r)
		if p == nil {
			writeErr(w, 401, "passkey не указан")
			return
		}
		for i, s := range p.Subs {
			if s.ID == r.PathValue("id") {
				p.Subs = append(p.Subs[:i], p.Subs[i+1:]...)
				invalidateFeed(p.Passkey)
				saveState()
				writeJSON(w, 200, map[string]string{"ok": "1"})
				return
			}
		}
		writeErr(w, 404, "подписка не найдена")
	})

	// ----- личная лента: /rss/<passkey>, как rssfeeds/<uuid> у lostfilmfeed

	mux.HandleFunc("GET /rss/{passkey}", func(w http.ResponseWriter, r *http.Request) {
		pk := strings.ToLower(r.PathValue("passkey"))
		if !passkeyRe.MatchString(pk) {
			http.Error(w, "нет такой ленты", 404)
			return
		}

		// снапшот профиля под блокировкой, сборка ленты (с сетью) — без неё
		stateMu.Lock()
		var p *Profile
		for _, pp := range state.Profiles {
			if pp.Passkey == pk {
				cp := *pp
				cp.Subs = make([]*Sub, len(pp.Subs))
				for i, s := range pp.Subs {
					sc := *s
					cp.Subs[i] = &sc
				}
				p = &cp
				break
			}
		}
		stateMu.Unlock()

		if p == nil {
			http.Error(w, "нет такой ленты (откройте страницу сервиса и укажите passkey)", 404)
			return
		}
		body, hist, err := buildFeed(p)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		// история «Моя подписка»: новые раздачи ленты (из кэша hist == nil)
		if hist != nil {
			stateMu.Lock()
			if live := profileByPassKeyExisting(pk); live != nil {
				seen := map[string]bool{}
				for _, h := range live.History {
					seen[h.GUID] = true
				}
				merged := live.History
				added := false
				for _, h := range hist {
					if !seen[h.GUID] {
						merged = append(merged, h)
						added = true
					}
				}
				if added || len(merged) > 50 {
					sort.Slice(merged, func(i, j int) bool { return merged[i].Date.After(merged[j].Date) })
					if len(merged) > 50 {
						merged = merged[:50]
					}
					live.History = merged
					saveState()
				}
			}
			stateMu.Unlock()
		}

		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		w.Write(body)
	})

	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig
		srv.Close()
		os.Exit(0)
	}()

	log.Printf("nnm-rss %s on :%s, nnm=%s", rev, port, nnmBase)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// profileByPassKeyExisting — без создания (под stateMu)
func profileByPassKeyExisting(pk string) *Profile {
	for _, p := range state.Profiles {
		if p.Passkey == pk {
			return p
		}
	}
	return nil
}

// invalidateFeed — подписки изменились, ленту пересоберём (под stateMu)
func invalidateFeed(passkey string) {
	cacheMu.Lock()
	delete(feedCache, passkey)
	cacheMu.Unlock()
}

func validateRegexes(filter, exclude string) error {
	for _, s := range []struct{ name, re string }{{"фильтр", filter}, {"исключение", exclude}} {
		if s.re != "" {
			if _, err := regexp.Compile(s.re); err != nil {
				return err
			}
		}
	}
	return nil
}

func ptrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
