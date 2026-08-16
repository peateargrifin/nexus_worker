package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"nexus/internal/chaos"
	"nexus/internal/store"
	"time"
)

func GetDiagnosis() *DiagnosisInfo {
	var anomaly store.Event
	err := store.DB.QueryRow(`
		SELECT e.id, e.ts, e.entity_type, e.entity_id, e.action, e.reason 
		FROM events e
		JOIN workers w ON w.id = e.entity_id
		WHERE e.entity_type = 'worker' AND e.action IN ('killed', 'budget_exhausted', 'panicked') 
		AND w.status IN ('dead', 'restarting')
		AND e.ts >= datetime('now', '-5 minutes')
		ORDER BY e.ts DESC LIMIT 1
	`).Scan(&anomaly.ID, &anomaly.Ts, &anomaly.EntityType, &anomaly.EntityID, &anomaly.Action, &anomaly.Reason)

	if err != nil {
		return nil
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

	info := &DiagnosisInfo{
		Severity: "critical",
		WorkerID: anomaly.EntityID,
		Action:   actionStr,
		Since:    anomaly.Ts.Local().Format("15:04:05"),
	}

	if err == nil {
		info.RelatedReleaseID = rel.EntityID
		info.Text = fmt.Sprintf("Worker %s has been %s since %s, %d seconds after release %s went out.",
			anomaly.EntityID, actionStr, info.Since, int(anomaly.Ts.Sub(rel.Ts).Seconds()), rel.EntityID)
	} else {
		info.Text = fmt.Sprintf("Worker %s has been %s since %s. No recent release found.",
			anomaly.EntityID, actionStr, info.Since)
	}

	return info
}

func handleGetDiagnosis(w http.ResponseWriter, r *http.Request) {
	diag := GetDiagnosis()
	w.Header().Set("Content-Type", "application/json")
	if diag == nil {
		json.NewEncoder(w).Encode(map[string]string{"diagnosis": "No anomalous worker behavior detected recently."})
	} else {
		json.NewEncoder(w).Encode(diag)
	}
}

type DashboardStatus struct {
	Diagnosis      *DiagnosisInfo     `json:"diagnosis"`
	PlatformHealthy bool              `json:"platform_healthy"`
	Backlog        BacklogInfo        `json:"backlog"`
	Workers        []WorkerInfo       `json:"workers"`
	DeadLetters    []DeadLetterInfo   `json:"dead_letters"`
	RecentEvents   []store.Event      `json:"recent_events"`
	ActiveRelease  *store.Release     `json:"active_release"`
}

type DiagnosisInfo struct {
	Text             string `json:"text"`
	Severity         string `json:"severity"`
	WorkerID         string `json:"worker_id"`
	Action           string `json:"action"`
	Since            string `json:"since"`
	RelatedReleaseID string `json:"related_release_id"`
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
		PlatformHealthy: !chaos.DependencyDown,
		Workers:         []WorkerInfo{},
		DeadLetters:     []DeadLetterInfo{},
		RecentEvents:    []store.Event{},
	}

	rel, _ := store.GetActiveRelease()
	status.ActiveRelease = rel

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
	diag := GetDiagnosis()
	if diag != nil {
		status.Diagnosis = diag
	} else if status.Backlog.DeadLetterCount > 0 {
		status.Diagnosis = &DiagnosisInfo{Text: "Some items are dead-lettered.", Severity: "warning"}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
