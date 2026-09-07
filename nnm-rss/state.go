package main

// Состояние сервиса: юзеры, сессии, подписки. Один JSON-файл на томе /data,
// атомарная запись через tmp+rename — без внешней БД, scratch-образу хватает.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
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

type User struct {
	ID        string `json:"id"`
	Login     string `json:"login"`
	PassHash  string `json:"pass_hash"`
	FeedToken string `json:"feed_token"` // секретная часть URL личной ленты
	NNMCookie string `json:"nnm_cookie"` // bb_data=… — сессия на трекере
	NNMUser   string `json:"nnm_user"`   // username на трекере
	NNMUID    int    `json:"nnm_uid"`
	Subs      []*Sub `json:"subs"`
}

type sessionRec struct {
	UserID string    `json:"user_id"`
	Expiry time.Time `json:"expiry"`
}

type stateFile struct {
	Users    []*User               `json:"users"`
	Sessions map[string]sessionRec `json:"sessions"`
}

var (
	stateMu sync.Mutex
	state   stateFile

	loginRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`)
)

func statePath() string {
	return filepath.Join(env("DATA_DIR", "data"), "state.json")
}

func loadState() {
	stateMu.Lock()
	defer stateMu.Unlock()

	state = stateFile{Sessions: map[string]sessionRec{}}
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
	if state.Sessions == nil {
		state.Sessions = map[string]sessionRec{}
	}

	// протухшие сессии вычищаем сразу
	now := time.Now()
	for t, s := range state.Sessions {
		if s.Expiry.Before(now) {
			delete(state.Sessions, t)
		}
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

// createUser — регистрация; логин латиницей, пароль bcrypt
func createUser(login, password string) (*User, error) {
	if !loginRe.MatchString(login) {
		return nil, errors.New("логин: 3-32 символа, латиница/цифры/._-")
	}
	if len(password) < 6 {
		return nil, errors.New("пароль: минимум 6 символов")
	}
	for _, u := range state.Users {
		if strings.EqualFold(u.Login, login) {
			return nil, errors.New("логин занят")
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := &User{
		ID:        randHex(8),
		Login:     login,
		PassHash:  string(hash),
		FeedToken: randHex(16),
	}
	state.Users = append(state.Users, u)
	return u, nil
}

func checkPassword(u *User, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.PassHash), []byte(password)) == nil
}

func userByLogin(login string) *User {
	for _, u := range state.Users {
		if strings.EqualFold(u.Login, login) {
			return u
		}
	}
	return nil
}

func userByFeedToken(token string) *User {
	for _, u := range state.Users {
		if u.FeedToken == token {
			return u
		}
	}
	return nil
}

func userBySession(token string) *User {
	s, ok := state.Sessions[token]
	if !ok || s.Expiry.Before(time.Now()) {
		return nil
	}
	for _, u := range state.Users {
		if u.ID == s.UserID {
			return u
		}
	}
	return nil
}

func openSession(userID string) (token string) {
	token = randHex(16)
	state.Sessions[token] = sessionRec{UserID: userID, Expiry: time.Now().Add(30 * 24 * time.Hour)}
	return token
}

func (u *User) sub(id string) *Sub {
	for _, s := range u.Subs {
		if s.ID == id {
			return s
		}
	}
	return nil
}
