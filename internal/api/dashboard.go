package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"nexus/internal/store"
	"time"
)

func GetDiagnosisString() string {
	var anomaly store.Event
	err := store.DB.QueryRow(`
		SELECT id, ts, entity_type, entity_id, action, reason 
		FROM events 
		WHERE entity_type = 'worker' AND action IN ('killed', 'budget_exhausted', 'panicked') 
		ORDER BY ts DESC LIMIT 1
	`).Scan(&anomaly.ID, &anomaly.Ts, &anomaly.EntityType, &anomaly.EntityID, &anomaly.Action, &anomaly.Reason)

	if err != nil {
		return ""
	}

	var rel store.Event
	err = store.DB.QueryRow(`
		SELECT id, ts, entity_type, entity_id, action 
		FROM events 
		WHERE entity_type = 'release' AND action = 'pushed' AND ts <= ? AND ts >= ?
		ORDER BY ts DESC LIMIT 1
	`, anomaly.Ts, anomaly.Ts.Add(-5*time.Minute)).Scan(&rel.ID, &rel.Ts, &rel.EntityType, &rel.EntityID, &rel.Action)

	actionStr := "crashing"
	if anomaly.Action == "budget_exhausted" {
		actionStr = "exhausting its restart budget"
	} else if anomaly.Action == "killed" {
		actionStr = "being killed"
	} else if anomaly.Action == "panicked" {
		actionStr = "crash-looping"
	}

	if err != nil {
		return fmt.Sprintf("Worker %s has been %s since %s. No recent release found.",
			anomaly.EntityID,
			actionStr,
			anomaly.Ts.Format("15:04:05"),
		)
	}

	return fmt.Sprintf("Worker %s has been %s since %s, %d seconds after release %s went out.",
		anomaly.EntityID,
		actionStr,
		anomaly.Ts.Format("15:04:05"),
		int(anomaly.Ts.Sub(rel.Ts).Seconds()),
		rel.EntityID,
	)
}

func handleGetDiagnosis(w http.ResponseWriter, r *http.Request) {
	diag := GetDiagnosisString()
	if diag == "" {
		diag = "No anomalous worker behavior detected recently."
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"diagnosis": diag,
	})
}

type DashboardStatus struct {
	Diagnosis    *DiagnosisInfo     `json:"diagnosis"`
	Backlog      BacklogInfo        `json:"backlog"`
	Workers      []WorkerInfo       `json:"workers"`
	DeadLetters  []DeadLetterInfo   `json:"dead_letters"`
	RecentEvents []store.Event      `json:"recent_events"`
}

type DiagnosisInfo struct {
	Text     string `json:"text"`
	Severity string `json:"severity"`
}

type BacklogInfo struct {
	Pending          int  `json:"pending"`
	OldestAgeSeconds *int `json:"oldest_age_seconds"`
	DeadLetterCount  int  `json:"dead_letter_count"`
}

type WorkerInfo struct {
	ID             string     `json:"id"`
	Status         string     `json:"status"`
	Version        string     `json:"version"`
	RestartCount   int        `json:"restart_count"`
	LastHealthyAt  *time.Time `json:"last_healthy_at"`
}

type DeadLetterInfo struct {
	ID     string    `json:"id"`
	Type   string    `json:"type"`
	Reason string    `json:"reason"`
	Since  time.Time `json:"since"`
}

func handleGetStatus(w http.ResponseWriter, r *http.Request) {
	status := DashboardStatus{
		Workers:      []WorkerInfo{},
		DeadLetters:  []DeadLetterInfo{},
		RecentEvents: []store.Event{},
	}

	// 1. Backlog
	_ = store.DB.QueryRow("SELECT COUNT(*) FROM work_items WHERE status = 'pending'").Scan(&status.Backlog.Pending)
	var oldest *time.Time
	_ = store.DB.QueryRow("SELECT updated_at FROM work_items WHERE status = 'pending' ORDER BY updated_at ASC LIMIT 1").Scan(&oldest)
	if oldest != nil {
		age := int(time.Since(*oldest).Seconds())
		status.Backlog.OldestAgeSeconds = &age
	}
	_ = store.DB.QueryRow("SELECT COUNT(*) FROM work_items WHERE status = 'dead_letter'").Scan(&status.Backlog.DeadLetterCount)

	// 2. Workers
	rowsW, _ := store.DB.Query("SELECT id, status, version, restart_count, last_healthy_at FROM workers ORDER BY id ASC")
	if rowsW != nil {
		for rowsW.Next() {
			var wi WorkerInfo
			_ = rowsW.Scan(&wi.ID, &wi.Status, &wi.Version, &wi.RestartCount, &wi.LastHealthyAt)
			status.Workers = append(status.Workers, wi)
		}
		rowsW.Close()
	}

	// 3. Dead letters
	rowsDL, _ := store.DB.Query("SELECT id, type, dead_letter_reason, updated_at FROM work_items WHERE status = 'dead_letter' ORDER BY updated_at DESC LIMIT 50")
	if rowsDL != nil {
		for rowsDL.Next() {
			var dl DeadLetterInfo
			var reason sql.NullString
			_ = rowsDL.Scan(&dl.ID, &dl.Type, &reason, &dl.Since)
			dl.Reason = reason.String
			status.DeadLetters = append(status.DeadLetters, dl)
		}
		rowsDL.Close()
	}

	// 4. Recent Events
	rowsE, _ := store.DB.Query("SELECT id, ts, entity_type, entity_id, action, reason, caused_by_event_id FROM events ORDER BY ts DESC LIMIT 50")
	if rowsE != nil {
		for rowsE.Next() {
			var e store.Event
			_ = rowsE.Scan(&e.ID, &e.Ts, &e.EntityType, &e.EntityID, &e.Action, &e.Reason, &e.CausedByEventID)
			status.RecentEvents = append(status.RecentEvents, e)
		}
		rowsE.Close()
	}

	// 5. Diagnosis
	diag := GetDiagnosisString()
	if diag != "" {
		status.Diagnosis = &DiagnosisInfo{Text: diag, Severity: "critical"}
	} else if status.Backlog.DeadLetterCount > 0 {
		status.Diagnosis = &DiagnosisInfo{Text: "Some items are dead-lettered.", Severity: "warning"}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
