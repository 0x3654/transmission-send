package main

import (
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36"

const pageSize = 50

// форумы NNMClub без русского перевода — всегда исключаем из видео-топа
var nnmNoRussian = map[int]bool{
	780: true, // сериалы без русского перевода (украинская озвучка)
	781: true, // сериалы без озвученного перевода
}

// раздачи, которых не хотим видеть вообще (вхождение в название, без учёта регистра)
var blockedNames = []string{"ultradox"}

func blockedName(title string) bool {
	low := strings.ToLower(title)
	for _, b := range blockedNames {
		if strings.Contains(low, b) {
			return true
		}
	}
	return false
}

// корневые разделы «видео» на NNMClub (идентификаторы форумов)
var nnmVideoRoots = map[int]bool{
	216: true, 318: true, 220: true, 224: true, 1311: true, 256: true, 264: true, // кино
	1219: true, 768: true, 769: true, 713: true, // сериалы
	576: true,                       // документалистика
	724: true,                       // детское видео/мультфильмы
	620: true, 624: true, 628: true, // аниме
}

func fetch(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// NNMClub отдаёт cp1251, RUTOR — utf-8: валидный utf-8 не трогаем
	if utf8.Valid(raw) {
		return string(raw), nil
	}
	decoded, err := charmap.Windows1251.NewDecoder().Bytes(raw)
	if err != nil {
		decoded = raw
	}
	return string(decoded), nil
}

// ---------- общие регулярки

var (
	tagRe   = regexp.MustCompile(`<[^>]+>`)
	spaceRe = regexp.MustCompile(`\s+`)

	tdRe        = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	viewtopicRe = regexp.MustCompile(`viewtopic\.php\?t=(\d+)`)
	titleBRe    = regexp.MustCompile(`(?s)href="viewtopic\.php\?t=\d+"[^>]*><b>(.*?)</b>`)
	forumRe     = regexp.MustCompile(`href="tracker\.php\?f=(\d+)"[^>]*>([^<]+)`)
	downloadRe  = regexp.MustCompile(`href="download\.php\?id=(\d+)"`)
	addedRe     = regexp.MustCompile(`(\d{9,10})`)
	numRe       = regexp.MustCompile(`\d+`)

	optionRe   = regexp.MustCompile(`<option[^>]+value="(\d+)"[^>]*>([^<]*)</option>`)
	rutTorrent = regexp.MustCompile(`(?s)<a href="/torrent/(\d+)/[^"]*">(.*?)</a>`)
	rutMagnet  = regexp.MustCompile(`href="(magnet:\?[^"]+)"`)
	rutGreenRe = regexp.MustCompile(`(?s)class="green"[^>]*>.*?(\d+)</span>`)
	rutRedRe   = regexp.MustCompile(`(?s)class="red"[^>]*>.*?(\d+)</span>`)
	rutSizeRe  = regexp.MustCompile(`([\d.]+)&nbsp;(KB|MB|GB|TB)`)

	yearRe    = regexp.MustCompile(`\((\d{4})`)
	baseCutRe = regexp.MustCompile(`[(\[]`)
	seasonRe  = regexp.MustCompile(`(?i)сезон|серии|\[s\d+\]|\d+х\d+`)

	q2160Re = regexp.MustCompile(`(?i)2160p?|4k|uhd`)
	q1080Re = regexp.MustCompile(`(?i)1080p?i?`)
	q720Re  = regexp.MustCompile(`(?i)720p?i?`)

	dubRe = regexp.MustCompile(`(?i)дубляж|дубл[её]`)
	// многоголосая озвучка: NNM «[MVO]», rutor «| P» (после качества, перед студией)
	mvoRe      = regexp.MustCompile(`(?i)mvo|многоголос`)
	rutorMvoRe = regexp.MustCompile(`\|\s*P\b`)
	rutorDubRe = regexp.MustCompile(`\|\s*D\b`)
	// плохой звук: «звук с TS» — дорожка записана с экрана камрип-сеанса
	tsRe = regexp.MustCompile(`(?i)звук с\s?ts`)
	// камрип: плохая картинка — CAMRip, TS/TeleSync, TC, SCR, «зрительный зал»
	camRe   = regexp.MustCompile(`(?i)\b(cam|ts|tc|hdts|telesync|telecine|scr)\b|camrip|зрительный зал`)
	voiceRe = regexp.MustCompile(`(?i)(LostFilm|Кубик в Кубе|NewStudio|Jaskier|Red ?Head Sound|` +
		`HDrezka(?: Studio)?|Kerob|TVShows|MetalVoice|Amazing Dubbing|Синема УС)`)
)

// ---------- разбор названия

func stripTags(s string) string {
	return strings.TrimSpace(spaceRe.ReplaceAllString(tagRe.ReplaceAllString(s, " "), " "))
}

// parseTitle: «Название / Original (Год) WEBRip … (сезон 2)» → ru/orig/year/season
func parseTitle(t string) (ru, orig string, year int, season bool) {
	if m := yearRe.FindStringSubmatch(t); m != nil {
		year, _ = strconv.Atoi(m[1])
	}

	base := baseCutRe.Split(t, 2)[0]
	parts := strings.Split(base, "/")
	for i := range parts {
		parts[i] = strings.TrimSpace(strings.Trim(parts[i], " -"))
	}

	var pp []string
	for _, p := range parts {
		if p != "" {
			pp = append(pp, p)
		}
	}
	if len(pp) > 0 {
		ru = pp[0]
	}
	if len(pp) > 1 {
		orig = pp[len(pp)-1]
	}

	season = seasonRe.MatchString(t)
	return
}

func parseQuality(t string) string {
	switch {
	case q2160Re.MatchString(t):
		return "2160"
	case q1080Re.MatchString(t):
		return "1080"
	case q720Re.MatchString(t):
		return "720"
	}
	return "sd"
}

func atoiSafe(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil
}

// ---------- NNMClub

func nnmTop(cat string, pages int, order int) ([]Item, error) {
	var pagesHTML []string
	for p := 0; p < pages; p++ {
		body, err := fetch(fmt.Sprintf("%s/forum/tracker.php?o=%d&start=%d", nnmBase, order, pageSize*p))
		if err != nil {
			return nil, err
		}
		pagesHTML = append(pagesHTML, body)
	}

	videoIDs := map[int]bool{}
	if cat == "video" {
		videoIDs = nnmVideoSubtree(pagesHTML[0])
	}

	var items []Item
	for _, body := range pagesHTML {
		for _, it := range parseNNM(body) {
			if cat == "video" && !videoIDs[it.ForumID] {
				continue
			}
			items = append(items, it)
		}
	}
	return items, nil
}

func parseNNM(body string) []Item {
	var items []Item

	for _, seg := range strings.Split(body, "</tr>") {
		if !viewtopicRe.MatchString(seg) || !titleBRe.MatchString(seg) {
			continue
		}

		cells := tdRe.FindAllStringSubmatch(seg, -1)
		if len(cells) < 9 {
			continue
		}
		c := func(i int) string { return cells[i][1] }

		idm := viewtopicRe.FindStringSubmatch(seg)
		title := html.UnescapeString(stripTags(c(2)))

		ru, orig, year, season := parseTitle(title)

		it := Item{
			ID:      atoiDefault(idm[1]),
			Ru:      ru,
			Orig:    orig,
			Year:    year,
			Season:  season,
			Title:   title,
			Source:  "nnmclub",
			URL:     fmt.Sprintf("%s/forum/viewtopic.php?t=%s", nnmBase, idm[1]),
			Quality: parseQuality(title),
			Dub:     dubRe.MatchString(title),
			TsSound: tsRe.MatchString(title),
			Cam:     camRe.MatchString(title),
			Mvo:     mvoRe.MatchString(title),
		}
		it.Voice = voiceOf(title)

		if fm := forumRe.FindStringSubmatch(c(1)); fm != nil {
			it.ForumID = atoiDefault(fm[1])
			it.Category = strings.TrimSpace(html.UnescapeString(stripTags(fm[2])))
		}
		if dm := downloadRe.FindStringSubmatch(c(4)); dm != nil {
			it.Download = fmt.Sprintf("%s/forum/download.php?id=%s", nnmBase, dm[1])
		}

		sizeText := stripTags(c(5)) // "24209567632 22.5 GB"
		if f := numRe.FindString(sizeText); f != "" {
			it.Size, _ = strconv.ParseInt(f, 10, 64)
		}
		if parts := strings.Fields(sizeText); len(parts) >= 2 {
			it.SizeText = parts[len(parts)-2] + " " + parts[len(parts)-1]
		}

		it.Seeders = atoiDefault(stripTags(c(6)))
		it.Leechers = atoiDefault(stripTags(c(7)))
		it.Completed = atoiDefault(stripTags(c(8)))
		if am := addedRe.FindStringSubmatch(c(8)); am != nil {
			it.Added = int64(atoiDefault(am[1]))
		}

		items = append(items, it)
	}
	return items
}

// nnmVideoSubtree — поддерево видео-разделов по списку категорий со страницы трекера:
// топ-уровень без "|-", подфорумы с ним; берём всё под видео-корнями
func nnmVideoSubtree(body string) map[int]bool {
	ids := map[int]bool{}
	inVideo := false
	for _, m := range optionRe.FindAllStringSubmatch(body, -1) {
		id := atoiDefault(m[1])
		text := html.UnescapeString(m[2])
		if !strings.Contains(text, "|-") {
			inVideo = nnmVideoRoots[id]
		}
		if inVideo && !nnmNoRussian[id] {
			ids[id] = true
		}
	}
	return ids
}

// voiceOf — каноническое имя озвучки (под ним живёт фильтр voice=)
func voiceOf(title string) string {
	if m := voiceRe.FindString(title); m != "" {
		low := strings.ToLower(m)
		switch {
		case strings.Contains(low, "hdrezka"):
			return "HDrezka Studio"
		case strings.Contains(low, "red") && strings.Contains(low, "head"):
			return "Red Head Sound"
		}
		return m
	}
	if dubRe.MatchString(title) {
		return "Дубляж"
	}
	if mvoRe.MatchString(title) || rutorMvoRe.MatchString(title) {
		return "Многоголосый"
	}
	return ""
}

func atoiDefault(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// ---------- поиск раздачи по названию (для «Топ · TMDB»: есть ли раздача под наши фильтры)

func cp1251Escape(s string) string {
	enc, err := charmap.Windows1251.NewEncoder().String(s)
	if err != nil {
		return url.QueryEscape(s)
	}
	return url.QueryEscape(enc)
}

// fetchPost — форма urlencoded (поиск NNM)
func fetchPost(u, form string) (string, error) {
	req, err := http.NewRequest("POST", u, strings.NewReader(form))
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if utf8.Valid(raw) {
		return string(raw), nil
	}
	decoded, err := charmap.Windows1251.NewDecoder().Bytes(raw)
	if err != nil {
		decoded = raw
	}
	return string(decoded), nil
}

// findItems — раздачи обоих трекеров по точному названию
func findItems(query string) []Item {
	var items []Item

	// поиск NNM работает только POST-ом (GET с кириллицей молча не ищет)
	if body, err := fetchPost(nnmBase+"/forum/tracker.php", "nm="+cp1251Escape(query)); err == nil {
		video := nnmVideoSubtree(body)
		for _, it := range parseNNM(body) {
			if video[it.ForumID] {
				items = append(items, it)
			}
		}
	} else {
		log.Printf("find nnm: %v", err)
	}

	if body, err := fetch(rutorBase + "/search/0/0/000/0/" + url.PathEscape(query) + "/"); err == nil {
		items = append(items, parseRutor(body)...)
	} else {
		log.Printf("find rutor: %v", err)
	}

	return items
}

// findRelease — лучшая раздача фильма под наши фильтры (год с допуском:
// фильм ±1, сериал ±6 — у сезонных раздач год сезона, не первого)
func findRelease(query string, year int, typ string, minq, voices string, junk, ru bool) (Item, bool) {
	tol := 1
	if typ == "tv" {
		tol = 6
	}

	var kept []Item
	for _, it := range findItems(query) {
		if it.Year == 0 || year == 0 || abs(it.Year-year) > tol {
			continue
		}
		kept = append(kept, it)
	}

	if junk {
		kept = filterJunk(kept)
	}
	kept = filterVoice(kept, voices)
	if ru {
		kept = filterRussian(kept, "1")
	}
	kept = dedupeFilms(kept)
	kept = filterItems(kept, minq, "all")
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Seeders > kept[j].Seeders })

	if len(kept) == 0 {
		return Item{}, false
	}
	return kept[0], true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ---------- RUTOR

// видео-категории RUTOR: 1 зарубежные фильмы, 5 наши, 12 научно-популярные,
// 4 зарубежные сериалы, 16 наши, 7 мультипликация, 10 аниме
var rutorVideoCats = []int{1, 5, 12, 4, 16, 7, 10}

func rutorTop(cat string) ([]Item, error) {
	// топы по разделам: browse с сортировкой «по раздающим» (order=2)
	if cat == "video" {
		var items []Item
		var firstErr error
		for _, c := range rutorVideoCats {
			body, err := fetch(fmt.Sprintf("%s/browse/0/%d/0/0/2/", rutorBase, c))
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			items = append(items, parseRutor(body)...)
		}
		if len(items) == 0 && firstErr != nil {
			return nil, firstErr
		}
		return items, nil
	}

	body, err := fetch(rutorBase + "/top/")
	if err != nil {
		return nil, err
	}
	return parseRutor(body), nil
}

func parseRutor(body string) []Item {
	var items []Item

	for _, seg := range strings.Split(body, "</tr>") {
		if !strings.Contains(seg, `<tr class="gai"`) && !strings.Contains(seg, `<tr class="tum"`) {
			continue
		}

		cells := tdRe.FindAllStringSubmatch(seg, -1)
		if len(cells) < 5 {
			continue
		}
		c := func(i int) string { return cells[i][1] }

		tm := rutTorrent.FindStringSubmatch(c(1))
		if tm == nil {
			continue
		}

		title := html.UnescapeString(stripTags(tm[2]))
		ru, orig, year, season := parseTitle(title)

		it := Item{
			ID:      atoiDefault(tm[1]),
			Ru:      ru,
			Orig:    orig,
			Year:    year,
			Season:  season,
			Title:   title,
			Source:  "rutor",
			URL:     fmt.Sprintf("%s/torrent/%s", rutorBase, tm[1]),
			Quality: parseQuality(title),
			Dub:     dubRe.MatchString(title) || rutorDubRe.MatchString(title),
			TsSound: tsRe.MatchString(title),
			Cam:     camRe.MatchString(title),
			Mvo:     mvoRe.MatchString(title) || rutorMvoRe.MatchString(title),
			Voice:   voiceOf(title),
		}

		if mm := rutMagnet.FindStringSubmatch(c(1)); mm != nil {
			it.Magnet = mm[1]
		}

		if sm := rutSizeRe.FindStringSubmatch(c(3)); sm != nil {
			it.SizeText = sm[1] + " " + sm[2]
			it.Size = humanToBytes(sm[1], sm[2])
		}

		if gm := rutGreenRe.FindStringSubmatch(c(4)); gm != nil {
			it.Seeders = atoiDefault(gm[1])
		}
		if rm := rutRedRe.FindStringSubmatch(c(4)); rm != nil {
			it.Leechers = atoiDefault(rm[1])
		}

		items = append(items, it)
	}
	return items
}

func humanToBytes(num, unit string) int64 {
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	var mult float64
	switch unit {
	case "KB":
		mult = 1 << 10
	case "MB":
		mult = 1 << 20
	case "GB":
		mult = 1 << 30
	case "TB":
		mult = 1 << 40
	}
	return int64(f * mult)
}
