package whatsapp

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"whatsapp-mcp/storage"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
)

// determines initial download status based on auto-download config
func (c *Client) getInitialDownloadStatus(mediaType string, fileSize int64, fromHistory bool) string {
	// check if auto-download is enabled based on source
	if fromHistory && !c.mediaConfig.AutoDownloadFromHistory {
		return "skipped"
	}

	if c.shouldAutoDownload(mediaType, fileSize) {
		return "pending"
	}
	return "skipped"
}

// extracts metadata from WhatsApp message
func (c *Client) extractMediaMetadata(msg *waE2E.Message, messageID string, fromHistory bool) *storage.MediaMetadata {
	if msg == nil {
		return nil
	}

	// sticker message
	if sticker := msg.GetStickerMessage(); sticker != nil {
		fileName := fmt.Sprintf("sticker_%s.webp", messageID[:8])
		fileSize := int64(sticker.GetFileLength())

		return &storage.MediaMetadata{
			MessageID:      messageID,
			FileName:       fileName,
			FileSize:       fileSize,
			MimeType:       sticker.GetMimetype(),
			Width:          intPtr(int(sticker.GetWidth())),
			Height:         intPtr(int(sticker.GetHeight())),
			MediaKey:       sticker.GetMediaKey(),
			DirectPath:     sticker.GetDirectPath(),
			FileSHA256:     sticker.GetFileSHA256(),
			FileEncSHA256:  sticker.GetFileEncSHA256(),
			DownloadStatus: c.getInitialDownloadStatus("sticker", fileSize, fromHistory),
		}
	}

	// image message
	if img := msg.GetImageMessage(); img != nil {
		// * images don't have filename field, generate from message ID
		fileName := fmt.Sprintf("image_%s.jpg", messageID[:min(8, len(messageID))])
		fileSize := int64(img.GetFileLength())

		return &storage.MediaMetadata{
			MessageID:      messageID,
			FileName:       fileName,
			FileSize:       fileSize,
			MimeType:       img.GetMimetype(),
			Width:          intPtr(int(img.GetWidth())),
			Height:         intPtr(int(img.GetHeight())),
			MediaKey:       img.GetMediaKey(),
			DirectPath:     img.GetDirectPath(),
			FileSHA256:     img.GetFileSHA256(),
			FileEncSHA256:  img.GetFileEncSHA256(),
			DownloadStatus: c.getInitialDownloadStatus("image", fileSize, fromHistory),
		}
	}

	// video message
	if vid := msg.GetVideoMessage(); vid != nil {
		// * videos don't have filename field, generate from message ID
		fileName := fmt.Sprintf("video_%s.mp4", messageID[:min(8, len(messageID))])
		fileSize := int64(vid.GetFileLength())

		return &storage.MediaMetadata{
			MessageID:      messageID,
			FileName:       fileName,
			FileSize:       fileSize,
			MimeType:       vid.GetMimetype(),
			Width:          intPtr(int(vid.GetWidth())),
			Height:         intPtr(int(vid.GetHeight())),
			Duration:       intPtr(int(vid.GetSeconds())),
			MediaKey:       vid.GetMediaKey(),
			DirectPath:     vid.GetDirectPath(),
			FileSHA256:     vid.GetFileSHA256(),
			FileEncSHA256:  vid.GetFileEncSHA256(),
			DownloadStatus: c.getInitialDownloadStatus("video", fileSize, fromHistory),
		}
	}

	// audio message
	if aud := msg.GetAudioMessage(); aud != nil {
		fileName := "audio.ogg"
		mediaType := "audio"
		if aud.GetPTT() {
			fileName = "voice_note.ogg"
			mediaType = "ptt"
		}
		fileName = fmt.Sprintf("%s_%s", messageID[:8], fileName)
		fileSize := int64(aud.GetFileLength())

		return &storage.MediaMetadata{
			MessageID:      messageID,
			FileName:       fileName,
			FileSize:       fileSize,
			MimeType:       aud.GetMimetype(),
			Duration:       intPtr(int(aud.GetSeconds())),
			MediaKey:       aud.GetMediaKey(),
			DirectPath:     aud.GetDirectPath(),
			FileSHA256:     aud.GetFileSHA256(),
			FileEncSHA256:  aud.GetFileEncSHA256(),
			DownloadStatus: c.getInitialDownloadStatus(mediaType, fileSize, fromHistory),
		}
	}

	// document message
	if doc := msg.GetDocumentMessage(); doc != nil {
		fileName := doc.GetFileName()
		if fileName == "" {
			fileName = fmt.Sprintf("document_%s", messageID[:8])
			// try to add extension from MIME type
			if ext := mimeToExtension(doc.GetMimetype()); ext != "" {
				fileName += ext
			}
		}
		fileSize := int64(doc.GetFileLength())

		return &storage.MediaMetadata{
			MessageID:      messageID,
			FileName:       fileName,
			FileSize:       fileSize,
			MimeType:       doc.GetMimetype(),
			MediaKey:       doc.GetMediaKey(),
			DirectPath:     doc.GetDirectPath(),
			FileSHA256:     doc.GetFileSHA256(),
			FileEncSHA256:  doc.GetFileEncSHA256(),
			DownloadStatus: c.getInitialDownloadStatus("document", fileSize, fromHistory),
		}
	}

	return nil
}

