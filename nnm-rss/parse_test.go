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

// ---------- страницы тем → info-hash (гостю магнит виден даже при скрытом «Скачать»)

func TestPostSegmentInfoHash(t *testing.T) {
	body := fixture(t, "nnm_thread.html") // тема с вложением в первом посте

	seg := postSegment(body, 6819070)
	check(t, "сегмент найден", seg != "")
	hash := findInfoHash(seg)
	check(t, "info-hash первого поста", hash != "", hash)

	// соседний пост без раздачи не должен отдавать чужой магнит
	seg2 := postSegment(body, 6820819)
	check(t, "пост без раздачи", findInfoHash(seg2) == "")
}

func TestGuestMagnetOnHiddenDownload(t *testing.T) {
	body := fixture(t, "nnm_topic.html") // «Скачать» гостю скрыт, магнит — виден
	m := titleTagRe.FindStringSubmatch(body)
	check(t, "title есть", m != nil)
	check(t, "тема", strings.Contains(m[1], "Касса невест"), m[1])
	hash := findInfoHash(body)
	check(t, "гость видит магнит", hash == "21F3A8C82E2D2375DD337D06C40D73C7A9D09B60", hash)
}

// ---------- магнит с персональным announce

func TestMagnetLink(t *testing.T) {
	m := magnetLink("21F3A8C82E2D2375DD337D06C40D73C7A9D09B60", "00680ffd1c4d0286486403595191d662")
	check(t, "хэш", strings.HasPrefix(m, "magnet:?xt=urn:btih:21F3A8"), m)
	check(t, "announce с пасскеем", strings.Contains(m, "bt02.nnm-club.cc%3A2710%2F00680ffd1c4d0286486403595191d662%2Fannounce"), m)
	check(t, "второй трекер", strings.Contains(m, "bt.searchtor.to%2F00680ffd"), m)
	check(t, "нет чужих трекеров", !strings.Contains(m, "retracker"), m)
}

// ---------- подписки: URL → вид + id

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

// ---------- gid ↔ (kind, id)

func TestGidParse(t *testing.T) {
	check(t, "topic", k2kind("topic1888923") == "topic" && k2id("topic1888923") == 1888923)
	check(t, "post", k2kind("post13108518") == "post" && k2id("post13108518") == 13108518)
}
