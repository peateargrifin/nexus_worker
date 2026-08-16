package api

import (
	"encoding/json"
	"net/http"
	"nexus/internal/chaos"
	"nexus/internal/dispatch"
	"nexus/internal/release"
	"nexus/internal/store"
	"nexus/internal/supervisor"
	"time"
)

func NewRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// Core endpoints
	mux.HandleFunc("POST /work", handlePostWork)
	mux.HandleFunc("POST /work/{id}/replay", handleReplayWork)
	mux.HandleFunc("GET /events", handleGetEvents)
	mux.HandleFunc("POST /releases", handlePostRelease)
	mux.HandleFunc("POST /releases/{id}/rollback", handleRollbackRelease)
	mux.HandleFunc("POST /workers/{id}/revive", handleReviveWorker)
	mux.HandleFunc("GET /cache/{key}", handleGetCache)
	mux.HandleFunc("GET /diagnosis", handleGetDiagnosis)
	mux.HandleFunc("GET /status", handleGetStatus)
	mux.HandleFunc("POST /work/{id}/claim", handleClaimWork)
	mux.HandleFunc("GET /beliefs", handleGetBeliefs)

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "dashboard.html")
		} else {
			http.NotFound(w, r)
		}
	})

	// Chaos endpoints
	mux.HandleFunc("POST /chaos/kill-worker/{id}", chaos.HandleKillWorker)
	mux.HandleFunc("POST /chaos/crash-loop/{id}", chaos.HandleCrashLoop)
	mux.HandleFunc("POST /chaos/crash-once/{id}", chaos.HandleCrashOnce)
	mux.HandleFunc("POST /chaos/fail-work/{id}", chaos.HandleFailWork)
	mux.HandleFunc("POST /chaos/duplicate", chaos.HandleDuplicateDelivery)
	mux.HandleFunc("POST /chaos/bad-release", chaos.HandleBadRelease)
	mux.HandleFunc("POST /chaos/dependency-down", chaos.HandleDependencyDown)
	mux.HandleFunc("POST /chaos/toggle-drainer", chaos.HandleDrainerDown)
	mux.HandleFunc("POST /chaos/drift/{key}", chaos.HandleDrift)

	return mux
}

func handlePostWork(w http.ResponseWriter, r *http.Request) {
	var item store.WorkItem
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	item.CreatedAt = time.Now()
	item.UpdatedAt = item.CreatedAt
	item.Status = "pending"
	if item.MaxAttempts == 0 {
		item.MaxAttempts = 5
	}

	isNew, err := dispatch.AcceptWork(r.Context(), &item)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if !isNew {
		w.WriteHeader(http.StatusOK) // duplicate delivery is harmless
		json.NewEncoder(w).Encode(map[string]string{"status": "duplicate_suppressed"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

func handleReplayWork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	res, err := store.DB.Exec("UPDATE work_items SET status = 'replay_queued', updated_at = ? WHERE id = ? AND status = 'dead_letter'", time.Now(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		http.Error(w, "Item not found or not dead_lettered", http.StatusBadRequest)
		return
	}

	_ = store.CreateEvent("work_item", id, "replay_queued", nil, nil)
	w.WriteHeader(http.StatusOK)
}

func handleClaimWork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		WorkerID string `json:"worker_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	claimed, err := store.AssignWorkItem(id, body.WorkerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !claimed {
		http.Error(w, "conflict", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleGetBeliefs(w http.ResponseWriter, r *http.Request) {
	beliefs := map[string]any{
		"dependency_down": chaos.DependencyDown,
		"drainer_down":    dispatch.DrainerDown,
	}

	rel, _ := store.GetActiveRelease()
	beliefs["active_release"] = rel

	workers, _ := store.DB.Query("SELECT id, status, version, restart_count FROM workers")
	var workerList []map[string]any
	if workers != nil {
		for workers.Next() {
			var id, status, version string
			var count int
			workers.Scan(&id, &status, &version, &count)
			workerList = append(workerList, map[string]any{
				"id":            id,
				"status":        status,
				"version":       version,
				"restart_count": count,
			})
		}
		workers.Close()
	}
	beliefs["workers"] = workerList

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(beliefs)
}

func handleGetEvents(w http.ResponseWriter, r *http.Request) {
	sinceStr := r.URL.Query().Get("since")
	entityType := r.URL.Query().Get("entity_type")
	entityID := r.URL.Query().Get("entity_id")

	var since time.Time
	if sinceStr != "" {
		parsed, err := time.Parse(time.RFC3339, sinceStr)
		if err == nil {
			since = parsed
		}
	}

	events, err := store.GetEvents(since, entityType, entityID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func handlePostRelease(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version         string `json:"version"`
		PreviousVersion string `json:"previous_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rel, err := release.PushRelease(body.Version, body.PreviousVersion)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rel)
}

func handleRollbackRelease(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := release.Rollback(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func handleReviveWorker(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := supervisor.DefaultSupervisor.ReviveWorker(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