// checks if media should be auto-downloaded
func (c *Client) shouldAutoDownload(mediaType string, fileSize int64) bool {
	if !c.mediaConfig.AutoDownloadEnabled {
		return false
	}

	// check type filter
	if !c.mediaConfig.AutoDownloadTypes[mediaType] {
		c.log.Debugf("Media type %s not in auto-download types", mediaType)
		return false
	}

	// check size filter (0 = unlimited)
	if c.mediaConfig.AutoDownloadMaxSize > 0 && fileSize > c.mediaConfig.AutoDownloadMaxSize {
		c.log.Debugf("Media size %d bytes exceeds max %d bytes", fileSize, c.mediaConfig.AutoDownloadMaxSize)
		return false
	}

	return true
}

// downloads media from WhatsApp and saves to disk
func (c *Client) downloadMedia(ctx context.Context, msg *waE2E.Message, meta *storage.MediaMetadata) error {
	if msg == nil || meta == nil {
		return fmt.Errorf("nil message or metadata")
	}

	// get the appropriate downloadable message
	var downloadable any

	if sticker := msg.GetStickerMessage(); sticker != nil {
		downloadable = sticker
	} else if img := msg.GetImageMessage(); img != nil {
		downloadable = img
	} else if vid := msg.GetVideoMessage(); vid != nil {
		downloadable = vid
	} else if aud := msg.GetAudioMessage(); aud != nil {
		downloadable = aud
	} else if doc := msg.GetDocumentMessage(); doc != nil {
		downloadable = doc
	} else {
		return fmt.Errorf("unsupported media type")
	}

	// generate unique file path
	filePath, err := c.generateMediaFilePath(meta)
	if err != nil {
		return fmt.Errorf("failed to generate file path: %w", err)
	}

	// create directory if needed
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// create file
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// download to file using whatsmeow's Download method
	var data []byte
	switch d := downloadable.(type) {
	case *waE2E.ImageMessage:
		data, err = c.wa.Download(ctx, d)
	case *waE2E.VideoMessage:
		data, err = c.wa.Download(ctx, d)
	case *waE2E.AudioMessage:
		data, err = c.wa.Download(ctx, d)
	case *waE2E.DocumentMessage:
		data, err = c.wa.Download(ctx, d)
	case *waE2E.StickerMessage:
		data, err = c.wa.Download(ctx, d)
	default:
		return fmt.Errorf("unknown downloadable type")
	}

	if err != nil {
		return err
	}

	// write data to file
	if _, err := file.Write(data); err != nil {
		os.Remove(filePath)
		return fmt.Errorf("failed to write file: %w", err)
	}

	// verify download
	if err := c.verifyDownload(filePath, meta); err != nil {
		os.Remove(filePath)
		return fmt.Errorf("verification failed: %w", err)
	}

	// update metadata with relative file path
	relPath, err := filepath.Rel(c.mediaConfig.StoragePath, filePath)
	if err != nil {
		os.Remove(filePath)
		return fmt.Errorf("failed to compute relative media path: %w", err)
	}
	meta.FilePath = relPath
	meta.DownloadStatus = "downloaded"
	now := time.Now()
	meta.DownloadTimestamp = &now

	c.log.Infof("Downloaded media %s to %s (%d bytes)", meta.MessageID, relPath, meta.FileSize)

	return nil
}

