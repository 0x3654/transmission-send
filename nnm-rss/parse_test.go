package main

import (
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("tests/" + name)
	if err != nil {
		t.Fatal(err)
	}
	// фикстуры — сырые cp1251-страницы, гоняем через тот же декодер, что и вживую
	return decodeBody(b)
}

var fails int

func check(t *testing.T, name string, cond bool, extra ...any) {
	t.Helper()
	if !cond {
		fails++
		t.Errorf("FAIL %s %v", name, extra)
	}
}

// ---------- родной rss.php

func TestParseRSSForum(t *testing.T) {
	body := fixture(t, "nnm_rss_f270.xml")

	ch := parseRSSChannel(body)
	check(t, "канал — название раздела", ch != "" && ch != "NNM-Club", ch)

	items := parseRSSItems(body)
	check(t, "items >= 2", len(items) >= 2, len(items)) // в разделе на момент снимка было 2 новых раздачи

	var sawTopic bool
	for _, it := range items {
		check(t, "id", it.ID > 0, it)
		check(t, "kind", it.Kind == "topic" || it.Kind == "post", it.Kind)
		check(t, "дата", !it.Date.IsZero(), it.Title)
		if it.Kind == "topic" {
			sawTopic = true
		}
	}
	check(t, "есть темы-раздачи", sawTopic)

	if len(items) > 0 {
		title := cleanTitle(items[0].Title)
		check(t, "cleanTitle без «Раздел ::»", !strings.Contains(title, "::"), title)
	}
}

func TestCleanTitle(t *testing.T) {
	check(t, "раздел", cleanTitle("Отечественные Новинки (SD, DVD) :: Касса невест (2025) WEBRip") ==
		"Касса невест (2025) WEBRip")
	check(t, "RE", cleanTitle("Раздел :: RE: Сериал (2026) S01") == "Сериал (2026) S01")
}

// ---------- страницы тем

func TestPostSegmentTorrent(t *testing.T) {
	body := fixture(t, "nnm_thread.html") // тема с вложением в первом посте

	seg := postSegment(body, 6819070)
	check(t, "сегмент найден", seg != "")
	dlID, magnet := findTorrent(seg)
	check(t, "download id", dlID == 715427, dlID)
	check(t, "магнит в посте", strings.HasPrefix(magnet, "magnet:?xt=urn:btih:"), magnet)

	// соседний пост без вложения не должен отдавать чужую раздачу
	seg2 := postSegment(body, 6820819)
	dl2, _ := findTorrent(seg2)
	check(t, "пост без раздачи", dl2 == 0, dl2)
}

func TestTopicTitle(t *testing.T) {
	// гостю «Скачать» скрыт, но <title> виден — резолв без cookie должен пасть мягко
	body := fixture(t, "nnm_topic.html")
	m := titleTagRe.FindStringSubmatch(body)
	check(t, "title есть", m != nil)
	check(t, "тема", strings.Contains(m[1], "Касса невест"), m[1])
	dlID, _ := findTorrent(body)
	check(t, "гостю раздача скрыта", dlID == 0, dlID)
}

// ---------- мелочи

func TestParseNNMURL(t *testing.T) {
	cases := []struct {
		url  string
		kind string
		id   int
	}{
		{"https://nnmclub.to/forum/viewtopic.php?t=830137", "topic", 830137},
		{"https://nnmclub.to/forum/viewtopic.php?p=13108518#13108518", "topic", 13108518},
		{"https://nnmclub.to/forum/tracker.php?f=270", "forum", 270},
		{"https://nnmclub.to/forum/viewforum.php?f=954", "forum", 954},
		{"https://example.com/nothing", "", 0},
	}
	for _, c := range cases {
		kind, id := parseNNMURL(c.url)
		check(t, c.url, kind == c.kind && id == c.id, kind, id)
	}
}

func TestSlug(t *testing.T) {
	s := slug("Касса невест (2025) WEBRip [H.264]/и:прочее*?")
	check(t, "без слэшей", !strings.ContainsAny(s, "/:*?"), s)
	check(t, "суффикс", strings.HasSuffix(s, ".torrent"), s)
	check(t, "пусто → torrent", slug("!!!") == "torrent.torrent" || strings.HasPrefix(slug("!!!"), "torrent"), slug("!!!"))
}

func TestNormalizeCookie(t *testing.T) {
	check(t, "голое значение", normalizeCookie("abc123") == "bb_data=abc123")
	check(t, "готовое", normalizeCookie("bb_data=abc123") == "bb_data=abc123")
	check(t, "с префиксом", normalizeCookie("Cookie: bb_data=abc123") == "bb_data=abc123")
	check(t, "полный header", normalizeCookie("bb_data=abc; x=y") == "bb_data=abc; x=y")
	check(t, "пусто", normalizeCookie("  ") == "")
}
