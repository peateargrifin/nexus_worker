package api

import (
	"encoding/json"
	"net/http"
	"nexus/internal/chaos"
	"nexus/internal/store"
	"time"
)

func handleGetCache(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	
	if chaos.DependencyDown {
		// Honest degradation (R-10)
		entry, err := store.GetCacheEntry(key)
		if err != nil || entry == nil {
			http.Error(w, "dependency down and no cache available", http.StatusServiceUnavailable)
			return
		}
		
		age := int(time.Since(entry.CachedAt).Seconds())
		if age > entry.MaxAgeSeconds {
			http.Error(w, "dependency down and cache expired", http.StatusServiceUnavailable)
			return
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"key": key,
			"value": entry.Value,
			"stale": true,
			"age_seconds": age,
		})
		return
	}

	// Normal path: fetch from primary DB (simulated real dependency)
	wi, err := store.GetWorkItem(key)
	if err != nil || wi == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	
	// Update cache
	_ = store.UpsertCacheEntry(key, wi.Status, 60)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"key": key,
		"value": wi.Status,
		"stale": false,
		"age_seconds": 0,
	})
}