// creates unique file path based on metadata
func (c *Client) generateMediaFilePath(meta *storage.MediaMetadata) (string, error) {
	// determine subdirectory based on MIME type
	var subdir string
	switch {
	case strings.HasPrefix(meta.MimeType, "image/"):
		subdir = "images"
	case strings.HasPrefix(meta.MimeType, "video/"):
		subdir = "videos"
	case strings.HasPrefix(meta.MimeType, "audio/"):
		subdir = "audio"
	default:
		subdir = "documents"
	}

	// safe filename: {message_id}_{timestamp}_{sanitized_filename}
	timestamp := time.Now().Format("20060102_150405")
	safeName := sanitizeFilename(meta.FileName)
	if safeName == "" {
		// fallback: use extension from MIME type
		ext := mimeToExtension(meta.MimeType)
		safeName = fmt.Sprintf("media_%s%s", timestamp, ext)
	}

	// ensure filename starts with message ID for uniqueness
	fileName := fmt.Sprintf("%s_%s_%s", meta.MessageID[:min(8, len(meta.MessageID))], timestamp, safeName)

	return filepath.Join(c.mediaConfig.StoragePath, subdir, fileName), nil
}

// checks file integrity
// note: whatsmeow's Download() already validates HMAC, encrypted SHA256, and decrypted SHA256
// this function only performs basic sanity checks on the written file
func (c *Client) verifyDownload(filePath string, meta *storage.MediaMetadata) error {
	stat, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("file not accessible: %w", err)
	}

	// verify file has non-zero size
	if stat.Size() == 0 {
		return fmt.Errorf("downloaded file is empty")
	}

	// log size comparison for debugging (but don't fail on mismatch)
	// whatsmeow already validated the file, so size differences are just metadata inaccuracies
	if stat.Size() != meta.FileSize {
		c.log.Debugf("File size differs from metadata: wrote %d bytes, metadata claimed %d bytes",
			stat.Size(), meta.FileSize)
	}

	return nil
}

// downloads media with retry logic
func (c *Client) downloadMediaWithRetry(ctx context.Context, msg *waE2E.Message, meta *storage.MediaMetadata) error {
	maxRetries := 3
	backoff := time.Second
	var allErrors []string

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := c.downloadMedia(ctx, msg, meta)
		if err == nil {
			return nil
		}

		allErrors = append(allErrors, fmt.Sprintf("attempt %d: %v", attempt, err))

		// is error retryable?
		// 404/410 errors indicate expired/deleted media - don't retry
		if errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) ||
			errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410) {
			return err
		}

		c.log.Warnf("Download attempt %d/%d failed: %v", attempt, maxRetries, err)

		if attempt < maxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2 // backoff
			}
		}
	}

	// combine all errors into a single message
	return fmt.Errorf("download failed after %d attempts: %s", maxRetries, strings.Join(allErrors, "; "))
}

// helperssss
func intPtr(i int) *int {
	return &i
}

func sanitizeFilename(name string) string {
	if name == "" {
		return ""
	}

	// remove path separators and other unsafe characters
	var result []rune
	for _, r := range name {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			result = append(result, '_')
		} else if unicode.IsPrint(r) {
			result = append(result, r)
		}
	}

	filename := string(result)

	// length to 200 characters
	if len(filename) > 200 {
		// preserve extension
		ext := filepath.Ext(filename)
		base := filename[:200-len(ext)]
		filename = base + ext
	}

	return filename
}

func mimeToExtension(mime string) string {
	extensions := map[string]string{
		"image/jpeg":      ".jpg",
		"image/jpg":       ".jpg",
		"image/png":       ".png",
		"image/gif":       ".gif",
		"image/webp":      ".webp",
		"video/mp4":       ".mp4",
		"video/3gpp":      ".3gp",
		"video/quicktime": ".mov",
		"audio/ogg":       ".ogg",
		"audio/mpeg":      ".mp3",
		"audio/mp4":       ".m4a",
		"audio/aac":       ".aac",
		"application/pdf": ".pdf",
		"application/zip": ".zip",
		"text/plain":      ".txt",
	}

	if ext, ok := extensions[mime]; ok {
		return ext
	}

	// try to extract extension from MIME type
	parts := strings.Split(mime, "/")
	if len(parts) == 2 {
		return "." + parts[1]
	}

	return ""
}

func hashFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return nil, err
	}

	return hasher.Sum(nil), nil
}
