package dispatch

import (
	"context"
	"log"
	"nexus/internal/store"
	"time"
)

// StartDrainer continuously drains replay_queued items back to active dispatch
// at a maximum rate of 20 items per second (R-11).
var DrainerDown bool

func StartDrainer(ctx context.Context) {
	ticker := time.NewTicker(time.Second / 20)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if DrainerDown {
				continue
			}
			res, err := store.DB.Exec(`
				UPDATE work_items 
				SET status = 'pending', attempt_count = 0, assigned_worker = NULL, dead_letter_reason = NULL, updated_at = ?
				WHERE id IN (
					SELECT id FROM work_items WHERE status = 'replay_queued' ORDER BY updated_at ASC LIMIT 1
				)
			`, time.Now())
			if err != nil {
				log.Printf("Drainer error: %v", err)
				continue
			}

			affected, _ := res.RowsAffected()
			if affected == 0 {
				// Backoff slightly to avoid slamming the DB when there's no backlog
				time.Sleep(500 * time.Millisecond)
			} else {
			    log.Printf("[DRAIN] %s: Drained 1 item from replay_queued to pending", time.Now().Format("15:04:05.000000"))
			}
		}
	}
}
