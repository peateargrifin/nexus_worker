package dispatch

import (
	"context"
	"log"
	"math"
	"nexus/internal/store"
	"time"
)

func AcceptWork(ctx context.Context, item *store.WorkItem) (bool, error) {
	isNew, err := store.SaveWorkItem(item)
	if err != nil {
		return false, err
	}

	if !isNew {
		log.Printf("Duplicate work item suppressed: %s", item.IdempotencyToken)
		_ = store.CreateEvent("work_item", item.ID, "duplicate_suppressed", nil, nil)
	} else {
		_ = store.CreateEvent("work_item", item.ID, "accepted", nil, nil)
	}

	return isNew, nil
}

func StartDispatcher(ctx context.Context, claimWorkChan chan<- *store.WorkItem) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Dispatcher stopped")
			return
		case <-ticker.C:
			// Fetch pending items
			items, err := store.GetPendingWorkItems(10)
			if err != nil {
				log.Printf("Error fetching pending items: %v", err)
				continue
			}

			for i := range items {
				item := &items[i]
				select {
				case claimWorkChan <- item:
					// Enqueued for a worker
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

func RetryOrDeadLetter(item *store.WorkItem, errReason string) {
	item.AttemptCount++
	if item.AttemptCount >= item.MaxAttempts {
		_ = store.DeadLetterWorkItem(item.ID, errReason, item.AttemptCount)
		_ = store.CreateEvent("work_item", item.ID, "dead_lettered", &errReason, nil)
	} else {
		// Backoff: base * 2^attempt, capped at 5 min
		backoffSeconds := math.Pow(2, float64(item.AttemptCount))
		if backoffSeconds > 300 {
			backoffSeconds = 300
		}
		nextRetryAt := time.Now().Add(time.Duration(backoffSeconds) * time.Second)
		_ = store.RetryWorkItem(item.ID, nextRetryAt, item.AttemptCount)
		reason := "retry_scheduled"
		_ = store.CreateEvent("work_item", item.ID, "retry_scheduled", &reason, nil)
	}
}
