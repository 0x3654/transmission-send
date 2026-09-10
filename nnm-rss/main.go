// nnm-rss — личные RSS-ленты NNMClub по образцу lostfilmfeed.byalex.dev.
// Вход — свой логин + пароль (bcrypt; не данные трекера — у nnm постоянного
// ключа нет, он минтит уникальный на каждое скачивание). Сессия — кука s со
// случайным WebToken; ленты — /rss/<секретный токен> (поле Passkey, историческое
// имя). Магниты в ленте — только btih без announce: пиры клиент находит через
// DHT/PEX, статистика на трекере не считается.
//
// GET  /                     — публичная главная + «Моя подписка» (веб)
// POST /api/login            — вход/регистрация (логин+пароль, кука s)
// POST /api/logout, POST /api/passwd
// GET  /api/me               — мой профиль (подписки, история, лента)
// POST /api/subs, PATCH/DELETE /api/subs/{id}
// GET  /rss/{токен}          — личная лента (+/all, +/top/{дни})
// GET  /healthz
//
// Состояние: JSON в DATA_DIR (том /data), без внешней БД. Кэши в памяти TTL.
// Старые профили (вход по коду) открываются прежней кукой pk, пока не зададут
// логин/пароль кнопкой «Задать логин и пароль».
package main

import (
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
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
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

var rev = "unknown" // проставляется сборкой: -ldflags -X main.rev=…

//go:embed ui.html
var uiHTML []byte

var (
	ttl = envInt("TTL", 600)
	tz  = time.FixedZone("user", envInt("TZ_OFFSET", 4)*3600) // часовой пояс pubDate в ленте

	cacheMu      sync.Mutex
	rssCache     = map[string]rssCacheEntry{}  // родные ленты подписок: kind:id → body
	resolveCache = map[string]resolveEntry{}   // тема/пост → info-hash
	feedCache    = map[string]feedCacheEntry{} // готовый XML ленты: код профиля → байты
)

type rssCacheEntry struct {
	ts   time.Time
	body string
}

type resolveEntry struct {
	ts  time.Time
	rel Release
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

var (
	passkeyRe = regexp.MustCompile(`^[a-f0-9]{32}$`)
	loginRe   = regexp.MustCompile(`^[a-z0-9_.-]{3,32}$`)
)

// sessionCookie — кука сессии s: значение — WebToken профиля (отдельный
// секрет, от токена лент не производен)
func sessionCookie(tok string, r *http.Request) *http.Cookie {
	return &http.Cookie{Name: "s", Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: isHTTPS(r),
		MaxAge: 5 * 365 * 24 * 3600}
}

// currentProfile — профиль по куке сессии s (WebToken); наследие: старая
// кука pk со входом по коду тоже открывает страницу. Вызывать под stateMu.
func currentProfile(r *http.Request) *Profile {
	if c, err := r.Cookie("s"); err == nil {
		tok := strings.ToLower(strings.TrimSpace(c.Value))
		if passkeyRe.MatchString(tok) {
			if p := profileByWebToken(tok); p != nil {
				return p
			}
		}
	}
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

type feedEnclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type feedItem struct {
	Title     string         `xml:"title"`
	Link      string         `xml:"link"`
	Desc      string         `xml:"description"`
	Guid      feedGuid       `xml:"guid"`
	PubDate   string         `xml:"pubDate"`
	Enclosure *feedEnclosure `xml:"enclosure,omitempty"`
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

// buildFeed — XML личной ленты, новые сверху. all=false — авто-лента:
// подписки-раздачи с тумблером «авто». all=true — лента разделов: только
// подписки-разделы (поиск нового), без раздач из авто.
// Ссылки — магниты (btih без трекеров, DHT), в описании — постер и
// описание фильма со страницы раздачи.
// Возвращает XML и items для истории «Моя подписка»; из кэша — hist == nil
func buildFeed(p *Profile, all bool) ([]byte, []HistItem, error) {
	variant := strconv.FormatBool(all)
	cacheMu.Lock()
	if e, ok := feedCache[p.Passkey+"|"+variant]; ok && time.Since(e.ts) < time.Duration(ttl)*time.Second {
		cacheMu.Unlock()
		return e.body, nil, nil
	}
	cacheMu.Unlock()

	type keyItem struct {
		it   feedItem
		at   time.Time
		gid  string
		tid  int    // тема — для резолва главной раздачи
		orig string // исходная ссылка на тему (в описании)
		drop bool   // вырезан фильтром ленты
	}
	byGUID := map[string]keyItem{}

	addTopic := func(topicID int, title string, at time.Time, desc string, s *Sub,
		inc, exc *regexp.Regexp) {
		if title == "" {
			return
		}
		if inc != nil && !inc.MatchString(title) {
			return
		}
		if exc != nil && exc.MatchString(title) {
			return
		}
		orig := nnmBase + "/forum/viewtopic.php?t=" + strconv.Itoa(topicID)
		byGUID["t"+strconv.Itoa(topicID)] = keyItem{
			it: feedItem{
				Title:   title,
				Link:    orig, // магнит проставим после резолва
				Desc:    desc,
				Guid:    feedGuid{Value: "nnm-topic" + strconv.Itoa(topicID), IsPermaLink: false},
				PubDate: at.In(tz).Format(time.RFC1123Z),
			},
			at:   at,
			gid:  "t" + strconv.Itoa(topicID),
			tid:  topicID,
			orig: orig,
		}
	}

	for _, s := range p.Subs {
		if !s.Enabled {
			continue
		}
		if all {
			// лента разделов: только подписки-разделы (поиск нового), без раздач из авто
			if s.Kind != "forum" {
				continue
			}
		} else if !s.Auto {
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

		if s.Kind == "topic" {
			// подписка на раздачу: одна позиция — текущая ГЛАВНАЯ раздача темы
			// (гостю виден только её магнит; эпизодические раздачи обновляются —
			// новая версия = новый hash = новый item ниже по ходу резолва)
			its := parseRSSItems(body)
			if len(its) == 0 {
				continue // в теме ничего нового с последнего опроса ленты
			}
			latest := its[0]
			for _, it := range its {
				if it.Date.After(latest.Date) {
					latest = it
				}
			}
			title := cleanTitle(latest.Title)
			if title == "" {
				title = s.Title
			}
			addTopic(s.NNMID, title, latest.Date, s.Title, s, inc, exc)
			continue
		}

		// подписка на раздел: каждая новая раздача = тема = позиция
		for _, it := range parseRSSItems(body) {
			addTopic(it.ID, cleanTitle(it.Title), it.Date, s.Title, s, inc, exc)
		}
	}

	items := make([]keyItem, 0, len(byGUID))
	for _, v := range byGUID {
		items = append(items, v)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].at.After(items[j].at) })

	// только новые фильмы и апгрейды качества: что уже выпускали — не повторяем
	// (ранг выпущенного хранится в TopSeen профиля)
	fresh := items[:0]
	released := map[string]int{}
	for _, k := range items {
		key := filmKey(k.it.Title)
		rank := filmQualityRank(k.it.Title)
		if (p.TopSeen != nil) && p.TopSeen[key] >= rank {
			continue
		}
		if rank > released[key] {
			released[key] = rank
		}
		fresh = append(fresh, k)
	}
	items = fresh // новые сверху
	if len(items) > 100 {
		items = items[:100]
	}

	// фильтр ленты — у каждой свой; матчит название И описание раздачи
	// (язык/перевод живут в описании, не в названии)
	feedKey := "auto"
	if all {
		feedKey = "all"
	}
	inc, exc := compileFilter(p.Filters[feedKey])

	// резолв главных раздач параллельно (первая сборка — до сотни страниц)
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for idx := range items {
		wg.Add(1)
		k := &items[idx]
		go func(k *keyItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			rel, err := resolveRelease(k.tid)
			if err != nil {
				// резолв не удался (трекер притормозил) — item без магнита
				// клиенту бесполезен: не выпускаем, следующий опрос повторит
				log.Printf("resolve %s: %v", k.it.Title, err)
				k.drop = true
				return
			}
			if rel.Hash == "" {
				k.drop = true // без магнита ссылка мёртвая для клиента
				return
			}
			// версия раздачи в guid: батч обновился (новый hash) — новый item
			if rel.Hash != "" {
				k.it.Guid.Value = k.it.Guid.Value + "-" + rel.Hash[:8]
				applyTorrent(&k.it, rel.Hash)
			}
			if tt := imdbTT(rel.IMDb); tt != "" {
				rel.IMDbRating, rel.IMDbVotes = imdbRating(tt)
			}
			if rel.MAL != "" {
				rel.MALRating, rel.MALVotes = malRating(rel.MAL)
			}
			k.it.Desc = itemDescr(rel, k.orig, rel.Hash != "")
			if (inc != nil && !inc.MatchString(k.it.Title+" "+rel.Descr+" "+rel.TechString())) ||
				(exc != nil && exc.MatchString(k.it.Title+" "+rel.Descr+" "+rel.TechString())) {
				k.drop = true
			}
		}(k)
	}
	wg.Wait()

	kept := items[:0]
	for _, k := range items {
		if !k.drop {
			kept = append(kept, k)
		}
	}
	items = kept

	var f feedRSS
	f.Version = "2.0"
	f.Channel.Title = "NNM-Club — " + map[bool]string{false: "авто ", true: "разделы "}[all] + p.Passkey[:8] + "…"
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
	feedCache[p.Passkey+"|"+variant] = feedCacheEntry{ts: time.Now(), body: out}
	cacheMu.Unlock()
	return out, hist, nil
}

// sprintInts — ключ кэша из списка интов
func sprintInts(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, ",")
}

// compileFilter — регэкспы фильтра ленты (include/exclude), невалидные пропускаем
func compileFilter(ff *FeedFilter) (inc, exc *regexp.Regexp) {
	if ff == nil {
		return nil, nil
	}
	if ff.Include != "" {
		if re, err := regexp.Compile(`(?i)` + ff.Include); err == nil {
			inc = re
		}
	}
	if ff.Exclude != "" {
		if re, err := regexp.Compile(`(?i)` + ff.Exclude); err == nil {
			exc = re
		}
	}
	return
}

// imdbTT — id ttXXXXXXX из ссылки IMDb
func imdbTT(u string) string {
	i := strings.Index(u, "/tt")
	if i < 0 {
		return ""
	}
	tt := u[i+1:]
	if j := strings.IndexByte(tt, '/'); j > 0 {
		tt = tt[:j]
	}
	return tt
}

// applyTorrent — магнет основной и мгновенный: есть всегда, ждать нечего,
// enclosure с ним сразу (в тесте юзера nasctl заполнял поле URL магнетом
// из enclosure). Если DHT-резолвер уже собрал .torrent — ссылка http
// (эталон lostfilmfeed так и делал). guid не трогаем: смена link не делает
// item «новым».
func applyTorrent(it *feedItem, hash string) {
	if hash == "" {
		return
	}
	it.Link = magnetLink(hash)
	it.Enclosure = &feedEnclosure{URL: magnetLink(hash), Length: 1, Type: "application/x-bittorrent"}
	if selfURL == "" {
		return
	}
	prefetchTorrent(hash)
	if torrentReady(hash) {
		st, err := os.Stat(torrentPath(hash))
		if err != nil {
			return
		}
		u := selfURL + "/t/" + strings.ToLower(hash) + ".torrent"
		it.Link = u
		it.Enclosure = &feedEnclosure{URL: u, Length: st.Size(), Type: "application/x-bittorrent"}
	}
}

// itemDescr — карточка как на странице раздачи. Постер — отдельной строкой
// сверху (колонка справа в nasctl скукоживалась: он игнорирует CSS-ширину
// картинки после её загрузки), поэтому width-атрибут + max-width в CSS.
// Ссылки голым текстом — в nasctl <a> не активны, автолинки только на текст.
func itemDescr(rel Release, topicURL string, magnet bool) string {
	var b strings.Builder
	b.WriteString(`<div style="font-family:sans-serif;font-size:13px;line-height:1.45">`)
	if rel.Poster != "" {
		b.WriteString(`<div style="margin:0 0 8px"><img src="` + rel.Poster + `" width="300" style="max-width:100%;border-radius:8px"></div>`)
	}
	if rel.RatingImg != "" || rel.IMDbRating != "" || rel.MALRating != "" {
		b.WriteString(`<div style="margin:0 0 10px">`)
		if rel.RatingImg != "" {
			b.WriteString(`<img src="` + rel.RatingImg + `" alt="Кинопоиск" style="vertical-align:middle;margin-right:10px">`)
		}
		if rel.IMDbRating != "" {
			votes := ""
			if rel.IMDbVotes != "" {
				votes = ` <span style="font-size:11px;color:#bbb">(` + rel.IMDbVotes + `)</span>`
			}
			b.WriteString(`<span style="display:inline-block;vertical-align:middle;border-radius:8px;background:#1f1f1f;color:#fff;padding:7px 10px;white-space:nowrap;margin-right:10px">` +
				`<span style="background:#f5c518;color:#000;font-weight:bold;border-radius:4px;padding:1px 6px;font-size:11px">IMDb</span>` +
				` <span style="font-size:18px;font-weight:bold">` + rel.IMDbRating + `</span>` + votes + `</span>`)
		}
		if rel.MALRating != "" {
			votes := ""
			if rel.MALVotes != "" {
				votes = ` <span style="font-size:11px;color:#bbb">(` + rel.MALVotes + `)</span>`
			}
			b.WriteString(`<span style="display:inline-block;vertical-align:middle;border-radius:8px;background:#1f1f1f;color:#fff;padding:7px 10px;white-space:nowrap">` +
				`<span style="background:#2e51a2;color:#fff;font-weight:bold;border-radius:4px;padding:1px 6px;font-size:11px">MAL</span>` +
				` <span style="font-size:18px;font-weight:bold">` + rel.MALRating + `</span>` + votes + `</span>`)
		}
		b.WriteString(`</div>`)
	}
	for _, t := range rel.Tech {
		if t.Name == "Описание" {
			b.WriteString(`<div><br></div>`) // пустая строка перед описанием: маржины nasctl не рендерит
		}
		v := strings.ReplaceAll(t.Value, "¶", "<br>")
		b.WriteString(`<div style="margin:2px 0"><b>` + t.Name + `:</b> ` + v + `</div>`)
	}
	if rel.TorName != "" {
		b.WriteString(`<div style="margin:10px 0 0">Торрент: ` + rel.TorName)
		if rel.TorSize != "" {
			b.WriteString(` · ` + rel.TorSize)
		}
		b.WriteString(` · список файлов — на странице раздачи</div>`)
	}
	b.WriteString(`<div style="margin:8px 0 0;font-size:12px">`)
	b.WriteString(topicURL)
	if rel.KP != "" {
		b.WriteString(` · ` + rel.KP)
	}
	if rel.IMDb != "" {
		b.WriteString(` · ` + rel.IMDb)
	}
	b.WriteString(`</div></div>`)
	return b.String()
}

// trackerPage — страница топа трекера (категории + строки) с серверными
// фильтрами: tm= окно дней, sds= тип раздач (вместе они активируются,
// по отдельности трекер игнорирует); кэш на TTL
func trackerPage(days, sdstype int) (string, error) {
	key := fmt.Sprintf("page:tracker:%d:%d", days, sdstype)
	cacheMu.Lock()
	if e, ok := rssCache[key]; ok && time.Since(e.ts) < time.Duration(ttl)*time.Second {
		cacheMu.Unlock()
		return e.body, nil
	}
	cacheMu.Unlock()

	u := fmt.Sprintf("%s/forum/tracker.php?o=10&tm=%d", nnmBase, days)
	if sdstype >= 0 {
		u += "&sds=" + strconv.Itoa(sdstype)
	}
	body, err := fetchNNM(u, "")
	if err != nil {
		return "", err
	}
	cacheMu.Lock()
	rssCache[key] = rssCacheEntry{ts: time.Now(), body: body}
	cacheMu.Unlock()
	return body, nil
}

// buildTopFeed — «популярное» за days дней: топ по сидам (tracker.php?o=10),
// разделы — выбранные юзером верхние категории (по умолчанию видео), строки
// старше окна отсекаются по времени добавления (параметр da= трекер
// игнорирует). Магниты — с announce юзера.
func buildTopFeed(p *Profile, days int) ([]byte, []HistItem, map[string]int, error) {
	// типы раздач — мультивыбор: каждый выбранный тип тянем своей страницей
	// (sds одиночный), пусто = одна страница без фильтра типа
	types := p.TopSds
	if len(types) == 0 {
		types = []int{-1}
	}
	key := p.Passkey + "|top" + strconv.Itoa(days) + ":" + sprintInts(types)
	cacheMu.Lock()
	if e, ok := feedCache[key]; ok && time.Since(e.ts) < time.Duration(ttl)*time.Second {
		cacheMu.Unlock()
		return e.body, nil, nil, nil // кэш: история уже записана при сборке
	}
	cacheMu.Unlock()

	pages := make([]string, len(types))
	var wg sync.WaitGroup
	for i, t := range types {
		wg.Add(1)
		go func(i, t int) {
			defer wg.Done()
			if body, err := trackerPage(days, t); err == nil {
				pages[i] = body
			} else {
				log.Printf("top tm=%d sds=%d: %v", days, t, err)
			}
		}(i, t)
	}
	wg.Wait()

	roots := map[int]bool{}
	for _, id := range p.TopRoots {
		roots[id] = true
	}
	if len(roots) == 0 {
		roots = nnmVideoRoots // настройка не сохранялась — видео по умолчанию
	}
	cutoff := time.Now().AddDate(0, 0, -days).Unix()

	// строки всех страниц: allowed-поддерево едино, дедуп по теме,
	// затем по фильму (разные темы одной раздачи больше не идут пачкой)
	allowed := map[int]bool{}
	seen := map[int]bool{}
	type keyItem struct {
		it   feedItem
		at   time.Time
		tid  int
		drop bool // вырезан фильтром ленты
	}
	var items []keyItem
	var rows []topRow
	for _, body := range pages {
		allowed = subtreeFor(body, roots)
		for _, r := range parseTrackerTop(body) {
			if seen[r.TopicID] {
				continue
			}
			seen[r.TopicID] = true
			if !allowed[r.ForumID] || r.Added < cutoff {
				continue
			}
			rows = append(rows, r)
		}
	}

	// один фильм — одна тема: лучшая по качеству, при равенстве — свежее
	rows = dedupeTopRows(rows)

	for _, r := range rows {
		if len(items) >= 50 {
			break
		}
		orig := nnmBase + "/forum/viewtopic.php?t=" + strconv.Itoa(r.TopicID)
		items = append(items, keyItem{
			it: feedItem{
				Title:   r.Title,
				Link:    orig,
				Guid:    feedGuid{Value: "nnm-topic" + strconv.Itoa(r.TopicID), IsPermaLink: false},
				PubDate: time.Unix(r.Added, 0).In(tz).Format(time.RFC1123Z),
			},
			at:  time.Unix(r.Added, 0),
			tid: r.TopicID,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].at.After(items[j].at) })

	// фильтр топ-ленты — свой; матчит название и описание раздачи
	inc, exc := compileFilter(p.Filters["top"])

	sem := make(chan struct{}, 8)
	var wg2 sync.WaitGroup
	for idx := range items {
		wg2.Add(1)
		k := &items[idx]
		go func(k *keyItem) {
			defer wg2.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			rel, err := resolveRelease(k.tid)
			if err != nil {
				// item без магнита клиенту бесполезен: не выпускаем
				log.Printf("resolve top %s: %v", k.it.Title, err)
				k.drop = true
				return
			}
			if rel.Hash == "" {
				k.drop = true
				return
			}
			// не-русская раздача: ни кириллицы в названии, ни русского описания
			if !hasCyrillic(k.it.Title) && !hasCyrillic(rel.Descr) {
				k.drop = true
				return
			}
			// стоп-лист релизеров
			if strings.Contains(strings.ToLower(k.it.Title), "ultradox") {
				k.drop = true
				return
			}
			if rel.Hash != "" {
				k.it.Guid.Value += "-" + rel.Hash[:8]
				applyTorrent(&k.it, rel.Hash)
			}
			if tt := imdbTT(rel.IMDb); tt != "" {
				rel.IMDbRating, rel.IMDbVotes = imdbRating(tt)
			}
			if rel.MAL != "" {
				rel.MALRating, rel.MALVotes = malRating(rel.MAL)
			}
			k.it.Desc = itemDescr(rel, nnmBase+"/forum/viewtopic.php?t="+strconv.Itoa(k.tid), rel.Hash != "")
			if (inc != nil && !inc.MatchString(k.it.Title+" "+rel.Descr+" "+rel.TechString())) ||
				(exc != nil && exc.MatchString(k.it.Title+" "+rel.Descr+" "+rel.TechString())) {
				k.drop = true
			}
		}(k)
	}
	wg2.Wait()

	kept := items[:0]
	for _, k := range items {
		if !k.drop {
			kept = append(kept, k)
		}
	}
	items = kept

	// только новые фильмы и апгрейды качества — и уже ПОСЛЕ drop-фильтра:
	// что отфильтровано/не разрешилось, в память не попадает и вернётся позже
	fresh := items[:0]
	released := map[string]int{}
	for _, k := range items {
		key := filmKey(k.it.Title)
		rank := filmQualityRank(k.it.Title)
		if (p.TopSeen != nil) && p.TopSeen[key] >= rank {
			continue
		}
		if rank > released[key] {
			released[key] = rank
		}
		fresh = append(fresh, k)
	}
	items = fresh

	var f feedRSS
	f.Version = "2.0"
	f.Channel.Title = "NNM-Club — популярное за " + strconv.Itoa(days) + " дней"
	f.Channel.Link = nnmBase
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
	out, err := xml.Marshal(&f)
	if err != nil {
		return nil, nil, nil, err
	}
	out = append([]byte(xml.Header), out...)

	cacheMu.Lock()
	feedCache[key] = feedCacheEntry{ts: time.Now(), body: out}
	cacheMu.Unlock()

	// что выпустили (filmKey → ранг качества) — хендлер запишет в профиль
	return out, hist, released, nil
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
	initTorrents()

	mux := http.NewServeMux()

	// отдача собранных DHT-резолвером .torrent: /t/<hash>.torrent
	mux.HandleFunc("GET /t/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/t/")
		hash := strings.TrimSuffix(name, ".torrent")
		if len(hash) != 40 || strings.Trim(hash, "0123456789abcdefABCDEF") != "" {
			http.NotFound(w, r)
			return
		}
		path := torrentPath(hash)
		if !torrentReady(hash) || !torrentFileValid(hash) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-bittorrent")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFile(w, r, path)
	})

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

	// ----- вход/регистрация: классический POST формой с редиректом (PRG).
	// Реальная навигация после сабмита — единственное, на что Safari
	// гарантированно показывает «сохранить пароль» (fetch c preventDefault
	// в динамических формах он распознаёт через раз).

	credPOST := func(w http.ResponseWriter, r *http.Request, create bool) {
		login := strings.ToLower(strings.TrimSpace(r.PostFormValue("login")))
		password := r.PostFormValue("password")
		fail := func(msg string) {
			http.Redirect(w, r, "/?e="+url.QueryEscape(msg), http.StatusSeeOther)
		}
		if !loginRe.MatchString(login) {
			fail("логин — 3–32 символа: латиница, цифры, . _ -")
			return
		}
		if len(password) < 4 {
			fail("пароль — минимум 4 символа")
			return
		}

		stateMu.Lock()
		p := profileByLogin(login)
		if create {
			if p != nil {
				stateMu.Unlock()
				fail("такой логин уже занят")
				return
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
			if err != nil {
				stateMu.Unlock()
				fail("не получилось сохранить пароль")
				return
			}
			p = profileByPasskey(randHex(16)) // новый профиль со случайным токеном лент
			p.Login, p.PassHash, p.WebToken = login, string(hash), randHex(16)
			saveState()
			log.Printf("новый профиль %s", login)
		} else {
			if p == nil || p.PassHash == "" {
				stateMu.Unlock()
				fail("нет такого логина или пароль не задан")
				return
			}
			if bcrypt.CompareHashAndPassword([]byte(p.PassHash), []byte(password)) != nil {
				stateMu.Unlock()
				fail("неправильный пароль")
				return
			}
			if p.WebToken == "" {
				p.WebToken = randHex(16)
				saveState()
			}
		}
		tok := p.WebToken
		stateMu.Unlock()

		http.SetCookie(w, sessionCookie(tok, r))
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) { credPOST(w, r, false) })
	mux.HandleFunc("POST /api/register", func(w http.ResponseWriter, r *http.Request) { credPOST(w, r, true) })

	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		// чистим обе куки: сессию s и наследие pk (вход по коду), иначе
		// «выйти» не выпускает со старой куки
		http.SetCookie(w, &http.Cookie{Name: "s", Value: "", Path: "/", MaxAge: -1})
		http.SetCookie(w, &http.Cookie{Name: "pk", Value: "", Path: "/", MaxAge: -1})
		writeJSON(w, 200, map[string]string{"ok": "1"})
	})

	// ----- смена/задание пароля — отдельная страница с классическим POST.
	// Профилю без логина (наследие входа по коду) задаёт логин и пароль;
	// иначе просит старый пароль и ротирует токен сессии — все другие
	// входы выпадут. Реальная навигация → Safari показывает «обновить пароль»

	passwdPage := func(w http.ResponseWriter, r *http.Request, errMsg string) {
		stateMu.Lock()
		p := currentProfile(r)
		login := ""
		if p != nil {
			login = p.Login
		}
		stateMu.Unlock()
		setup := login == ""
		oldRow := ""
		if !setup {
			oldRow = `<div class="row"><input name="old" type="password" placeholder="старый пароль" autocomplete="current-password" required></div>`
		}
		errRow := ""
		if errMsg != "" {
			errRow = `<p class="msg err">` + html.EscapeString(errMsg) + `</p>`
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="ru"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>Пароль — NNM-Club RSS</title>
<style>
:root{--accent:#c2410c;--accent-txt:#fff;--bg:#f2f4f7;--card:#fff;--text:#222;--muted:#7a828c;--line:#dde2e8;--err:#c0392b;--field:#fff}
@media (prefers-color-scheme: dark){:root{--accent:#c2623d;--accent-txt:#2c2525;--bg:#221f20;--card:#2c2828;--text:#f0e6c8;--muted:#8b8481;--line:#413b3b;--err:#f85e50;--field:#353030}}
*{box-sizing:border-box}body{margin:0;font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:var(--bg);color:var(--text)}
.wrap{max-width:420px;margin:60px auto;padding:0 16px}
.card{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:20px}
h1{font-size:18px;margin:0 0 6px}.hint{color:var(--muted);font-size:13px;margin:0 0 14px}
.row{display:flex;gap:10px;margin-bottom:10px}.row>*{flex:1}
input{padding:9px 11px;border:1px solid var(--line);border-radius:7px;font:inherit;background:var(--field);color:var(--text);width:100%%}
input:focus{outline:none;border-color:var(--accent)}
button{padding:9px 16px;border:0;border-radius:7px;background:var(--accent);color:var(--accent-txt);font:inherit;cursor:pointer}
.msg{margin:0 0 12px;font-size:14px}.msg.err{color:var(--err)}
a{color:var(--muted);font-size:13px;text-decoration:none}
</style></head><body><div class="wrap"><div class="card">
<h1>%s</h1>
<p class="hint">Смена пароля завершит сессии на других устройствах. URL лент не меняются.</p>
%s
<form method="post" action="/passwd">
<div class="row"><input name="login" type="text" placeholder="логин" value="%s" autocomplete="username" required></div>
%s
<div class="row"><input name="new" type="password" placeholder="новый пароль" autocomplete="new-password" required></div>
<div class="row"><button type="submit">Сохранить</button></div>
</form>
<p style="margin-top:14px"><a href="/">← назад</a></p>
</div></div></body></html>`,
			map[bool]string{true: "Задать логин и пароль", false: "Смена пароля"}[setup],
			errRow, html.EscapeString(login), oldRow)
	}
	mux.HandleFunc("GET /passwd", func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		p := currentProfile(r)
		stateMu.Unlock()
		if p == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		passwdPage(w, r, "")
	})
	mux.HandleFunc("POST /passwd", func(w http.ResponseWriter, r *http.Request) {
		in := struct{ Login, Old, New string }{
			Login: strings.ToLower(strings.TrimSpace(r.PostFormValue("login"))),
			Old:   r.PostFormValue("old"),
			New:   r.PostFormValue("new"),
		}

		stateMu.Lock()
		p := currentProfile(r)
		if p == nil {
			stateMu.Unlock()
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if p.Login == "" {
			if !loginRe.MatchString(in.Login) {
				stateMu.Unlock()
				passwdPage(w, r, "логин — 3–32 символа: латиница, цифры, . _ -")
				return
			}
			if profileByLogin(in.Login) != nil {
				stateMu.Unlock()
				passwdPage(w, r, "такой логин уже занят")
				return
			}
			p.Login = in.Login
		} else if bcrypt.CompareHashAndPassword([]byte(p.PassHash), []byte(in.Old)) != nil {
			stateMu.Unlock()
			passwdPage(w, r, "старый пароль не подходит")
			return
		}
		if len(in.New) < 4 {
			stateMu.Unlock()
			passwdPage(w, r, "новый пароль — минимум 4 символа")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(in.New), 12)
		if err != nil {
			stateMu.Unlock()
			passwdPage(w, r, "не получилось сохранить пароль")
			return
		}
		p.PassHash = string(hash)
		p.WebToken = randHex(16)
		saveState()
		tok := p.WebToken
		login := p.Login
		stateMu.Unlock()

		http.SetCookie(w, sessionCookie(tok, r))
		log.Printf("passwd %s", login)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		p := currentProfile(r)
		if p == nil {
			stateMu.Unlock()
			writeErr(w, 401, "не авторизован")
			return
		}
		base := baseURL(r)
		resp := map[string]any{
			"login":        p.Login,
			"passkey":      p.Passkey,
			"feed_url":     base + "/rss/" + p.Passkey,          // авто: подписки с тумблером «авто»
			"feed_url_all": base + "/rss/" + p.Passkey + "/all", // общая: все подписки
			"subs":         p.Subs,
			"filters":      p.Filters,
			"history_auto": p.HistoryAuto,
			"history_all":  p.HistoryAll,
		}
		stateMu.Unlock()
		writeJSON(w, 200, resp)
	})

	// ----- подписки

	mux.HandleFunc("POST /api/subs", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			URL, Filter, Exclude string
			Target               string // auto | forum — куда добавляем
		}
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

		title, poster := strconv.Itoa(id), ""
		if kind == "forum" {
			if body, err := fetchNNM(subRSSURL(&Sub{Kind: kind, NNMID: id}), ""); err == nil {
				if ch := parseRSSChannel(body); ch != "" {
					title = ch
				}
			}
		} else {
			// тему тянем с реальным названием и постером; ссылку на пост
			// конвертируем в тему (лента тем и есть наши посты-эпизоды)
			topicID, t, po, err := topicInfo(kind, id)
			if err != nil {
				writeErr(w, 400, err.Error())
				return
			}
			kind, id, title, poster = "topic", topicID, t, po
		}

		stateMu.Lock()
		defer stateMu.Unlock()
		p := currentProfile(r)
		if p == nil {
			writeErr(w, 401, "не авторизован")
			return
		}
		for _, s := range p.Subs {
			if s.Kind == kind && s.NNMID == id {
				writeErr(w, 400, "уже в подписках")
				return
			}
		}

		s := &Sub{ID: randHex(8), Kind: kind, NNMID: id, Title: title, Poster: poster,
			Filter: in.Filter, Exclude: in.Exclude, Auto: kind == "topic", Enabled: true}
		p.Subs = append(p.Subs, s)
		invalidateFeed(p.Passkey)
		saveState()
		log.Printf("sub+ %.8s… %s:%d (%s)", p.Passkey, kind, id, title)
		writeJSON(w, 200, s)
	})

	mux.HandleFunc("PATCH /api/subs/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Filter, Exclude *string
			Enabled, Auto   *bool
		}
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
			writeErr(w, 401, "не авторизован")
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
		if in.Auto != nil {
			s.Auto = *in.Auto
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
			writeErr(w, 401, "не авторизован")
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

	// ----- личные ленты: /rss/<passkey> — авто (тумблер «авто»),
	// /rss/<passkey>/all — общая; как rssfeeds/<uuid> у lostfilmfeed

	mux.HandleFunc("GET /rss/{passkey}", func(w http.ResponseWriter, r *http.Request) {
		serveFeed(w, r, false)
	})
	mux.HandleFunc("GET /rss/{passkey}/all", func(w http.ResponseWriter, r *http.Request) {
		serveFeed(w, r, true)
	})

	// ----- (был /api/rekey — смена кода; с логином/паролем неактуален)

	// ----- фильтры лент (у каждой свой, блок настроек одинаковый)

	mux.HandleFunc("PUT /api/filters", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Feed, Include, Exclude string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, 400, "неправильный запрос")
			return
		}
		if in.Feed != "auto" && in.Feed != "all" && in.Feed != "top" {
			writeErr(w, 400, "feed must be one of auto|all|top")
			return
		}
		if err := validateRegexes(in.Include, in.Exclude); err != nil {
			writeErr(w, 400, err.Error())
			return
		}

		stateMu.Lock()
		defer stateMu.Unlock()
		p := currentProfile(r)
		if p == nil {
			writeErr(w, 401, "не авторизован")
			return
		}
		if p.Filters == nil {
			p.Filters = map[string]*FeedFilter{}
		}
		p.Filters[in.Feed] = &FeedFilter{Include: in.Include, Exclude: in.Exclude}
		cacheMu.Lock()
		for k := range feedCache { // все варианты лент этого юзера пересоберутся
			if strings.HasPrefix(k, p.Passkey+"|") {
				delete(feedCache, k)
			}
		}
		cacheMu.Unlock()
		saveState()
		writeJSON(w, 200, map[string]string{"ok": "1"})
	})

	// ----- разделы «Ленты разделов»: галки по корням, внутри — подписки-листья

	mux.HandleFunc("GET /api/sections", func(w http.ResponseWriter, r *http.Request) {
		body, err := trackerPage(7, -1)
		if err != nil {
			writeErr(w, 502, "NNM недоступен: "+err.Error())
			return
		}
		lr := leafRoots(body)

		stateMu.Lock()
		p := currentProfile(r)
		selected := []int{}
		if p != nil {
			seen := map[int]bool{}
			for _, s := range p.Subs {
				if s.Kind != "forum" {
					continue
				}
				root := s.RootID
				if root == 0 {
					if rr, ok := lr[s.NNMID]; ok {
						root = rr.ID
					}
				}
				if root != 0 && !seen[root] {
					seen[root] = true
					selected = append(selected, root)
				}
			}
		}
		stateMu.Unlock()
		writeJSON(w, 200, map[string]any{
			"categories": parseTrackerCategories(body),
			"selected":   selected,
		})
	})

	mux.HandleFunc("PUT /api/sections", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ IDs []int }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, 400, "неправильный запрос")
			return
		}
		if len(in.IDs) > 16 {
			writeErr(w, 400, "слишком много разделов")
			return
		}
		body, err := trackerPage(7, -1)
		if err != nil {
			writeErr(w, 502, "NNM недоступен: "+err.Error())
			return
		}
		lr := leafRoots(body)
		names := leafNames(body)
		cats := map[int]string{}
		for _, c := range parseTrackerCategories(body) {
			cats[c.ID] = c.Name
		}

		wanted := map[int]bool{}
		for _, id := range in.IDs {
			wanted[id] = true
		}

		stateMu.Lock()
		defer stateMu.Unlock()
		p := currentProfile(r)
		if p == nil {
			writeErr(w, 401, "не авторизован")
			return
		}

		// нормализуем существующие подписки-разделы (проставляем корень)
		for _, s := range p.Subs {
			if s.Kind == "forum" && s.RootID == 0 {
				if rr, ok := lr[s.NNMID]; ok {
					s.RootID, s.Root = rr.ID, rr.Name
				}
			}
		}
		// убираем листья невыбранных корней
		kept := p.Subs[:0]
		for _, s := range p.Subs {
			if s.Kind == "forum" && s.RootID != 0 && !wanted[s.RootID] {
				continue
			}
			kept = append(kept, s)
		}
		p.Subs = kept

		// добавляем недостающие листья выбранных корней
		exists := map[int]bool{}
		for _, s := range p.Subs {
			if s.Kind == "forum" {
				exists[s.NNMID] = true
			}
		}
		for root := range wanted {
			rootName := cats[root]
			for leaf, rr := range lr {
				if rr.ID != root || exists[leaf] {
					continue
				}
				p.Subs = append(p.Subs, &Sub{
					ID: randHex(8), Kind: "forum", NNMID: leaf,
					Title: names[leaf], RootID: root, Root: rootName, Enabled: true,
				})
				exists[leaf] = true
			}
			// корень-одиночка (без подфорумов) — подписка на сам корень
			if !exists[root] && rootName != "" {
				p.Subs = append(p.Subs, &Sub{
					ID: randHex(8), Kind: "forum", NNMID: root,
					Title: rootName, RootID: root, Root: rootName, Enabled: true,
				})
			}
		}

		invalidateFeed(p.Passkey)
		saveState()
		log.Printf("sections %.8s…: корней %d", p.Passkey, len(wanted))
		writeJSON(w, 200, map[string]string{"ok": "1"})
	})

	// ----- топ-ленты: популярное за N дней, разделы — из настройки профиля

	mux.HandleFunc("GET /rss/{passkey}/top/{days}", func(w http.ResponseWriter, r *http.Request) {
		pk := strings.ToLower(r.PathValue("passkey"))
		if !passkeyRe.MatchString(pk) {
			http.Error(w, "нет такой ленты", 404)
			return
		}
		days, err := strconv.Atoi(r.PathValue("days"))
		if err != nil || days < 1 || days > 30 {
			http.Error(w, "дни: 1..30", 404)
			return
		}

		stateMu.Lock()
		var p *Profile
		if src := profileByPassKeyExisting(pk); src != nil {
			cp := *src // снапшот: TopRoots читаем, ленту строим без блокировки
			cp.Subs = nil
			if len(src.TopSeen) > 0 { // копия: в снапшоте только читаем
				cp.TopSeen = make(map[string]int, len(src.TopSeen))
				for k, v := range src.TopSeen {
					cp.TopSeen[k] = v
				}
			}
			p = &cp
		}
		stateMu.Unlock()
		if p == nil {
			http.Error(w, "нет такой ленты (откройте страницу сервиса и укажите passkey)", 404)
			return
		}

		body, hist, released, err := buildTopFeed(p, days)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}

		// запоминаем выпущенное: повторы и downgrade качества больше не придут
		if released != nil {
			stateMu.Lock()
			if live := profileByPassKeyExisting(pk); live != nil {
				if live.TopSeen == nil {
					live.TopSeen = map[string]int{}
				}
				changed := false
				for k, rank := range released {
					if live.TopSeen[k] < rank {
						live.TopSeen[k] = rank
						changed = true
					}
				}
				seenGUID := map[string]bool{}
				for _, h := range live.HistoryTop {
					seenGUID[h.GUID] = true
				}
				for _, h := range hist {
					if !seenGUID[h.GUID] {
						seenGUID[h.GUID] = true
						live.HistoryTop = append(live.HistoryTop, h)
						changed = true
					}
				}
				if len(live.HistoryTop) > 50 {
					live.HistoryTop = live.HistoryTop[len(live.HistoryTop)-50:]
					changed = true
				}
				if changed {
					saveState()
				}
			}
			stateMu.Unlock()
		}

		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		w.Write(body)
	})

	// ----- разделы для топ-лент: список + выбранное

	mux.HandleFunc("GET /api/forums", func(w http.ResponseWriter, r *http.Request) {
		body, err := trackerPage(7, -1)
		if err != nil {
			writeErr(w, 502, "NNM недоступен: "+err.Error())
			return
		}
		stateMu.Lock()
		p := currentProfile(r)
		selected := []int{}
		typesSel := []int{}
		if p != nil {
			selected = p.TopRoots
			typesSel = p.TopSds
		}
		stateMu.Unlock()
		writeJSON(w, 200, map[string]any{
			"categories": parseTrackerCategories(body),
			"selected":   selected,
			"types":      trackerOptions(body, "sds"),
			"types_sel":  typesSel,
		})
	})

	mux.HandleFunc("PUT /api/forums", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs   []int
			Types []int
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, 400, "неправильный запрос")
			return
		}
		bad := len(in.IDs) > 64 || len(in.Types) > 5
		for _, t := range in.Types {
			if t < 0 || t > 4 {
				bad = true
			}
		}
		if bad {
			writeErr(w, 400, "неправильные значения")
			return
		}

		stateMu.Lock()
		defer stateMu.Unlock()
		p := currentProfile(r)
		if p == nil {
			writeErr(w, 401, "не авторизован")
			return
		}
		p.TopRoots = in.IDs
		p.TopSds = in.Types
		cacheMu.Lock()
		for k := range feedCache {
			if strings.HasPrefix(k, p.Passkey+"|top") {
				delete(feedCache, k)
			}
		}
		cacheMu.Unlock()
		saveState()
		writeJSON(w, 200, map[string]string{"ok": "1"})
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

// serveFeed — сборка и отдача ленты + пополнение истории «Моя подписка»
func serveFeed(w http.ResponseWriter, r *http.Request, all bool) {
	pk := strings.ToLower(r.PathValue("passkey"))
	if !passkeyRe.MatchString(pk) {
		http.Error(w, "нет такой ленты", 404)
		return
	}

	// снапшот профиля под блокировкой, сборка ленты (с сетью) — без неё
	stateMu.Lock()
	var p *Profile
	if src := profileByPassKeyExisting(pk); src != nil {
		cp := *src
		cp.Subs = make([]*Sub, len(src.Subs))
		for i, s := range src.Subs {
			sc := *s
			cp.Subs[i] = &sc
		}
		p = &cp
	}
	stateMu.Unlock()

	if p == nil {
		http.Error(w, "нет такой ленты (откройте страницу сервиса и укажите passkey)", 404)
		return
	}
	body, hist, err := buildFeed(p, all)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// истории «Моя подписка»: у каждой ленты своя (из кэша hist == nil)
	if hist != nil {
		stateMu.Lock()
		if live := profileByPassKeyExisting(pk); live != nil {
			old := &live.HistoryAll
			if !all {
				old = &live.HistoryAuto
			}
			seen := map[string]bool{}
			for _, h := range *old {
				seen[h.GUID] = true
			}
			merged := *old
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
				*old = merged
				saveState()
			}
		}
		stateMu.Unlock()
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write(body)
}

// invalidateFeed — подписки изменились, обе ленты пересоберём (под stateMu)
func invalidateFeed(passkey string) {
	cacheMu.Lock()
	delete(feedCache, passkey+"|false")
	delete(feedCache, passkey+"|true")
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
