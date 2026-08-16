package store

import (
	"database/sql"
	"time"
)

type CacheEntry struct {
	Key           string    `json:"key"`
	Value         string    `json:"value"`
	CachedAt      time.Time `json:"cached_at"`
	MaxAgeSeconds int       `json:"max_age_seconds"`
}

func UpsertCacheEntry(key, value string, maxAge int) error {
	_, err := DB.Exec(`
		INSERT INTO cache_entries (key, value, cached_at, max_age_seconds)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET 
			value = excluded.value, 
			cached_at = excluded.cached_at,
			max_age_seconds = excluded.max_age_seconds
	`, key, value, time.Now(), maxAge)
	return err
}

func GetCacheEntry(key string) (*CacheEntry, error) {
	row := DB.QueryRow("SELECT key, value, cached_at, max_age_seconds FROM cache_entries WHERE key = ?", key)
	var c CacheEntry
	err := row.Scan(&c.Key, &c.Value, &c.CachedAt, &c.MaxAgeSeconds)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func GetAllCacheEntries() ([]CacheEntry, error) {
	rows, err := DB.Query("SELECT key, value, cached_at, max_age_seconds FROM cache_entries")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []CacheEntry
	for rows.Next() {
		var c CacheEntry
		if err := rows.Scan(&c.Key, &c.Value, &c.CachedAt, &c.MaxAgeSeconds); err != nil {
			return nil, err
		}
		entries = append(entries, c)
	}
	return entries, nil
}
