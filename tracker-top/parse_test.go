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
	return string(b)
}

var fails int

func check(t *testing.T, name string, cond bool, extra ...any) {
	t.Helper()
	if !cond {
		fails++
		t.Errorf("FAIL %s %v", name, extra)
	}
}

// ---------- NNMClub

func TestParseNNM(t *testing.T) {
	rows := parseNNM(fixture(t, "nnm_tracker_o10.html"))

	check(t, "rows >= 40", len(rows) >= 40, len(rows))
	check(t, "отсортировано по сидам", len(rows) > 1 && rows[0].Seeders >= rows[len(rows)-1].Seeders,
		rows[0].Seeders, rows[len(rows)-1].Seeders)

	top := rows[0]
	check(t, "url", strings.Contains(top.URL, "viewtopic.php?t="), top.URL)
	check(t, "download", strings.Contains(top.Download, "download.php?id="), top.Download)
	check(t, "category", top.Category != "", top.Category)
	check(t, "size_text", strings.HasSuffix(top.SizeText, "B"), top.SizeText)
	check(t, "source", top.Source == "nnmclub")
	check(t, "quality", top.Quality != "", top.Quality)

	video := nnmVideoSubtree(fixture(t, "nnm_tracker_o10.html"))
	check(t, "видео-поддерево", len(video) > 60, len(video))
	for _, id := range []int{224, 220, 768, 769, 576, 620, 624} {
		check(t, "корень в поддереве", video[id], id)
	}
	check(t, "софта нет", !video[503])
	check(t, "книг нет", !video[434])
}

// ---------- RUTOR

func TestParseRutor(t *testing.T) {
	rows := parseRutor(fixture(t, "rutor_top.html"))

	check(t, "rows > 100", len(rows) > 100, len(rows))

	var withSeeders, withMagnet int
	for _, r := range rows {
		if r.Seeders > 0 {
			withSeeders++
		}
		if strings.HasPrefix(r.Magnet, "magnet:") {
			withMagnet++
		}
	}
	check(t, "сиды распознаны", withSeeders > len(rows)*9/10, withSeeders)
	check(t, "магнеты распознаны", withMagnet > len(rows)*9/10, withMagnet)

	// строка «Укрытие / Бункер / Silo [S03] (2026) WEB-DL 1080p | P | Red Head Sound»
	var silo *Item
	for i := range rows {
		if rows[i].ID == 1097765 {
			silo = &rows[i]
		}
	}
	if silo == nil {
		t.Errorf("FAIL раздача 1097765 не найдена")
	} else {
		check(t, "ru", silo.Ru == "Укрытие", silo.Ru)
		check(t, "orig (последняя часть)", silo.Orig == "Silo", silo.Orig)
		check(t, "year", silo.Year == 2026, silo.Year)
		check(t, "season", silo.Season)
		check(t, "quality", silo.Quality == "1080", silo.Quality)
		check(t, "voice", silo.Voice == "Red Head Sound", silo.Voice)
		check(t, "seeders > 0", silo.Seeders > 0, silo.Seeders)
		check(t, "size_text", silo.SizeText == "41.63 GB", silo.SizeText)
	}
}

// ---------- названия / качество / озвучка

func TestParseTitle(t *testing.T) {
	cases := []struct {
		title    string
		ru, orig string
		year     int
		season   bool
	}{
		{"Джентльмены / The Gentlemen (2026) WEB-DL [H.264/1080p] (сезон 2, серии 1-8)",
			"Джентльмены", "The Gentlemen", 2026, true},
		{"Овчарка (2026) WEBRip [H.264/1080p] (обновляемая)",
			"Овчарка", "", 2026, false},
		{"Холоп 3 (2026) WEBRip [H.264/1080p]",
			"Холоп 3", "", 2026, false},
		{"Укрытие / Бункер / Silo [S03] (2026) WEB-DL 1080p | P | Red Head Sound",
			"Укрытие", "Silo", 2026, true},
		{"Человек-паук: Новый день / Spider-Man: Brand New Day (2026) WEB-DL 2160p | Дубляж",
			"Человек-паук: Новый день", "Spider-Man: Brand New Day", 2026, false},
	}
	for _, c := range cases {
		ru, orig, year, season := parseTitle(c.title)
		check(t, "parseTitle "+c.title[:25], ru == c.ru && orig == c.orig && year == c.year && season == c.season,
			ru, orig, year, season)
	}
}

func TestParseQuality(t *testing.T) {
	cases := map[string]string{
		"Что-то (2026) WEB-DL 2160p | Дубляж": "2160",
		"Что-то (2026) WEBRip [H.264/1080p]":  "1080",
		"Что-то (2026) WEB-DLRip-AVC":         "sd",
		"Что-то (2026) UHD BDRip":             "2160",
		"Что-то (2026) 720p":                  "720",
		"Что-то (2026) DVDRip":                "sd",
	}
	for title, want := range cases {
		check(t, "quality "+title, parseQuality(title) == want, parseQuality(title))
	}
}

func TestFilters(t *testing.T) {
	items := []Item{
		{Title: "A (2026) 1080p", Quality: "1080", Seeders: 100},
		{Title: "B (2026) 720p", Quality: "720", Seeders: 90},
		{Title: "C (2026) DVDRip", Quality: "sd", Seeders: 80, Voice: "Дубляж", Dub: true},
		{Title: "D (2026) 1080p Звук с TS", Quality: "1080", Seeders: 70, TsSound: true},
	}

	minq := filterItems(items, "1080", "all")
	check(t, "minq=1080", len(minq) == 2 && minq[0].Title[0] == 'A' && minq[1].Title[0] == 'D', len(minq))

	dub := filterItems(items, "", "dub")
	check(t, "audio=dub", len(dub) == 1 && dub[0].Title[0] == 'C')

	noTS := filterItems(items, "", "no_ts")
	check(t, "audio=no_ts", len(noTS) == 3)

	all := filterItems(items, "any", "all")
	check(t, "фильтры выключены", len(all) == 4)
}

func TestDedupeFilms(t *testing.T) {
	items := []Item{
		{Ru: "Холоп 3", Year: 2026, Quality: "720", Seeders: 300, Size: 2 << 30},
		{Ru: "Холоп 3", Year: 2026, Quality: "1080", Seeders: 1698, Size: 8 << 30},
		{Ru: "Холоп 3", Year: 2026, Quality: "2160", Seeders: 100, Size: 30 << 30, Dub: true},
		{Ru: "Мятеж", Orig: "Mutiny", Year: 2026, Quality: "1080", Seeders: 675},
	}

	out := dedupeFilms(items)
	check(t, "дубли схлопнуты", len(out) == 2, len(out))

	var holop *Item
	for i := range out {
		if out[i].Ru == "Холоп 3" {
			holop = &out[i]
		}
	}
	check(t, "сидов суммировано", holop != nil && holop.Seeders == 2098, holop.Seeders)
	check(t, "представитель — лучшая раздача", holop != nil && holop.Quality == "2160", holop.Quality)
	check(t, "размер лучшей раздачи", holop != nil && holop.Size == 30<<30, holop.Size)
	check(t, "дубляж не потерян", holop != nil && holop.Dub)
}
