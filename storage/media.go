package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// represents media file metadata
type MediaMetadata struct {
	MessageID         string
	FilePath          string // relative path from data/media/ (empty if not downloaded)
	FileName          string
	FileSize          int64
	MimeType          string
	Width             *int
	Height            *int
	Duration          *int
	MediaKey          []byte
	DirectPath        string
	FileSHA256        []byte
	FileEncSHA256     []byte
	DownloadStatus    string // pending, downloaded, failed, expired
	DownloadTimestamp *time.Time
	DownloadError     string
	CreatedAt         time.Time
}

// handles media metadata operations
type MediaStore struct {
	db *sql.DB
}

// creates a new MediaStore
func NewMediaStore(db *sql.DB) *MediaStore {
	return &MediaStore{db: db}
}

// inserts or updates media metadata
func (s *MediaStore) SaveMediaMetadata(meta MediaMetadata) error {
	query := `
	INSERT OR REPLACE INTO media_metadata
	(message_id, file_path, file_name, file_size, mime_type, width, height, duration,
	 media_key, direct_path, file_sha256, file_enc_sha256, download_status, download_timestamp, download_error)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	var downloadTimestampUnix *int64
	if meta.DownloadTimestamp != nil {
		ts := meta.DownloadTimestamp.Unix()
		downloadTimestampUnix = &ts
	}

	_, err := s.db.Exec(
		query,
		meta.MessageID,
		meta.FilePath,
		meta.FileName,
		meta.FileSize,
		meta.MimeType,
		meta.Width,
		meta.Height,
		meta.Duration,
		meta.MediaKey,
		meta.DirectPath,
		meta.FileSHA256,
		meta.FileEncSHA256,
		meta.DownloadStatus,
		downloadTimestampUnix,
		meta.DownloadError,
	)

	return err
}

// retrieves metadata by message ID
func (s *MediaStore) GetMediaMetadata(messageID string) (*MediaMetadata, error) {
	query := `
	SELECT message_id, file_path, file_name, file_size, mime_type, width, height, duration,
	       media_key, direct_path, file_sha256, file_enc_sha256, download_status,
	       download_timestamp, download_error, created_at
	FROM media_metadata
	WHERE message_id = ?
	`

	var meta MediaMetadata
	var filePath sql.NullString
	var width, height, duration sql.NullInt64
	var mediaKey, fileSHA256, fileEncSHA256 []byte
	var directPath, downloadError sql.NullString
	var downloadTimestampUnix sql.NullInt64
	var createdAtStr string

	err := s.db.QueryRow(query, messageID).Scan(
		&meta.MessageID,
		&filePath,
		&meta.FileName,
		&meta.FileSize,
		&meta.MimeType,
		&width,
		&height,
		&duration,
		&mediaKey,
		&directPath,
		&fileSHA256,
		&fileEncSHA256,
		&meta.DownloadStatus,
		&downloadTimestampUnix,
		&downloadError,
		&createdAtStr,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get media metadata: %w", err)
	}

	// handle nullable fields
	if filePath.Valid {
		meta.FilePath = filePath.String
	}
	if width.Valid {
		w := int(width.Int64)
		meta.Width = &w
	}
	if height.Valid {
		h := int(height.Int64)
		meta.Height = &h
	}
	if duration.Valid {
		d := int(duration.Int64)
		meta.Duration = &d
	}
	if directPath.Valid {
		meta.DirectPath = directPath.String
	}
	if downloadError.Valid {
		meta.DownloadError = downloadError.String
	}
	if downloadTimestampUnix.Valid {
		ts := time.Unix(downloadTimestampUnix.Int64, 0)
		meta.DownloadTimestamp = &ts
	}

	meta.MediaKey = mediaKey
	meta.FileSHA256 = fileSHA256
	meta.FileEncSHA256 = fileEncSHA256

	// parse created_at
	meta.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)

	return &meta, nil
}

// updates download status and timestamp
func (s *MediaStore) UpdateDownloadStatus(messageID, status string, downloadErr error) error {
	query := `
	UPDATE media_metadata
	SET download_status = ?, download_timestamp = ?, download_error = ?
	WHERE message_id = ?
	`

	now := time.Now().Unix()
	var errMsg *string
	if downloadErr != nil {
		msg := downloadErr.Error()
		errMsg = &msg
	}

	_, err := s.db.Exec(query, status, now, errMsg, messageID)
	return err
}

// returns media filtered by MIME type prefix
func (s *MediaStore) ListMediaByType(mimeTypePrefix string, limit int) ([]MediaMetadata, error) {
	query := `
	SELECT message_id, file_path, file_name, file_size, mime_type, width, height, duration,
	       download_status, download_timestamp, download_error
	FROM media_metadata
	WHERE mime_type LIKE ?
	ORDER BY created_at DESC
	LIMIT ?
	`

	rows, err := s.db.Query(query, mimeTypePrefix+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MediaMetadata
	for rows.Next() {
		var meta MediaMetadata
		var filePath sql.NullString
		var width, height, duration sql.NullInt64
		var downloadTimestampUnix sql.NullInt64
		var downloadError sql.NullString

		err := rows.Scan(
			&meta.MessageID,
			&filePath,
			&meta.FileName,
			&meta.FileSize,
			&meta.MimeType,
			&width,
			&height,
			&duration,
			&meta.DownloadStatus,
			&downloadTimestampUnix,
			&downloadError,
		)
		if err != nil {
			return nil, err
		}

		// handle nullable fields
		if filePath.Valid {
			meta.FilePath = filePath.String
		}
		if width.Valid {
			w := int(width.Int64)
			meta.Width = &w
		}
		if height.Valid {
			h := int(height.Int64)
			meta.Height = &h
		}
		if duration.Valid {
			d := int(duration.Int64)
			meta.Duration = &d
		}
		if downloadTimestampUnix.Valid {
			ts := time.Unix(downloadTimestampUnix.Int64, 0)
			meta.DownloadTimestamp = &ts
		}
		if downloadError.Valid {
			meta.DownloadError = downloadError.String
		}

		results = append(results, meta)
	}

	return results, rows.Err()
}

// returns all media from a specific chat
func (s *MediaStore) GetMediaByChat(chatJID string, limit int) ([]MediaMetadata, error) {
	query := `
	SELECT m.message_id, m.file_path, m.file_name, m.file_size, m.mime_type,
	       m.width, m.height, m.duration, m.download_status, m.download_timestamp, m.download_error
	FROM media_metadata m
	JOIN messages msg ON m.message_id = msg.id
	WHERE msg.chat_jid = ?
	ORDER BY msg.timestamp DESC
	LIMIT ?
	`

	rows, err := s.db.Query(query, chatJID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MediaMetadata
	for rows.Next() {
		var meta MediaMetadata
		var filePath sql.NullString
		var width, height, duration sql.NullInt64
		var downloadTimestampUnix sql.NullInt64
		var downloadError sql.NullString

		err := rows.Scan(
			&meta.MessageID,
			&filePath,
			&meta.FileName,
			&meta.FileSize,
			&meta.MimeType,
			&width,
			&height,
			&duration,
			&meta.DownloadStatus,
			&downloadTimestampUnix,
			&downloadError,
		)
		if err != nil {
			return nil, err
		}

		// handle nullable fields
		if filePath.Valid {
			meta.FilePath = filePath.String
		}
		if width.Valid {
			w := int(width.Int64)
			meta.Width = &w
		}
		if height.Valid {
			h := int(height.Int64)
			meta.Height = &h
		}
		if duration.Valid {
			d := int(duration.Int64)
			meta.Duration = &d
		}
		if downloadTimestampUnix.Valid {
			ts := time.Unix(downloadTimestampUnix.Int64, 0)
			meta.DownloadTimestamp = &ts
		}
		if downloadError.Valid {
			meta.DownloadError = downloadError.String
		}

		results = append(results, meta)
	}

	return results, rows.Err()
}

// removes metadata (file deletion handled separately)
func (s *MediaStore) DeleteMediaMetadata(messageID string) error {
	query := `DELETE FROM media_metadata WHERE message_id = ?`
	_, err := s.db.Exec(query, messageID)
	return err
}