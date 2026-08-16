package supervisor

import (
	"context"
	"fmt"
	"log"
	"nexus/internal/dispatch"
	"nexus/internal/store"
	"sync"
	"time"
)

type Supervisor struct {
	workers       map[string]context.CancelFunc
	mu            sync.Mutex
	workChan      chan *store.WorkItem
	chaosSettings map[string]string // e.g. "worker-1" -> "crash-loop"
}

var DefaultSupervisor = &Supervisor{
	workers:       make(map[string]context.CancelFunc),
	workChan:      make(chan *store.WorkItem, 100),
	chaosSettings: make(map[string]string),
}

func (s *Supervisor) Start(ctx context.Context, workerCount int) {
	go dispatch.StartDispatcher(ctx, s.workChan)

	for i := 1; i <= workerCount; i++ {
		workerID := fmt.Sprintf("w-%d", i)
		s.spawnWorker(workerID)
	}
}

func (s *Supervisor) InjectWork(item *store.WorkItem) {
	s.workChan <- item
}


func (s *Supervisor) SetChaos(workerID, mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chaosSettings[workerID] = mode
}

func (s *Supervisor) spawnWorker(workerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cancel, exists := s.workers[workerID]; exists {
		cancel() // Ensure no zombie
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.workers[workerID] = cancel

	now := time.Now()
	worker := &store.Worker{
		ID:                 workerID,
		Status:             "starting",
		RestartCount:       0,
		RestartWindowStart: &now,
	}
	// Fetch previous state to preserve restart budgets (R-04)
	if existing, err := store.GetWorker(workerID); err == nil {
		worker.RestartCount = existing.RestartCount
		worker.RestartWindowStart = existing.RestartWindowStart
		worker.LastHealthyAt = existing.LastHealthyAt
	}

	worker.RestartCount++
	if worker.RestartCount > 5 {
		log.Printf("Worker %s exhausted restart budget", workerID)
		worker.Status = "dead"
		_ = store.UpsertWorker(worker)
		_ = store.CreateEvent("worker", workerID, "budget_exhausted", nil, nil)
		return
	}

	_ = store.UpsertWorker(worker)
	_ = store.CreateEvent("worker", workerID, "starting", nil, nil)

	go s.runWorker(ctx, workerID)
}

func (s *Supervisor) runWorker(ctx context.Context, workerID string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Worker %s panicked: %v", workerID, r)
			reason := fmt.Sprintf("%v", r)
			_ = store.CreateEvent("worker", workerID, "panicked", &reason, nil)
			
			// Restart logic
			time.AfterFunc(1*time.Second, func() {
				s.spawnWorker(workerID)
			})
		}
	}()

	worker, _ := store.GetWorker(workerID)
	worker.Status = "running"
	now := time.Now()
	worker.LastHealthyAt = &now
	_ = store.UpsertWorker(worker)
	_ = store.CreateEvent("worker", workerID, "running", nil, nil)

	budgetResetTimer := time.NewTimer(30 * time.Second)
	defer budgetResetTimer.Stop()

	chaosMode := func() string {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.chaosSettings[workerID]
	}

	for {
		if chaosMode() == "crash-loop" {
			panic("chaos: crash-loop")
		} else if chaosMode() == "crash-once" {
			s.mu.Lock()
			s.chaosSettings[workerID] = ""
			s.mu.Unlock()
			panic("chaos: crash-once")
		}

		select {
		case <-ctx.Done():
			log.Printf("Worker %s context cancelled", workerID)
			worker.Status = "dead"
			_ = store.UpsertWorker(worker)
			_ = store.CreateEvent("worker", workerID, "killed", nil, nil)
			return
		case <-budgetResetTimer.C:
			// Continuous 30s of health achieved! Earn recovered.
			if w, err := store.GetWorker(workerID); err == nil && w.RestartCount > 0 {
				w.RestartCount = 0
				_ = store.UpsertWorker(w)
				log.Printf("Worker %s earned its restart budget back", workerID)
				_ = store.CreateEvent("worker", workerID, "budget_recovered", nil, nil)
			}
		case item := <-s.workChan:
			assigned, err := store.AssignWorkItem(item.ID, workerID)
			if err != nil || !assigned {
				continue
			}

			log.Printf("Worker %s processing item %s", workerID, item.ID)

			// Simulate work
			time.Sleep(500 * time.Millisecond)

			if chaosMode() == "fail-work" {
				dispatch.RetryOrDeadLetter(item, "chaos: fail-work")
				continue
			}

			done, err := store.MarkWorkItemDone(item.ID)
			if err == nil && done {
				_ = store.CreateEvent("work_item", item.ID, "done", nil, nil)
			}
		}
	}
}

func (s *Supervisor) KillWorker(workerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, exists := s.workers[workerID]; exists {
		cancel()
		delete(s.workers, workerID)
	}
}

func (s *Supervisor) ReviveWorker(workerID string) error {
	worker, err := store.GetWorker(workerID)
	if err != nil {
		return err
	}
	worker.RestartCount = -1 // will become 0 in spawnWorker
	_ = store.UpsertWorker(worker)
	
	s.spawnWorker(workerID)
	_ = store.CreateEvent("worker", workerID, "revived", nil, nil)
	return nil
}

func (s *Supervisor) RestartAll() {
	s.mu.Lock()
	var ids []string
	for id := range s.workers {
		ids = append(ids, id)
	}
	s.mu.Unlock()

	for _, id := range ids {
		s.spawnWorker(id)
	}
}
