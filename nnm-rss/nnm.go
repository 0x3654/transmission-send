package main

// Клиент NNMClub. Авторизации нет нигде: родные rss.php читает гость, info-hash
// раздачи виден в магнит-ссылке на странице темы. Лента отдаёт магниты с
// персональным announce юзера (bt02.nnm-club.cc:2710/<passkey>/announce) —
// клиент аннонсит его ключом, статистика на трекере считается ему.

import (
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

var nnmBase = env("NNM_BASE", "https://nnmclub.to")

const nnmUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36"

var nnmClient = &http.Client{Timeout: 20 * time.Second}

// fetchNNM — GET с UA и (не)равным cookie; cp1251 → utf-8 (валидный utf-8 не трогаем)
func fetchNNM(u, cookie string) (string, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", nnmUA)
	req.Header.Set("Accept-Encoding", "identity")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := nnmClient.Do(req)
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
	return decodeBody(raw), nil
}

func decodeBody(raw []byte) string {
	if utf8.Valid(raw) {
		return string(raw)
	}
	decoded, err := charmap.Windows1251.NewDecoder().Bytes(raw)
	if err != nil {
		return string(raw)
	}
	return string(decoded)
}

// ---------- разбор родного rss.php

type rssItem struct {
	Title string
	Kind  string // topic | post
	ID    int    // id темы или поста
	Date  time.Time
}

var (
	rssItemRe  = regexp.MustCompile(`(?s)<item[^>]*>(.*?)</item>`)
	rssTitleRe = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	rssLinkRe  = regexp.MustCompile(`<link>\s*([^<]+?)\s*</link>`)
	rssDateRe  = regexp.MustCompile(`<pubDate>([^<]+)</pubDate>`)
	rssChanRe  = regexp.MustCompile(`(?s)<channel>.*?<title>(.*?)</title>`)

	linkTopicRe = regexp.MustCompile(`viewtopic\.php\?t=(\d+)`)
	linkPostRe  = regexp.MustCompile(`viewtopic\.php\?p=(\d+)`)
)

// parseRSSItems — items родной ленты; ссылки бывают на темы (t=, новые раздачи
// раздела) и на посты (p=, новые сообщения в теме)
func parseRSSItems(body string) []rssItem {
	var out []rssItem
	for _, m := range rssItemRe.FindAllStringSubmatch(body, -1) {
		seg := m[1]

		tm := rssTitleRe.FindStringSubmatch(seg)
		if tm == nil {
			continue
		}
		it := rssItem{Title: html.UnescapeString(tm[1])}

		lm := rssLinkRe.FindStringSubmatch(seg)
		if lm != nil {
			if pm := linkPostRe.FindStringSubmatch(lm[1]); pm != nil {
				it.Kind, it.ID = "post", atoiDefault(pm[1])
			} else if tm2 := linkTopicRe.FindStringSubmatch(lm[1]); tm2 != nil {
				it.Kind, it.ID = "topic", atoiDefault(tm2[1])
			}
		}
		if it.ID == 0 {
			continue
		}

		if dm := rssDateRe.FindStringSubmatch(seg); dm != nil {
			it.Date, _ = time.Parse(time.RFC1123, strings.TrimSpace(dm[1]))
		}
		if it.Date.IsZero() {
			it.Date = time.Now()
		}
		out = append(out, it)
	}
	return out
}

// parseRSSChannel — название раздела из заголовка канала
func parseRSSChannel(body string) string {
	if m := rssChanRe.FindStringSubmatch(body); m != nil {
		return html.UnescapeString(strings.TrimSpace(m[1]))
	}
	return ""
}

// cleanTitle: «Раздел :: RE: Название» → «Название»
func cleanTitle(s string) string {
	if i := strings.LastIndex(s, "::"); i >= 0 {
		s = s[i+2:]
	}
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "RE:") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "RE:"))
	}
	return s
}

// ---------- страницы тем/постов → торрент

var (
	magnetRe   = regexp.MustCompile(`magnet:\?xt=urn:btih:([A-Fa-f0-9]{40})`)
	anchorRe   = regexp.MustCompile(`<a name="\d+">`)
	titleTagRe = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
)

// postSegment — кусок страницы от якоря поста до якоря следующего поста:
// «Скачать» конкретной раздачи в темах-сералах живёт в посте релиза, не в шапке
func postSegment(body string, postID int) string {
	start := strings.Index(body, fmt.Sprintf(`<a name="%d">`, postID))
	if start < 0 {
		return ""
	}
	rest := body[start:]
	if next := anchorRe.FindStringIndex(rest[20:]); next != nil {
		return rest[:next[0]+20]
	}
	return rest
}

