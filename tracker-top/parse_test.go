package main

import (
	"os"
	"strings"
	"testing"
)

var _ = camRe

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
		// один фильм, разные написания orig (жivoй кейс rutor)
		{Ru: "Бэтмен: Падение рыцаря. Часть первая", Orig: "Batman: Knightfall, Part 1", Year: 2026, Quality: "2160", Seeders: 53},
		{Ru: "Бэтмен: Падение рыцаря. Часть первая", Orig: "Batman: Knightfall - Part 1: Knight", Year: 2026, Quality: "1080", Seeders: 19},
		// ru без перевода + с переводом — один фильм
		{Ru: "Вышка", Year: 2026, Quality: "1080", Seeders: 40},
		{Ru: "Вышка", Orig: "The Fall", Year: 2026, Quality: "2160", Seeders: 10},
	}

	out := dedupeFilms(items)
	check(t, "дубли схлопнуты", len(out) == 4, len(out)) // Холоп 3, Мятеж, Бэтмен, Вышка

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

	for _, it := range out {
		if it.Ru == "Бэтмен: Падение рыцаря. Часть первая" {
			check(t, "бэтмен один, лучший качеством", it.Quality == "2160" && it.Seeders == 72, it.Quality, it.Seeders)
		}
		if it.Ru == "Вышка" {
			check(t, "вышка одна (ru+год, orig игнорируется)", it.Quality == "2160" && it.Seeders == 50, it.Quality, it.Seeders)
		}
	}
}

func TestCamAndJunk(t *testing.T) {
	camCases := map[string]bool{
		"Фильм (2026) TS":                   true,
		"Фильм (2026) CAMRip":               true,
		"Фильм (2026) TeleSync":             true,
		"Фильм (2026) WEB-DL 1080p":         false,
		"Фильм (2026) WEBRip [H.264/1080p]": false,
		"Фильм (2026) BDRip":                false,
	}
	for title, want := range camCases {
		check(t, "cam "+title, camRe.MatchString(title) == want, camRe.MatchString(title))
	}

	items := []Item{
		{Ru: "A", Year: 2026, Cam: true, Seeders: 500},
		{Ru: "A", Year: 2026, Seeders: 10, Quality: "1080"},
		{Ru: "B", Year: 2026, TsSound: true, Seeders: 900}, // только звук с TS
		{Ru: "C", Year: 2026, Seeders: 100},
	}
	out := filterJunk(items)
	// A осталась (есть нормальная раздача), B исчезла целиком, C на месте
	check(t, "junk: A осталась", len(out) == 2 && (out[0].Ru == "A" || out[1].Ru == "A") &&
		(out[0].Ru == "C" || out[1].Ru == "C"), len(out))

	// представитель у A — не камрип, несмотря на 500 сидов кам-раздачи
	for _, it := range out {
		if it.Ru == "A" {
			check(t, "junk: представитель не камрип", !it.Cam)
			check(t, "junk: сиды камрип-раздачи не считаются", it.Seeders == 10, it.Seeders)
		}
	}
}

func TestSortItems(t *testing.T) {
	items := []Item{
		{Ru: "A", Seeders: 100, Completed: 5},
		{Ru: "B", Seeders: 50, Completed: 900},
		{Ru: "C", Seeders: 70, Completed: 100},
	}
	sortItems(items, "seeds")
	check(t, "по сидам", items[0].Ru == "A" && items[1].Ru == "C" && items[2].Ru == "B")
	sortItems(items, "top")
	check(t, "по завершённости", items[0].Ru == "B" && items[1].Ru == "C" && items[2].Ru == "A")
}

