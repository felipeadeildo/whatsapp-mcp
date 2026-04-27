package whatsapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow"
)

// outboundMediaKind classifies an outbound media payload.
// It maps to a whatsmeow.MediaType for the upload step and selects the
// corresponding *Message proto field (image/video/audio/document) when
// building the message.
type outboundMediaKind int

const (
	outboundMediaImage outboundMediaKind = iota
	outboundMediaVideo
	outboundMediaAudio
	outboundMediaDocument
)

// MediaType returns the whatsmeow MediaType used by the Upload call for this kind.
func (k outboundMediaKind) MediaType() whatsmeow.MediaType {
	switch k {
	case outboundMediaImage:
		return whatsmeow.MediaImage
	case outboundMediaVideo:
		return whatsmeow.MediaVideo
	case outboundMediaAudio:
		return whatsmeow.MediaAudio
	default:
		return whatsmeow.MediaDocument
	}
}

// String returns a short human-readable label, matching the inbound media_type values
// already used by getMediaTypeFromMessage in handlers.go.
func (k outboundMediaKind) String() string {
	switch k {
	case outboundMediaImage:
		return "image"
	case outboundMediaVideo:
		return "video"
	case outboundMediaAudio:
		return "audio"
	default:
		return "document"
	}
}

// extensionMimeMap maps lower-case file extensions (with leading dot) to the
// outbound classification + MIME string.
//
// Detection is intentionally extension-based: WhatsApp respects the explicit
// Mimetype field on the message proto regardless of the underlying bytes, so
// what matters is consistency between the kind/proto field and the declared
// MIME. Anything not in this map falls through to a generic document with
// application/octet-stream, which receivers display as a downloadable file.
var extensionMimeMap = map[string]struct {
	Kind outboundMediaKind
	Mime string
}{
	// images
	".jpg":  {outboundMediaImage, "image/jpeg"},
	".jpeg": {outboundMediaImage, "image/jpeg"},
	".png":  {outboundMediaImage, "image/png"},
	".gif":  {outboundMediaImage, "image/gif"},
	".webp": {outboundMediaImage, "image/webp"},

	// videos
	".mp4":  {outboundMediaVideo, "video/mp4"},
	".mov":  {outboundMediaVideo, "video/quicktime"},
	".avi":  {outboundMediaVideo, "video/x-msvideo"},
	".webm": {outboundMediaVideo, "video/webm"},
	".3gp":  {outboundMediaVideo, "video/3gpp"},
	".mkv":  {outboundMediaVideo, "video/x-matroska"},

	// audio
	//
	// .ogg and .opus are sent as-is (WhatsApp's native voice format).
	// .mp3/.m4a/.aac/.wav are accepted by send_file as regular audio (not voice
	// notes); the dedicated send_audio_message tool converts these into ogg-opus
	// before uploading so they appear as voice notes (PTT) on the receiver.
	".ogg":  {outboundMediaAudio, "audio/ogg; codecs=opus"},
	".opus": {outboundMediaAudio, "audio/ogg; codecs=opus"},
	".m4a":  {outboundMediaAudio, "audio/mp4"},
	".aac":  {outboundMediaAudio, "audio/aac"},
	".mp3":  {outboundMediaAudio, "audio/mpeg"},
	".wav":  {outboundMediaAudio, "audio/wav"},

	// documents (a small allowlist of well-known types; everything else gets
	// application/octet-stream which is the safe default)
	".pdf":  {outboundMediaDocument, "application/pdf"},
	".doc":  {outboundMediaDocument, "application/msword"},
	".docx": {outboundMediaDocument, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
	".xls":  {outboundMediaDocument, "application/vnd.ms-excel"},
	".xlsx": {outboundMediaDocument, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
	".ppt":  {outboundMediaDocument, "application/vnd.ms-powerpoint"},
	".pptx": {outboundMediaDocument, "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
	".zip":  {outboundMediaDocument, "application/zip"},
	".txt":  {outboundMediaDocument, "text/plain"},
	".csv":  {outboundMediaDocument, "text/csv"},
	".json": {outboundMediaDocument, "application/json"},
}

// detectOutboundMediaType classifies the file at path and returns a suitable
// MIME type for the WhatsApp message proto. Unknown extensions are treated as
// generic documents with application/octet-stream.
func detectOutboundMediaType(path string) (outboundMediaKind, string) {
	ext := strings.ToLower(filepath.Ext(path))
	if entry, ok := extensionMimeMap[ext]; ok {
		return entry.Kind, entry.Mime
	}
	return outboundMediaDocument, "application/octet-stream"
}

// validateOutboundMediaPath verifies that path points to a readable regular file
// and returns its cleaned absolute form. Empty paths, traversal attempts (any
// ".." segment after Clean), directories and non-regular files (sockets, named
// pipes) are rejected.
//
// The returned absolute path is safe to pass to os.ReadFile.
func validateOutboundMediaPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("media path is empty")
	}

	cleaned := filepath.Clean(path)

	// after cleaning, any remaining ".." indicates the user tried to escape upward
	for _, part := range strings.Split(cleaned, string(filepath.Separator)) {
		if part == ".." {
			return "", fmt.Errorf("path traversal not allowed: %q", path)
		}
	}

	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("invalid path %q: %w", path, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file does not exist: %s", abs)
		}
		return "", fmt.Errorf("cannot access %s: %w", abs, err)
	}

	if info.IsDir() {
		return "", fmt.Errorf("path is a directory, not a file: %s", abs)
	}

	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path is not a regular file: %s", abs)
	}

	return abs, nil
}
