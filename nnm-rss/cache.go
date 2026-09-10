package main

// Персистентный кэш на томе (cache.json рядом со state.json): резолвы тем и
// тела родных лент/страниц топа переживают рестарт контейнера. Записываем раз
// в минуту и только при изменениях, старше 72 ч вычищаем.
// (Фоновое обновление по таймеру и «только новое» — следующий шаг, отдельной
// правкой логики запросов к nnm.)

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

var (
	cacheFile  = filepath.Join(env("DATA_DIR", "data"), "cache.json")
	cacheDirty bool // под cacheMu
)

type diskCache struct {
	Resolve map[string]resolveEntry  `json:"resolve"` // темы → Release, рейтинги imdb:/mal:
	RSS     map[string]rssCacheEntry `json:"rss"`     // родные ленты подписок + страницы топа
	Saved   time.Time                `json:"saved"`
}

// loadDiskCache — warm start: накопленное не теряется на рестартах
func loadDiskCache() {
	raw, err := os.ReadFile(cacheFile)
	if err != nil {
		return
	}
	var dc diskCache
	if json.Unmarshal(raw, &dc) != nil {
		return
	}
	cacheMu.Lock()
	for k, v := range dc.Resolve {
		resolveCache[k] = v
	}
	for k, v := range dc.RSS {
		rssCache[k] = v
	}
	cacheMu.Unlock()
	log.Printf("кэш: загружено %d резолвов, %d лент (сохранено %s)",
		len(dc.Resolve), len(dc.RSS), dc.Saved.Format("02.01 15:04"))
}

// markCacheDirty — под cacheMu, в блоках записи кэшей
func markCacheDirty() { cacheDirty = true }

// startCacheSaver — фоновый сброс на диск
func startCacheSaver() {
	go func() {
		for range time.Tick(time.Minute) {
			cacheMu.Lock()
			dirty := cacheDirty
			cacheDirty = false
			cutoff := time.Now().Add(-72 * time.Hour)
			resolve := make(map[string]resolveEntry, len(resolveCache))
			for k, v := range resolveCache {
				if v.ts.After(cutoff) {
					resolve[k] = v
				}
			}
			rss := make(map[string]rssCacheEntry, len(rssCache))
			for k, v := range rssCache {
				if v.ts.After(cutoff) {
					rss[k] = v
				}
			}
			resolveCache, rssCache = resolve, rss
			cacheMu.Unlock()
			if !dirty {
				continue
			}
			raw, err := json.Marshal(diskCache{Resolve: resolve, RSS: rss, Saved: time.Now()})
			if err != nil {
				continue
			}
			tmp := cacheFile + ".tmp"
			if os.WriteFile(tmp, raw, 0o644) == nil {
				os.Rename(tmp, cacheFile)
			}
		}
	}()
}
