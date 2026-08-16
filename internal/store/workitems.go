package store

import (
	"database/sql"
	"time"
)

// SaveWorkItem inserts a new work item. Returns (isNew, error).
// If isNew is false, a duplicate idempotency_token was found and the insert was suppressed.
func SaveWorkItem(item *WorkItem) (bool, error) {
	// R-01: one transaction. if it fails before commit, nothing is saved.
	tx, err := DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	// Check idempotency first (R-03)
	var existingID string
	err = tx.QueryRow("SELECT id FROM work_items WHERE idempotency_token = ?", item.IdempotencyToken).Scan(&existingID)
	if err == nil {
		// Found existing item with same token
		return false, nil // false indicates duplicate suppressed
	} else if err != sql.ErrNoRows {
		return false, err
	}

	_, err = tx.Exec(`
		INSERT INTO work_items (id, type, body, status, max_attempts, idempotency_token, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Type, item.Body, item.Status, item.MaxAttempts, item.IdempotencyToken, item.CreatedAt, item.UpdatedAt)

	if err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}

	return true, nil
}

func GetPendingWorkItems(limit int) ([]WorkItem, error) {
	rows, err := DB.Query("SELECT id, type, body, status, attempt_count, max_attempts, next_retry_at, idempotency_token FROM work_items WHERE status = 'pending' AND (next_retry_at IS NULL OR next_retry_at <= ?) ORDER BY updated_at ASC LIMIT ?", time.Now(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []WorkItem
	for rows.Next() {
		var item WorkItem
		if err := rows.Scan(&item.ID, &item.Type, &item.Body, &item.Status, &item.AttemptCount, &item.MaxAttempts, &item.NextRetryAt, &item.IdempotencyToken); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func GetAssignedWorkItems(workerID string) ([]WorkItem, error) {
	rows, err := DB.Query("SELECT id, type, body, status, attempt_count, max_attempts, next_retry_at, idempotency_token FROM work_items WHERE status = 'assigned' AND assigned_worker = ?", workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []WorkItem
	for rows.Next() {
		var item WorkItem
		if err := rows.Scan(&item.ID, &item.Type, &item.Body, &item.Status, &item.AttemptCount, &item.MaxAttempts, &item.NextRetryAt, &item.IdempotencyToken); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func AssignWorkItem(id string, workerID string) (bool, error) {
	res, err := DB.Exec("UPDATE work_items SET status = 'assigned', assigned_worker = ?, updated_at = ? WHERE id = ? AND status = 'pending'", workerID, time.Now(), id)
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}

func MarkWorkItemDone(id string) (bool, error) {
	// Conditional update (R-03: duplicate delivery is harmless)
	res, err := DB.Exec("UPDATE work_items SET status = 'done', updated_at = ? WHERE id = ? AND status != 'done'", time.Now(), id)
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}

func DeadLetterWorkItem(id string, reason string, attemptCount int) error {
	_, err := DB.Exec("UPDATE work_items SET status = 'dead_letter', dead_letter_reason = ?, attempt_count = ?, updated_at = ? WHERE id = ?", reason, attemptCount, time.Now(), id)
	return err
}

func RetryWorkItem(id string, nextRetryAt time.Time, attemptCount int) error {
	_, err := DB.Exec("UPDATE work_items SET status = 'pending', next_retry_at = ?, attempt_count = ?, assigned_worker = NULL, updated_at = ? WHERE id = ?", nextRetryAt, attemptCount, time.Now(), id)
	return err
}

func GetWorkItem(id string) (*WorkItem, error) {
	var item WorkItem
	err := DB.QueryRow("SELECT id, type, body, status, attempt_count, max_attempts, next_retry_at, idempotency_token, assigned_worker FROM work_items WHERE id = ?", id).
		Scan(&item.ID, &item.Type, &item.Body, &item.Status, &item.AttemptCount, &item.MaxAttempts, &item.NextRetryAt, &item.IdempotencyToken, &item.AssignedWorker)
	if err != nil {
		return nil, err
	}
	return &item, nil
}
