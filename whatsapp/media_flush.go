package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"whatsapp-mcp/paths"
	"whatsapp-mcp/storage"
)

// FlushFilter narrows which media rows are touched by FlushMediaCache.
// All fields are optional; an empty filter targets every downloaded file.
type FlushFilter struct {
	ChatJID    string    // limit to one chat (matches messages.chat_jid)
	MediaType  string    // image | video | audio | ptt | sticker | document — matches mime prefix
	BeforeDate time.Time // only files whose message timestamp is older than this
	DryRun     bool      // if true, count what would be removed without touching anything
	ResetState bool      // if true (default), set download_status='skipped' so EnsureMediaDownloaded can re-fetch
}

// FlushResult summarises a flush call.
type FlushResult struct {
	FilesRemoved   int   `json:"files_removed"`
	BytesFreed     int64 `json:"bytes_freed"`
	DBRowsUpdated  int   `json:"db_rows_updated"`
	DryRun         bool  `json:"dry_run"`
	SampleRemoved  []string `json:"sample_removed,omitempty"` // up to 10 paths for visibility
}

// FlushMediaCache deletes downloaded media files matching the filter and
// (unless DryRun) resets their download_status so they can be re-fetched
// on demand via EnsureMediaDownloaded.
//
// Metadata rows are preserved — only the on-disk artefact is removed and
// the row's file_path/download_status are reset. media_key/direct_path
// are kept so a subsequent EnsureMediaDownloaded can re-decrypt from the
// CDN if the URL has not yet expired.
func (c *Client) FlushMediaCache(ctx context.Context, filter FlushFilter) (*FlushResult, error) {
	if c.mediaStore == nil || c.store == nil {
		return nil, errors.New("client not wired with stores")
	}

	rows, err := c.listFlushCandidates(filter)
	if err != nil {
		return nil, fmt.Errorf("list candidates: %w", err)
	}

	result := &FlushResult{DryRun: filter.DryRun}
	resetState := filter.ResetState
	// default to true if caller didn't explicitly opt out via DryRun-only
	if !filter.DryRun && !resetState {
		// caller didn't set ResetState — default to true. Only honour
		// an explicit false when paired with a meaningful filter.
		resetState = true
	}

	for _, row := range rows {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		if row.FilePath == "" {
			continue
		}
		abs := paths.GetMediaPath(row.FilePath)
		stat, statErr := os.Stat(abs)
		if statErr != nil {
			// file already missing — still reset the DB row if requested
			if !filter.DryRun && resetState {
				if err := c.mediaStore.ResetDownloadState(row.MessageID); err == nil {
					result.DBRowsUpdated++
				}
			}
			continue
		}

		size := stat.Size()
		if filter.DryRun {
			result.FilesRemoved++
			result.BytesFreed += size
			if len(result.SampleRemoved) < 10 {
				result.SampleRemoved = append(result.SampleRemoved, row.FilePath)
			}
			continue
		}

		if err := os.Remove(abs); err != nil {
			c.log.Warnf("Flush: failed to remove %s: %v", abs, err)
			continue
		}
		result.FilesRemoved++
		result.BytesFreed += size
		if len(result.SampleRemoved) < 10 {
			result.SampleRemoved = append(result.SampleRemoved, row.FilePath)
		}

		if resetState {
			if err := c.mediaStore.ResetDownloadState(row.MessageID); err != nil {
				c.log.Warnf("Flush: failed to reset DB row for %s: %v", row.MessageID, err)
			} else {
				result.DBRowsUpdated++
			}
		}
	}

	return result, nil
}

// listFlushCandidates pulls media_metadata rows that should be considered
// for flushing, applying the filter at SQL level so we don't iterate the
// whole table in Go.
func (c *Client) listFlushCandidates(filter FlushFilter) ([]storage.MediaMetadata, error) {
	// Build query dynamically — keep it simple, no prepared statement reuse.
	var (
		clauses []string
		args    []any
	)
	clauses = append(clauses, "mm.download_status = 'downloaded'")
	clauses = append(clauses, "mm.file_path IS NOT NULL AND mm.file_path != ''")

	if filter.ChatJID != "" {
		clauses = append(clauses, "m.chat_jid = ?")
		args = append(args, filter.ChatJID)
	}
	if filter.MediaType != "" {
		mt := strings.ToLower(strings.TrimSpace(filter.MediaType))
		// special case: 'ptt' is a message_type, not a mime — voice notes are audio/ogg
		switch mt {
		case "ptt":
			clauses = append(clauses, "m.message_type = 'ptt'")
		case "image", "video", "audio", "sticker", "document":
			// match mime prefix; stickers come as image/webp so we match by message_type instead
			if mt == "sticker" {
				clauses = append(clauses, "m.message_type = 'sticker'")
			} else if mt == "document" {
				// documents use a wide range of mimes; match by message_type
				clauses = append(clauses, "m.message_type = 'document'")
			} else {
				clauses = append(clauses, "mm.mime_type LIKE ?")
				args = append(args, mt+"/%")
			}
		default:
			return nil, fmt.Errorf("unknown media_type %q (valid: image, video, audio, ptt, sticker, document)", filter.MediaType)
		}
	}
	if !filter.BeforeDate.IsZero() {
		clauses = append(clauses, "m.timestamp < ?")
		args = append(args, filter.BeforeDate.Unix())
	}

	query := `
		SELECT mm.message_id, mm.file_path, mm.file_name, mm.file_size, mm.mime_type,
		       mm.download_status
		FROM media_metadata mm
		JOIN messages m ON mm.message_id = m.id
		WHERE ` + strings.Join(clauses, " AND ") + `
		ORDER BY m.timestamp ASC
	`

	rows, err := c.mediaStore.QueryFlushCandidates(query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}
