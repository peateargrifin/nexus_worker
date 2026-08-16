package chaos

import (
	"encoding/json"
	"net/http"
	"nexus/internal/dispatch"
	"nexus/internal/store"
	"nexus/internal/supervisor"
	"time"
)

func HandleKillWorker(w http.ResponseWriter, r *http.Request) {
	workerID := r.PathValue("id")
	supervisor.DefaultSupervisor.KillWorker(workerID)
	w.WriteHeader(http.StatusOK)
}

func HandleCrashLoop(w http.ResponseWriter, r *http.Request) {
	workerID := r.PathValue("id")
	supervisor.DefaultSupervisor.SetChaos(workerID, "crash-loop")
	w.WriteHeader(http.StatusOK)
}

func HandleCrashOnce(w http.ResponseWriter, r *http.Request) {
	workerID := r.PathValue("id")
	supervisor.DefaultSupervisor.SetChaos(workerID, "crash-once")
	w.WriteHeader(http.StatusOK)
}

var DependencyDown bool

func HandleDependencyDown(w http.ResponseWriter, r *http.Request) {
	DependencyDown = !DependencyDown
	w.WriteHeader(http.StatusOK)
}

func HandleDrainerDown(w http.ResponseWriter, r *http.Request) {
	dispatch.DrainerDown = !dispatch.DrainerDown
	w.WriteHeader(http.StatusOK)
}

func HandleDrift(w http.ResponseWriter, r *http.Request) {
	// Pick any existing work item to demonstrate drift
	var key string
	err := store.DB.QueryRow("SELECT id FROM work_items LIMIT 1").Scan(&key)
	if err == nil && key != "" {
		_ = store.UpsertCacheEntry(key, "drifted_status", 60)
	}
	w.WriteHeader(http.StatusOK)
}

func HandleFailWork(w http.ResponseWriter, r *http.Request) {
	workerID := r.PathValue("id")
	supervisor.DefaultSupervisor.SetChaos(workerID, "fail-work")
	w.WriteHeader(http.StatusOK)
}

func HandleDuplicateDelivery(w http.ResponseWriter, r *http.Request) {
	// Re-deliver an item that is already done to test R-03 delivery-side idempotency
	// Simulating a worker that took too long, was timed out, but still reports "done" later.
	var id string
	err := store.DB.QueryRow("SELECT id FROM work_items WHERE status = 'done' LIMIT 1").Scan(&id)
	if err != nil {
		http.Error(w, "no done item found to duplicate", http.StatusBadRequest)
		return
	}

	done, err := store.MarkWorkItemDone(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	if done {
		_ = store.CreateEvent("work_item", id, "done", nil, nil)
	} else {
		reason := "duplicate_delivery_suppressed"
		_ = store.CreateEvent("work_item", id, "duplicate_suppressed", &reason, nil)
	}
	
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id": id,
		"was_new_done": done,
	})
}

func HandleBadRelease(w http.ResponseWriter, r *http.Request) {
	// Push a release without a previous version, should fail (R-06)
	rel := &store.Release{
		ID:              "r-" + time.Now().Format("20060102150405"),
		Version:         "bad-version",
		PreviousVersion: "", // Missing!
		Status:          "watching",
		StartedAt:       time.Now(),
		WatchUntil:      time.Now().Add(5 * time.Minute),
	}

	err := store.CreateRelease(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}
