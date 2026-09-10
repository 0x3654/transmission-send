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

// ---------- страницы тем → Release (магнит, постер, описание)

func TestGuestReleaseOnHiddenDownload(t *testing.T) {
	body := fixture(t, "nnm_topic.html") // «Скачать» гостю скрыт, магнит — виден
	m := titleTagRe.FindStringSubmatch(body)
	check(t, "title есть", m != nil)
	check(t, "тема", strings.Contains(m[1], "Касса невест"), m[1])

	rel := parseRelease(body)
	check(t, "гость видит магнит", rel.Hash == "21F3A8C82E2D2375DD337D06C40D73C7A9D09B60", rel.Hash)
	check(t, "постер найден (не рейтинг КП)",
		rel.Poster != "" && !strings.Contains(rel.Poster, "kinopoisk.ru/rating"), rel.Poster)
	check(t, "описание найдено",
		strings.Contains(rel.Descr, "Недалекое будущее"), rel.Descr)

	// техданные раздачи
	tech := map[string]string{}
	for _, f := range rel.Tech {
		tech[f.Name] = f.Value
	}
	check(t, "жанр", strings.Contains(tech["Жанр"], "фантастика"), tech["Жанр"])
	check(t, "режиссер", strings.Contains(tech["Режиссер"], "Бальтцер"), tech["Режиссер"])
	check(t, "видео", strings.Contains(tech["Видео"], "AVC"), tech["Видео"])
	check(t, "аудио", strings.Contains(tech["Аудио"], "AC3"), tech["Аудио"])
	check(t, "описание в техполях на своём месте", strings.Contains(tech["Описание"], "Недалекое будущее"), tech["Описание"][:60])
	check(t, "tech string для фильтров", strings.Contains(rel.TechString(), "AVC"))
}

// ---------- топ трекера (популярное за N дней)

func TestParseTrackerTop(t *testing.T) {
	body := fixture(t, "nnm_tracker_o10.html")

	rows := parseTrackerTop(body)
	check(t, "строк >= 40", len(rows) >= 40, len(rows))
	if len(rows) == 0 {
		return
	}
	check(t, "id и название", rows[0].TopicID > 0 && rows[0].Title != "", rows[0])
	check(t, "время добавления", rows[0].Added > 1_600_000_000, rows[0].Added)

	video := nnmVideoSubtree(body)
	check(t, "видео-поддерево непустое", len(video) > 10, len(video))

	cats := parseTrackerCategories(body)
	check(t, "верхние разделы", len(cats) >= 10, len(cats))
	for _, c := range cats {
		check(t, "имя без |-", !strings.Contains(c.Name, "|-"), c.Name)
		// мусорные опции чужих селектов (сортировки, медальки) не проходят
		check(t, "не мусор", !strings.Contains(strings.ToLower(c.Name), "seeders") &&
			!strings.Contains(strings.ToLower(c.Name), "золот"), c.Name)
	}

	// опции-фильтры: тип раздачи (sds) — отдельным списком
	types := trackerOptions(body, "sds")
	check(t, "5 типов раздач", len(types) == 5, len(types))
	if len(types) == 5 {
		check(t, "обычные..платиновые", strings.Contains(types[0].Name, "обычн") &&
			strings.Contains(types[1].Name, "золот") && strings.Contains(types[4].Name, "платин"),
			types[0].Name, types[1].Name, types[4].Name)
	}
	// сортировки и окна времени не попадают в sds
	check(t, "sds без сортировок", len(trackerOptions(body, "o")) >= 10)
}

// ---------- магнит: btih без трекеров (DHT)

func TestMagnetLink(t *testing.T) {
	m := magnetLink("21F3A8C82E2D2375DD337D06C40D73C7A9D09B60")
	check(t, "ровно btih", m == "magnet:?xt=urn:btih:21F3A8C82E2D2375DD337D06C40D73C7A9D09B60", m)
	check(t, "без трекеров", !strings.Contains(m, "tr=") && !strings.Contains(m, "announce"), m)
}

// ---------- подписки: URL → вид + id

func TestParseNNMURL(t *testing.T) {
	cases := []struct {
		url  string
		kind string
		id   int
	}{
		{"https://nnmclub.to/forum/viewtopic.php?t=830137", "topic", 830137},
		{"https://nnmclub.to/forum/viewtopic.php?p=13108518#13108518", "post", 13108518},
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

func TestFilmKeyAndQuality(t *testing.T) {
	// один фильм под разными русскими названиями и качествами — один ключ
	a := filmKey("Мэйдэй / Mayday (2026) WEB-DL [H.264/1080p]")
	b := filmKey("Мэйдэй / Сигнал бедствия / Mayday (2026) WEB-DL [H.264/1080p]")
	c := filmKey("Моана / Moana (2026) WEB-DL [H.265/2160p] [4K, HDR10, DV 8, 10-bit]")
	d := filmKey("Моана / Moana (2026) WEB-DL [H.264/720p]")
	if a != b {
		t.Errorf("filmKey: вариант перевода названия должен склеиваться: %q != %q", a, b)
	}
	if c != d {
		t.Errorf("filmKey: разные качества одного фильма должны склеиваться: %q != %q", c, d)
	}
	if a == c {
		t.Errorf("filmKey: разные фильмы не должны склеиваться")
	}

	ranks := map[string]int{
		"X (2026) WEB-DL [H.265/2160p] [4K]": 3,
		"X (2026) WEBRip [H.264/1080p]":      2,
		"X (2026) WEB-DL [H.264/720p]":       1,
		"X (2026) WEB-DLRip":                 0,
	}
	for title, want := range ranks {
		if got := filmQualityRank(title); got != want {
			t.Errorf("filmQualityRank(%q) = %d, хочу %d", title, got, want)
		}
	}
}

func TestDedupeTopRows(t *testing.T) {
	rows := []topRow{
		{TopicID: 1, Title: "Моана / Moana (2026) WEB-DL [H.264/720p]", Added: 200},
		{TopicID: 2, Title: "Моана / Moana (2026) WEB-DL [H.264/1080p]", Added: 100},
		{TopicID: 3, Title: "Моана / Moana (2026) WEB-DL [H.265/2160p] [4K, SDR]", Added: 150},
		{TopicID: 4, Title: "Мэйдэй / Mayday (2026) WEB-DL [H.264/1080p]", Added: 300},
		{TopicID: 5, Title: "Мэйдэй / Сигнал бедствия / Mayday (2026) WEB-DL [H.265/2160p]", Added: 280},
		{TopicID: 6, Title: "Другой фильм (2026) WEBRip [H.264/1080p]", Added: 400},
	}
	out := dedupeTopRows(rows)
	if len(out) != 3 {
		t.Fatalf("дедуп: %d позиций, хочу 3 (Моана, Мэйдэй, Другой)", len(out))
	}
	byTitle := map[int]bool{}
	for _, r := range out {
		byTitle[r.TopicID] = true
	}
	// Моана: лучшее качество 2160 → тема 3; Мэйдэй: 2160 → тема 5
	if !byTitle[3] || !byTitle[5] || !byTitle[6] {
		t.Errorf("дедуп выбрал не лучшие раздачи: %+v", byTitle)
	}
	if byTitle[1] || byTitle[2] || byTitle[4] {
		t.Errorf("дедуп оставил лишние темы: %+v", byTitle)
	}
}
