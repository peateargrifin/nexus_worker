package store

import "time"

func CreateEvent(entityType, entityID, action string, reason *string, causedByEventID *int) error {
	if causedByEventID == nil && entityType != "release" {
		var activeReleaseEventID int
		err := DB.QueryRow(`
			SELECT e.id 
			FROM releases r
			JOIN events e ON e.entity_type = 'release' AND e.entity_id = r.id AND e.action = 'pushed'
			WHERE r.status = 'watching' AND r.watch_until > ?
			ORDER BY r.started_at DESC LIMIT 1
		`, time.Now().UTC()).Scan(&activeReleaseEventID)
		if err == nil {
			causedByEventID = &activeReleaseEventID
		}
	}

	_, err := DB.Exec(`
		INSERT INTO events (entity_type, entity_id, action, reason, caused_by_event_id)
		VALUES (?, ?, ?, ?, ?)
	`, entityType, entityID, action, reason, causedByEventID)
	return err
}

func GetEvents(since time.Time, entityType string, entityID string) ([]Event, error) {
	query := "SELECT id, ts, entity_type, entity_id, action, reason, caused_by_event_id FROM events WHERE ts >= ?"
	args := []interface{}{since}

	if entityType != "" {
		query += " AND entity_type = ?"
		args = append(args, entityType)
	}
	if entityID != "" {
		query += " AND entity_id = ?"
		args = append(args, entityID)
	}

	query += " ORDER BY ts ASC"

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Ts, &e.EntityType, &e.EntityID, &e.Action, &e.Reason, &e.CausedByEventID); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}
