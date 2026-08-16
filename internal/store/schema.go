package store

func createSchema() error {
	schema := `
CREATE TABLE IF NOT EXISTS work_items (
  id                 TEXT PRIMARY KEY,
  type               TEXT NOT NULL,
  body               TEXT NOT NULL,
  status             TEXT NOT NULL DEFAULT 'pending',   -- pending | assigned | done | dead_letter
  attempt_count      INTEGER NOT NULL DEFAULT 0,
  max_attempts       INTEGER NOT NULL DEFAULT 5,
  next_retry_at      DATETIME,
  assigned_worker    TEXT,
  idempotency_token  TEXT NOT NULL,
  dead_letter_reason TEXT,
  created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS events (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  ts                  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  entity_type         TEXT NOT NULL,   -- work_item | worker | release | cache
  entity_id           TEXT NOT NULL,
  action              TEXT NOT NULL,
  reason              TEXT,
  caused_by_event_id  INTEGER REFERENCES events(id)   -- NULL = no known causal link (R-13)
);

CREATE TABLE IF NOT EXISTS releases (
  id                TEXT PRIMARY KEY,
  version           TEXT NOT NULL,
  previous_version  TEXT NOT NULL,     -- required at creation — R-06 "know how to undo before doing"
  status            TEXT NOT NULL DEFAULT 'watching',   -- watching | committed | rolled_back
  started_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  watch_until       DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS workers (
  id                     TEXT PRIMARY KEY,
  pid                    INTEGER,
  status                 TEXT NOT NULL DEFAULT 'starting', -- starting | running | restarting | dead
  version                TEXT NOT NULL DEFAULT 'v1.0',
  restart_count          INTEGER NOT NULL DEFAULT 0,
  restart_window_start   DATETIME,
  last_healthy_at        DATETIME
);

CREATE TABLE IF NOT EXISTS cache_entries (
  key             TEXT PRIMARY KEY,
  value           TEXT NOT NULL,
  cached_at       DATETIME NOT NULL,
  max_age_seconds INTEGER NOT NULL
);
`
	_, err := DB.Exec(schema)
	if err == nil {
		DB.Exec("ALTER TABLE workers ADD COLUMN version TEXT NOT NULL DEFAULT 'v1.0'")
	}
	return err
}
