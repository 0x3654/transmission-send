package main

// Состояние сервиса: профили, ключённые по passkey трекера. Личность юзера —
// сам passkey (как TrackerId у lostfilmfeed): 128 бит, отдельные логины не нужны.
// Один JSON-файл на томе /data, атомарная запись через tmp+rename.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Sub struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`    // forum | topic
	NNMID   int    `json:"nnm_id"`  // id раздела или темы
	Title   string `json:"title"`   // человекочитаемое название
	Poster  string `json:"poster"`  // для списка подписок и описаний в ленте
	Filter  string `json:"filter"`  // regexp включения (пустой = всё)
	Exclude string `json:"exclude"` // regexp исключения
	Auto    bool   `json:"auto"`    // попадает в авто-ленту (выбор юзера, любой тип)
	Enabled bool   `json:"enabled"`
	RootID  int    `json:"root_id,omitempty"` // корневой раздел (для галок «Ленты разделов»)
	Root    string `json:"root,omitempty"`    // имя корневого раздела
}

// HistItem — строка истории «Моя подписка»: что прошло через ленту
type HistItem struct {
	GUID  string    `json:"guid"`
	Title string    `json:"title"`
	URL   string    `json:"url"` // магнит с announce юзера
	Date  time.Time `json:"date"`
}

// Profile — настройки и истории страницы; истории две — как ленты:
// авто (что скачал клиент) и общая (всё, что прошло, включая разделы)
// FeedFilter — фильтр ленты (у каждой свой, блок настроек одинаковый)
type FeedFilter struct {
	Include string `json:"include"` // regexp включения (пустой = всё)
	Exclude string `json:"exclude"` // regexp исключения
}

type Profile struct {
	// доступ: логин + пароль (bcrypt). Passkey — секретный токен лент
	// (/rss/<токен>, историческое имя поля), WebToken — токен сессии в куке
	// (отдельный, от токена лент не производен). У старых профилей (до логинов)
	// Login пуст — страница открывается прежней кукой, задать пароль можно
	// кнопкой «Задать логин и пароль».
	Login       string                 `json:"login,omitempty"`
	PassHash    string                 `json:"pass_hash,omitempty"` // bcrypt
	WebToken    string                 `json:"web_token,omitempty"`
	Passkey     string                 `json:"passkey"` // токен лент: /rss/<passkey>
	Subs        []*Sub                 `json:"subs"`
	TopRoots    []int                  `json:"top_roots"`           // выбранные верхние разделы для топ-лент (пусто = видео по умолчанию)
	TopSds      []int                  `json:"top_types,omitempty"` // типы раздач топа, мультивыбор (пусто = все): 0 обычные, 1 золотые, 2 серебряные, 3 бронзовые, 4 платиновые
	TopSeen     map[string]int         `json:"top_seen,omitempty"`  // топ-лента: фильм → ранг качества выпущенного (повторы и downgrade не выпускаем)
	Filters     map[string]*FeedFilter `json:"filters,omitempty"`   // ключи: auto, all, top
	HistoryAuto []HistItem             `json:"history_auto"`
	HistoryAll  []HistItem             `json:"history_all"`
	HistoryTop  []HistItem             `json:"history_top"`
}

// profileByLogin / profileByWebToken — поиск по полям доступа
func profileByLogin(login string) *Profile {
	for _, p := range state.Profiles {
		if p.Login == login {
			return p
		}
	}
	return nil
}

func profileByWebToken(tok string) *Profile {
	for _, p := range state.Profiles {
		if p.WebToken != "" && p.WebToken == tok {
			return p
		}
	}
	return nil
}

type stateFile struct {
	Profiles []*Profile `json:"profiles"`
}

var (
	stateMu sync.Mutex
	state   stateFile
)

func statePath() string {
	return filepath.Join(env("DATA_DIR", "data"), "state.json")
}

func loadState() {
	stateMu.Lock()
	defer stateMu.Unlock()

	state = stateFile{}
	b, err := os.ReadFile(statePath())
	if err != nil {
		if !os.IsNotExist(err) {
			panic(err)
		}
		return
	}
	if err := json.Unmarshal(b, &state); err != nil {
		panic(err)
	}
}

// saveState — под мьютексом вызывающего
func saveState() {
	if err := os.MkdirAll(filepath.Dir(statePath()), 0o700); err != nil {
		panic(err)
	}
	b, err := json.MarshalIndent(&state, "", "  ")
	if err != nil {
		panic(err)
	}
	tmp := statePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		panic(err)
	}
	if err := os.Rename(tmp, statePath()); err != nil {
		panic(err)
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// profileByPasskey — профиль по ключу; новый юзер создаётся при первом входе
func profileByPasskey(pk string) *Profile {
	for _, p := range state.Profiles {
		if p.Passkey == pk {
			return p
		}
	}
	p := &Profile{Passkey: pk}
	state.Profiles = append(state.Profiles, p)
	return p
}

func (p *Profile) sub(id string) *Sub {
	for _, s := range p.Subs {
		if s.ID == id {
			return s
		}
	}
	return nil
}
