package release

import (
	"nexus/internal/store"
	"nexus/internal/supervisor"
	"time"
)

func PushRelease(version, previousVersion string) (*store.Release, error) {
	r := &store.Release{
		ID:              "r-" + time.Now().Format("20060102150405"),
		Version:         version,
		PreviousVersion: previousVersion,
		Status:          "watching",
		StartedAt:       time.Now(),
		WatchUntil:      time.Now().Add(5 * time.Minute),
	}

	err := store.CreateRelease(r)
	if err != nil {
		return nil, err
	}

	_ = store.CreateEvent("release", r.ID, "pushed", nil, nil)

	// Restart workers to pick up release
	supervisor.DefaultSupervisor.RestartAll()

	return r, nil
}

func Rollback(id string) error {
	r, err := store.RollbackRelease(id)
	if err != nil || r == nil {
		return err
	}

	_ = store.CreateEvent("release", id, "rolled_back", nil, nil)

	// Restart workers to pick up previous_version
	supervisor.DefaultSupervisor.RestartAll()
	return nil
}
