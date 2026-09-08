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
	Filter  string `json:"filter"`  // regexp включения (пустой = всё)
	Exclude string `json:"exclude"` // regexp исключения
	Enabled bool   `json:"enabled"`
}

// HistItem — строка истории «Моя подписка»: что прошло через ленту
type HistItem struct {
	GUID  string    `json:"guid"`
	Title string    `json:"title"`
	URL   string    `json:"url"` // магнит с announce юзера
	Date  time.Time `json:"date"`
}

// Profile — настройки и история владельца passkey
type Profile struct {
	Passkey string     `json:"passkey"`
	Subs    []*Sub     `json:"subs"`
	History []HistItem `json:"history"`
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
