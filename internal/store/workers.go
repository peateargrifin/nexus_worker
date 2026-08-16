package store

import "time"

type Worker struct {
	ID                 string
	PID                int
	Status             string
	RestartCount       int
	RestartWindowStart *time.Time
	LastHealthyAt      *time.Time
}

func UpsertWorker(w *Worker) error {
	_, err := DB.Exec(`
		INSERT INTO workers (id, pid, status, restart_count, restart_window_start, last_healthy_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET 
		  pid=excluded.pid, 
		  status=excluded.status, 
		  restart_count=excluded.restart_count, 
		  restart_window_start=excluded.restart_window_start, 
		  last_healthy_at=excluded.last_healthy_at
	`, w.ID, w.PID, w.Status, w.RestartCount, w.RestartWindowStart, w.LastHealthyAt)
	return err
}

func GetWorker(id string) (*Worker, error) {
	var w Worker
	err := DB.QueryRow("SELECT id, pid, status, restart_count, restart_window_start, last_healthy_at FROM workers WHERE id = ?", id).
		Scan(&w.ID, &w.PID, &w.Status, &w.RestartCount, &w.RestartWindowStart, &w.LastHealthyAt)
	if err != nil {
		return nil, err
	}
	return &w, nil
}
