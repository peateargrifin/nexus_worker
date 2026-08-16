package store

import (
	"database/sql"
	"errors"
)

func CreateRelease(r *Release) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// R-06: require previous version
	if r.PreviousVersion == "" {
		return errors.New("previous_version is required")
	}

	_, err = tx.Exec(`
		INSERT INTO releases (id, version, previous_version, status, started_at, watch_until)
		VALUES (?, ?, ?, ?, ?, ?)
	`, r.ID, r.Version, r.PreviousVersion, r.Status, r.StartedAt, r.WatchUntil)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func RollbackRelease(id string) (*Release, error) {
	// one action: flip active version, update status
	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	_, err = tx.Exec("UPDATE releases SET status = 'rolled_back' WHERE id = ?", id)
	if err != nil {
		return nil, err
	}

	var r Release
	err = tx.QueryRow("SELECT id, version, previous_version, status, started_at, watch_until FROM releases WHERE id = ?", id).
		Scan(&r.ID, &r.Version, &r.PreviousVersion, &r.Status, &r.StartedAt, &r.WatchUntil)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &r, nil
}

func GetRelease(id string) (*Release, error) {
	var r Release
	err := DB.QueryRow("SELECT id, version, previous_version, status, started_at, watch_until FROM releases WHERE id = ?", id).
		Scan(&r.ID, &r.Version, &r.PreviousVersion, &r.Status, &r.StartedAt, &r.WatchUntil)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func GetActiveRelease() (*Release, error) {
	var r Release
	err := DB.QueryRow("SELECT id, version, previous_version, status, started_at, watch_until FROM releases WHERE status != 'rolled_back' ORDER BY started_at DESC LIMIT 1").
		Scan(&r.ID, &r.Version, &r.PreviousVersion, &r.Status, &r.StartedAt, &r.WatchUntil)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