func TestMvoAndBlocked(t *testing.T) {
	// многоголосость: NNM «[MVO]», rutor «| P»; дубляж rutor «| D»
	check(t, "mvo [MVO]", mvoRe.MatchString("Всего одна ночь (2026) WEB-DL [H.264/1080p] [MVO]"))
	check(t, "mvo rutor | P", rutorMvoRe.MatchString("Укрытие / Silo (2026) WEB-DL 1080p | P | Red Head Sound"))
	check(t, "не mvo", !rutorMvoRe.MatchString("Фильм (2026) WEB-DL 1080p | D"))
	check(t, "dub rutor | D", rutorDubRe.MatchString("Фильм (2026) WEB-DL 1080p | D"))
	check(t, "не dub от | P", !rutorDubRe.MatchString("Фильм (2026) WEB-DL 1080p | P"))

	items := []Item{
		{Ru: "A", Year: 2026, Mvo: true, Voice: "", Seeders: 100},
		{Ru: "B", Year: 2026, Mvo: false, Voice: "LostFilm", Seeders: 90},
		{Ru: "C", Year: 2026, Mvo: false, Dub: true, Seeders: 80},
		{Ru: "D", Year: 2026, Seeders: 70},
	}
	out := filterVoice(items, "Многоголосый")
	check(t, "voice=Многоголосый", len(out) == 1 && out[0].Ru == "A")

	// стоп-лист
	blocked := []Item{
		{Ru: "Фильм", Title: "Фильм (2026) WEB-DL 1080p | Ultradox"},
		{Ru: "Фильм 2", Title: "Фильм 2 (2026) WEB-DL 1080p"},
	}
	out2 := filterBlocked(blocked)
	check(t, "ultradox всегда вырезан", len(out2) == 1 && out2[0].Ru == "Фильм 2")
}

func TestFilterVoice(t *testing.T) {
	items := []Item{
		{Ru: "A", Year: 2026, Voice: "LostFilm", Quality: "1080", Seeders: 100},
		{Ru: "A", Year: 2026, Voice: "", Dub: true, Quality: "720", Seeders: 50},
		{Ru: "B", Year: 2026, Voice: "Red Head Sound", Seeders: 90},
		{Ru: "C", Year: 2026, Voice: "", Dub: false, Seeders: 80},
	}

	// одна озвучка
	out := filterVoice(items, "LostFilm")
	check(t, "voice=LostFilm", len(out) == 1 && out[0].Ru == "A")

	// две сразу (через запятую)
	out = filterVoice(items, "LostFilm,Red Head Sound")
	check(t, "две озвучки", len(out) == 2)

	// «Дубляж» ловит раздачи с дубляжом
	out = filterVoice(items, "Дубляж")
	check(t, "voice=Дубляж", len(out) == 1 && out[0].Quality == "720")

	// связка: дубляж + студия — обе раздачи A остаются, B/C нет
	out = filterVoice(items, "Дубляж,LostFilm")
	check(t, "Дубляж+LostFilm", len(out) == 2 && out[0].Ru == "A" && out[1].Ru == "A")

	// фильтр до дедупа: у A остаются LostFilm и дубляж → дедуп выберет лучшую
	ded := dedupeFilms(filterVoice(items, "Дубляж,LostFilm"))
	check(t, "после дедупа одна A", len(ded) == 1 && ded[0].Seeders == 150 && ded[0].Quality == "1080")

	// пустой фильтр — всё на месте
	check(t, "voice пустой", len(filterVoice(items, "")) == 4)
}

func TestFilterRussian(t *testing.T) {
	items := []Item{
		{Ru: "Холоп 3", Orig: "", Year: 2026},
		{Ru: "Джентльмены", Orig: "The Gentlemen", Year: 2026},
		{Ru: "Minions & Monsters", Orig: "", Year: 2026},   // совсем без кириллицы
		{Ru: "Some Movie", Orig: "Some Movie", Year: 2026}, // тоже
		{Ru: "Аниме", Orig: "Naruto", Year: 2026},
	}
	out := filterRussian(items, "1")
	check(t, "ru=1: без кириллицы скрыты", len(out) == 3, len(out))
	for _, it := range out {
		check(t, "ru=1: "+it.Ru, hasCyrillic(it.Ru) || hasCyrillic(it.Orig), it.Ru)
	}
	check(t, "ru=0: всё на месте", len(filterRussian(items, "0")) == 5)
}

func TestFindRelease(t *testing.T) {
	// findItems на живых снимках поиска: NNM (cp1251) + rutor (utf-8)
	nnm := parseNNM(fixture(t, "nnm_search.html"))
	rut := parseRutor(fixture(t, "rutor_search.html"))
	check(t, "nnm search rows", len(nnm) > 0, len(nnm))
	check(t, "rutor search rows", len(rut) > 25, len(rut))
	hits := 0
	for _, it := range append(append([]Item{}, nnm...), rut...) {
		if it.Ru == "Холоп 3" || it.Ru == "Холоп" {
			hits++
		}
	}
	check(t, "холоп найден в обеих выдачах", hits >= 2, hits)
}
