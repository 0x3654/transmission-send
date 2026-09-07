// nnm-rss — личные RSS-ленты NNMClub по образцу lostfilmfeed.byalex.dev:
// регистрируешься, вставляешь bb_data-cookie трекера, подписываешься на разделы
// и конкретные раздачи — торрент-клиент забирает из ленты .torrent, скачанный
// твоей сессией (пасскей в announce, статистика на трекере твоя).
//
// GET  /                     — веб-интерфейс
// POST /api/register|login|logout
// GET  /api/me               — состояние для UI
// PUT  /api/nnm              — сохранить/проверить cookie трекера
// POST /api/subs, PATCH/DELETE /api/subs/{id}
// GET  /rss/{token}          — личная лента (token — секрет юзера)
// GET  /dl/{token}?topic=|post=|id=&name= — .torrent с cookie юзера
// GET  /healthz
//
// Состояние: JSON в DATA_DIR (том /data), без внешней БД. Кэши в памяти TTL.
// Публичный сервис: REGISTRATION=off закрывает регистрацию.
package main

import (
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var rev = "unknown" // проставляется сборкой: -ldflags -X main.rev=…

//go:embed ui.html
var uiHTML []byte

var (
	ttl           = envInt("TTL", 600)
	registration  = env("REGISTRATION", "on") != "off"
	sessionCookie = "sid"

	cacheMu      sync.Mutex
	rssCache     = map[string]rssCacheEntry{}  // личные ленты подписок: userID|subID → body
	resolveCache = map[string]resolveEntry{}   // nnm.go: тема/пост → download id
	feedCache    = map[string]feedCacheEntry{} // готовый XML ленты: userID → байты
)

type rssCacheEntry struct {
	ts   time.Time
	body string
}

type resolveEntry struct {
	ts     time.Time
	dlID   int
	magnet string
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

// ---------- сессии

func setSessionCookie(w *http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(*w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isHTTPS(r),
		MaxAge:   30 * 24 * 3600,
	})
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

// currentUser — юзер по сессионной куке (вызывается под stateMu)
func currentUser(r *http.Request) *User {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	return userBySession(c.Value)
}

// ---------- brute-force защита логина/регистрации

var (
	rlMu    sync.Mutex
	rlTries = map[string]*rlEntry{}
)

type rlEntry struct {
	count int
	reset time.Time
}

func rateLimitOK(ip string) bool {
	rlMu.Lock()
	defer rlMu.Unlock()
	e := rlTries[ip]
	if e == nil || time.Now().After(e.reset) {
		rlTries[ip] = &rlEntry{count: 1, reset: time.Now().Add(15 * time.Minute)}
		return true
	}
	e.count++
	return e.count <= 20
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

// cachedRSS — родная лента подписки с cookie владельца (раздел может быть
// закрыт от гостей); кэшируется на TTL
func cachedRSS(u *User, s *Sub) (string, error) {
	key := u.ID + "|" + s.ID
	cacheMu.Lock()
	if e, ok := rssCache[key]; ok && time.Since(e.ts) < time.Duration(ttl)*time.Second {
		cacheMu.Unlock()
		return e.body, nil
	}
	cacheMu.Unlock()

	body, err := fetchNNM(subRSSURL(s), u.NNMCookie)
	if err != nil {
		return "", err
	}
	cacheMu.Lock()
	rssCache[key] = rssCacheEntry{ts: time.Now(), body: body}
	cacheMu.Unlock()
	return body, nil
}

// buildFeed — XML личной ленты: все включённые подписки, фильтры, новые сверху.
// Ссылки ведут на наш /dl (лениво резолвит .torrent с cookie юзера); без cookie
// отдаём исходные ссылки на темы — качать нечем, но лента живая
func buildFeed(u *User, base string) ([]byte, error) {
	cacheMu.Lock()
	if e, ok := feedCache[u.ID]; ok && time.Since(e.ts) < time.Duration(ttl)*time.Second {
		cacheMu.Unlock()
		return e.body, nil
	}
	cacheMu.Unlock()

	type keyItem struct {
		it  feedItem
		at  time.Time
		gid string
	}
	byGUID := map[string]keyItem{}

	for _, s := range u.Subs {
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

		body, err := cachedRSS(u, s)
		if err != nil {
			log.Printf("feed %s/%s: %v", u.Login, s.Title, err)
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
			if u.NNMCookie != "" {
				link = base + "/dl/" + u.FeedToken + "?" + it.Kind + "=" + strconv.Itoa(it.ID) +
					"&name=" + url.QueryEscape(title)
			}

			byGUID[gid] = keyItem{
				it: feedItem{
					Title:   title,
					Link:    link,
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

	var f feedRSS
	f.Version = "2.0"
	f.Channel.Title = "NNM-Club — " + u.Login
	f.Channel.Link = nnmBase
	f.Channel.Description = "nnm-rss: подписки " + u.Login
	for _, v := range items {
		f.Channel.Items = append(f.Channel.Items, v.it)
	}

	body, err := xml.Marshal(&f)
	if err != nil {
		return nil, err
	}
	out := append([]byte(xml.Header), body...)

	cacheMu.Lock()
	feedCache[u.ID] = feedCacheEntry{ts: time.Now(), body: out}
	cacheMu.Unlock()
	return out, nil
}

// ---------- HTTP

func main() {
	port := env("PORT", "8356")
	loadState()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		n := len(state.Users)
		stateMu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true, "rev": rev, "users": n})
	})

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(uiHTML)
	})

	// ----- аккаунты сервиса

	mux.HandleFunc("POST /api/register", func(w http.ResponseWriter, r *http.Request) {
		if !registration {
			writeErr(w, 403, "регистрация закрыта")
			return
		}
		if !rateLimitOK(r.RemoteAddr) {
			writeErr(w, 429, "слишком много попыток, подождите")
			return
		}
		var in struct{ Login, Password string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, 400, "неправильный запрос")
			return
		}

		stateMu.Lock()
		defer stateMu.Unlock()
		u, err := createUser(in.Login, in.Password)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		token := openSession(u.ID)
		saveState()
		setSessionCookie(&w, r, token)
		log.Printf("register %s", u.Login)
		writeJSON(w, 200, map[string]string{"ok": "1"})
	})

	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		if !rateLimitOK(r.RemoteAddr) {
			writeErr(w, 429, "слишком много попыток, подождите")
			return
		}
		var in struct{ Login, Password string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, 400, "неправильный запрос")
			return
		}

		stateMu.Lock()
		defer stateMu.Unlock()
		u := userByLogin(in.Login)
		if u == nil || !checkPassword(u, in.Password) {
			log.Printf("login fail %s", in.Login)
			writeErr(w, 401, "неверный логин или пароль")
			return
		}
		token := openSession(u.ID)
		saveState()
		setSessionCookie(&w, r, token)
		writeJSON(w, 200, map[string]string{"ok": "1"})
	})

	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		defer stateMu.Unlock()
		if c, err := r.Cookie(sessionCookie); err == nil {
			delete(state.Sessions, c.Value)
			saveState()
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
		writeJSON(w, 200, map[string]string{"ok": "1"})
	})

	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		u := currentUser(r)
		if u == nil {
			stateMu.Unlock()
			writeErr(w, 401, "не авторизован")
			return
		}
		resp := map[string]any{
			"login":    u.Login,
			"feed_url": baseURL(r) + "/rss/" + u.FeedToken,
			"nnm": map[string]any{
				"set":  u.NNMCookie != "",
				"user": u.NNMUser,
				"uid":  u.NNMUID,
			},
			"subs": u.Subs,
		}
		stateMu.Unlock()
		writeJSON(w, 200, resp)
	})

	// ----- cookie трекера

	mux.HandleFunc("PUT /api/nnm", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Cookie string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, 400, "неправильный запрос")
			return
		}

		cookie := normalizeCookie(in.Cookie)
		if cookie == "" {
			writeErr(w, 400, "пусто")
			return
		}

		username, uid, err := checkCookie(cookie)
		if err != nil {
			writeErr(w, 502, "NNM недоступен: "+err.Error())
			return
		}
		if username == "" {
			writeErr(w, 400, "cookie не работает: сессия не находится (скопируйте bb_data заново)")
			return
		}

		stateMu.Lock()
		defer stateMu.Unlock()
		u := currentUser(r)
		if u == nil {
			writeErr(w, 401, "не авторизован")
			return
		}
		u.NNMCookie, u.NNMUser, u.NNMUID = cookie, username, uid
		invalidateFeed(u)
		saveState()
		log.Printf("nnm cookie ok: %s → %s (uid %d)", u.Login, username, uid)
		writeJSON(w, 200, map[string]any{"ok": "1", "user": username, "uid": uid})
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
		u := currentUser(r)
		if u == nil {
			writeErr(w, 401, "не авторизован")
			return
		}
		for _, s := range u.Subs {
			if s.Kind == kind && s.NNMID == id {
				writeErr(w, 400, "уже в подписках")
				return
			}
		}

		title := strconv.Itoa(id)
		if kind == "forum" {
			if body, err := fetchNNM(subRSSURL(&Sub{Kind: kind, NNMID: id}), u.NNMCookie); err == nil {
				if ch := parseRSSChannel(body); ch != "" {
					title = ch
				}
			}
		} else if t, err := topicTitle(id, u.NNMCookie); err == nil {
			title = t
		}

		s := &Sub{ID: randHex(8), Kind: kind, NNMID: id, Title: title,
			Filter: in.Filter, Exclude: in.Exclude, Enabled: true}
		u.Subs = append(u.Subs, s)
		invalidateFeed(u)
		saveState()
		log.Printf("sub+ %s %s:%d (%s)", u.Login, kind, id, title)
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
		u := currentUser(r)
		if u == nil {
			writeErr(w, 401, "не авторизован")
			return
		}
		s := u.sub(r.PathValue("id"))
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
		invalidateFeed(u)
		saveState()
		writeJSON(w, 200, s)
	})

	mux.HandleFunc("DELETE /api/subs/{id}", func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		defer stateMu.Unlock()
		u := currentUser(r)
		if u == nil {
			writeErr(w, 401, "не авторизован")
			return
		}
		for i, s := range u.Subs {
			if s.ID == r.PathValue("id") {
				u.Subs = append(u.Subs[:i], u.Subs[i+1:]...)
				invalidateFeed(u)
				saveState()
				writeJSON(w, 200, map[string]string{"ok": "1"})
				return
			}
		}
		writeErr(w, 404, "подписка не найдена")
	})

	// ----- личная лента и скачивание

	mux.HandleFunc("GET /rss/{token}", func(w http.ResponseWriter, r *http.Request) {
		// снапшот юзера под блокировкой, сборка ленты (с сетевыми запросами) — без неё
		stateMu.Lock()
		src := userByFeedToken(r.PathValue("token"))
		var u *User
		if src != nil {
			cp := *src
			cp.Subs = make([]*Sub, len(src.Subs))
			for i, s := range src.Subs {
				sc := *s
				cp.Subs[i] = &sc
			}
			u = &cp
		}
		stateMu.Unlock()

		if u == nil {
			http.Error(w, "нет такой ленты", 404)
			return
		}
		body, err := buildFeed(u, baseURL(r))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		w.Write(body)
	})

	mux.HandleFunc("GET /dl/{token}", func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		u := userByFeedToken(r.PathValue("token"))
		cookie := ""
		if u != nil {
			cookie = u.NNMCookie
		}
		stateMu.Unlock()

		if u == nil {
			http.Error(w, "нет такой ленты", 404)
			return
		}
		if cookie == "" {
			http.Error(w, "сохраните cookie трекера в настройках nnm-rss", 409)
			return
		}

		q := r.URL.Query()
		name := slug(q.Get("name"))

		dlID := atoiDefault(q.Get("id"))
		if dlID == 0 {
			kind, id := "topic", atoiDefault(q.Get("topic"))
			if id == 0 {
				kind, id = "post", atoiDefault(q.Get("post"))
			}
			if id == 0 {
				http.Error(w, "нужен topic=, post= или id=", 400)
				return
			}

			var err error
			dlID, _, err = resolveTorrent(kind, id, cookie)
			if err != nil {
				http.Error(w, "NNM недоступен: "+err.Error(), 502)
				return
			}
			if dlID == 0 {
				http.Error(w, "в этой теме/посте не нашлось раздачи (или она скрыта)", 404)
				return
			}
		}

		log.Printf("dl %s id=%d", u.Login, dlID)
		proxyTorrent(w, dlID, cookie, name)
	})

	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig
		srv.Close()
		os.Exit(0)
	}()

	log.Printf("nnm-rss %s on :%s, nnm=%s, registration=%t", rev, port, nnmBase, registration)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// invalidateFeed — подписки изменились, ленту пересоберём (под stateMu)
func invalidateFeed(u *User) {
	cacheMu.Lock()
	delete(feedCache, u.ID)
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

// normalizeCookie: голое значение → bb_data=значение; строку с "x=y" не трогаем
func normalizeCookie(s string) string {
	s = strings.TrimSpace(s)
	if l := strings.ToLower(s); strings.HasPrefix(l, "cookie:") {
		s = strings.TrimSpace(s[len("cookie:"):])
	}
	if s == "" || strings.Contains(s, "=") {
		return s
	}
	return "bb_data=" + s
}
