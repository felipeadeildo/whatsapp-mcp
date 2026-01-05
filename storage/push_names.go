package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// SavePushNames saves multiple push names in a single transaction.
// This is typically called from HistorySync events.
func (s *MessageStore) SavePushNames(pushNames map[string]string) error {
	if len(pushNames) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO push_names (jid, push_name, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET
			push_name = excluded.push_name,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for jid, pushName := range pushNames {
		_, err := stmt.Exec(jid, pushName, now)
		if err != nil {
			return fmt.Errorf("failed to save push name for %s: %w", jid, err)
		}
	}

	return tx.Commit()
}

// GetPushName retrieves a single push name by JID.
// It returns an empty string if the JID is not found.
func (s *MessageStore) GetPushName(jid string) (string, error) {
	var pushName string
	err := s.db.QueryRow("SELECT push_name FROM push_names WHERE jid = ?", jid).Scan(&pushName)
	if err == sql.ErrNoRows {
		return "", nil // not found, return empty string
	}
	if err != nil {
		return "", err
	}
	return pushName, nil
}

// LoadAllPushNames loads all push names into a map for fast lookup.
// This is used during batch processing like history sync.
func (s *MessageStore) LoadAllPushNames() (map[string]string, error) {
	rows, err := s.db.Query("SELECT jid, push_name FROM push_names")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pushNames := make(map[string]string)
	for rows.Next() {
		var jid, pushName string
		if err := rows.Scan(&jid, &pushName); err != nil {
			return nil, err
		}
		pushNames[jid] = pushName
	}

	return pushNames, rows.Err()
}