// findInfoHash — info-hash из магнит-ссылки в сегменте страницы (или во всей)
func findInfoHash(seg string) string {
	if m := magnetRe.FindStringSubmatch(seg); m != nil {
		return m[1]
	}
	return ""
}

// resolveInfoHash — info-hash раздачи по теме или посту (гостю магнит виден
// на странице); кэшируется — ключ не зависит от юзера
func resolveInfoHash(kind string, id int) (hash string, err error) {
	cacheKey := fmt.Sprintf("%s:%d", kind, id)
	cacheMu.Lock()
	if e, ok := resolveCache[cacheKey]; ok && time.Since(e.ts) < time.Duration(ttl)*time.Second {
		cacheMu.Unlock()
		return e.hash, nil
	}
	cacheMu.Unlock()

	var body, seg string
	if kind == "topic" {
		body, err = fetchNNM(fmt.Sprintf("%s/forum/viewtopic.php?t=%d", nnmBase, id), "")
		seg = body
	} else {
		body, err = fetchNNM(fmt.Sprintf("%s/forum/viewtopic.php?p=%d", nnmBase, id), "")
		seg = postSegment(body, id)
		if seg == "" {
			seg = body
		}
	}
	if err != nil {
		return "", err
	}

	hash = findInfoHash(seg) // пусто = пост без раздачи (обсуждение), кэшируем пусто

	cacheMu.Lock()
	resolveCache[cacheKey] = resolveEntry{ts: time.Now(), hash: hash}
	cacheMu.Unlock()
	return hash, nil
}

// magnetLink — магнит с персональными announce юзера: клиент будет аннонсить
// его ключом, статистика зачтётся ему (механизм тот же, что у TrackerId lostfilm)
func magnetLink(hash, passkey string) string {
	trackers := []string{
		"http://bt02.nnm-club.cc:2710/" + passkey + "/announce",
		"http://bt.searchtor.to/" + passkey + "/announce",
		"http://ipv6.bt.searchtor.to/" + passkey + "/announce",
		"http://bt02.ipv6.nnm-club.cc:2710/" + passkey + "/announce",
	}
	s := "magnet:?xt=urn:btih:" + hash
	for _, t := range trackers {
		s += "&tr=" + url.QueryEscape(t)
	}
	return s
}

// topicTitle — «Название :: NNM-Club» из <title> страницы темы
func topicTitle(id int) (string, error) {
	body, err := fetchNNM(fmt.Sprintf("%s/forum/viewtopic.php?t=%d", nnmBase, id), "")
	if err != nil {
		return "", err
	}
	m := titleTagRe.FindStringSubmatch(body)
	if m == nil {
		return "", errors.New("нет заголовка темы")
	}
	t := html.UnescapeString(m[1])
	if i := strings.LastIndex(t, "::"); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t), nil
}

// ---------- подписки: URL → вид + id

var (
	urlTopicRe = regexp.MustCompile(`viewtopic\.php\?[^#]*\bt=(\d+)`)
	urlPostRe  = regexp.MustCompile(`viewtopic\.php\?[^#]*\bp=(\d+)`)
	urlForumRe = regexp.MustCompile(`(?:tracker|viewforum)\.php\?[^#]*\bf=(\d+)`)
)

// parseNNMURL — из вставленной человеком ссылки (тема, пост или раздел трекера)
func parseNNMURL(u string) (kind string, id int) {
	if m := urlPostRe.FindStringSubmatch(u); m != nil {
		return "topic", atoiDefault(m[1]) // ссылка на пост внутри темы → тема
	}
	if m := urlTopicRe.FindStringSubmatch(u); m != nil {
		return "topic", atoiDefault(m[1])
	}
	if m := urlForumRe.FindStringSubmatch(u); m != nil {
		return "forum", atoiDefault(m[1])
	}
	return "", 0
}

// subRSSURL — адрес родной ленты подписки
func subRSSURL(s *Sub) string {
	if s.Kind == "forum" {
		return fmt.Sprintf("%s/forum/rss.php?f=%d&t=1&c=50", nnmBase, s.NNMID) // новые раздачи раздела
	}
	return fmt.Sprintf("%s/forum/rss.php?topic=%d&c=50", nnmBase, s.NNMID) // новые посты темы
}


func atoiDefault(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
