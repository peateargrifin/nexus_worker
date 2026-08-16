package store

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func InitDB(dsn string) error {
	var err error
	DB, err = sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// R-01 Connection pool fix
	DB.SetMaxOpenConns(1)

	if _, err := DB.Exec("PRAGMA busy_timeout = 5000;"); err != nil {
		return fmt.Errorf("failed to set busy_timeout: %w", err)
	}

	if _, err := DB.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		return fmt.Errorf("failed to set WAL mode: %w", err)
	}

	if _, err := DB.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	if err := createSchema(); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Startup recovery scan: every row with status='assigned' gets reverted to 'pending' (R-01)
	if _, err := DB.Exec("UPDATE work_items SET status = 'pending', assigned_worker = NULL WHERE status = 'assigned';"); err != nil {
		log.Printf("Warning: failed to reset assigned work items on startup: %v", err)
	}

	// Workers are goroutines, none survive restart, so we can clean up worker state too
	if _, err := DB.Exec("UPDATE workers SET status = 'dead' WHERE status != 'dead';"); err != nil {
		log.Printf("Warning: failed to reset workers on startup: %v", err)
	}

	return nil
}
