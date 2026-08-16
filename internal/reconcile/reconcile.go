package reconcile

import (
	"context"
	"log"
	"nexus/internal/store"
	"strings"
	"time"
)

func StartReconciler(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second) // 2s tick to speed up manual testing, realistic could be 5-10s
	defer ticker.Stop()

	// Keep track of keys that mismatched on the PREVIOUS tick
	previousMismatches := make(map[string]bool)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentMismatches := make(map[string]bool)

			rows, err := store.DB.Query(`
				SELECT c.key, c.value, w.status, c.cached_at, w.updated_at
				FROM cache_entries c 
				JOIN work_items w ON c.key = w.id 
				WHERE c.value != w.status OR w.updated_at > datetime(c.cached_at, '+60 seconds')
			`)
			if err != nil {
				log.Printf("Reconciler error querying mismatches: %v", err)
				continue
			}

			var toLog []string
			for rows.Next() {
				var key, cacheVal, dbVal string
				var cachedAt, updatedAt time.Time
				if err := rows.Scan(&key, &cacheVal, &dbVal, &cachedAt, &updatedAt); err != nil {
					continue
				}
				currentMismatches[key] = true

				if previousMismatches[key] {
					log.Printf("[RECONCILE] key %s mismatched (tick 2 of 2) -> firing disagreement event", key)
					var reason string
					if cacheVal != dbVal {
						reason = "cache=" + cacheVal + " != db=" + dbVal
					} else {
						reason = "cache value matches but is too old relative to truth"
					}
					toLog = append(toLog, key+"|"+reason)
					// Remove from current so it doesn't log every single tick if it never resolves
					delete(currentMismatches, key)
				} else {
					log.Printf("[RECONCILE] key %s mismatched (tick 1 of 2) -> waiting for next tick", key)
				}
			}
			rows.Close()

			for _, l := range toLog {
				parts := strings.SplitN(l, "|", 2)
				reason := parts[1]
				_ = store.CreateEvent("cache", parts[0], "disagreement", &reason, nil)
			}

			previousMismatches = currentMismatches
		}
	}
}
