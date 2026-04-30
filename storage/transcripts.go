package storage

import (
	"database/sql"
	"errors"
)

// TranscriptStore caches audio-message transcriptions so repeated calls to
// transcribe_audio_message don't re-run whisper.cpp.
type TranscriptStore struct {
	db *sql.DB
}

// NewTranscriptStore creates a TranscriptStore over the given DB handle.
func NewTranscriptStore(db *sql.DB) *TranscriptStore {
	return &TranscriptStore{db: db}
}

// Get returns the cached transcript for messageID. The boolean is true when
// a row was found, false on cache miss; err is non-nil only on a real DB
// failure (a miss is not an error).
func (s *TranscriptStore) Get(messageID string) (string, bool, error) {
	var text string
	err := s.db.QueryRow(
		`SELECT text FROM transcripts WHERE message_id = ?`,
		messageID,
	).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return text, true, nil
}

// Save stores the transcript for messageID, overwriting any existing entry.
func (s *TranscriptStore) Save(messageID, text string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO transcripts (message_id, text) VALUES (?, ?)`,
		messageID, text,
	)
	return err
}
