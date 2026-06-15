package whatsapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
)

func TestDetectOutboundMediaType(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		wantKind outboundMediaKind
		wantMime string
	}{
		// images
		{"jpg", "/tmp/foo.jpg", outboundMediaImage, "image/jpeg"},
		{"jpeg", "/tmp/foo.jpeg", outboundMediaImage, "image/jpeg"},
		{"png", "photo.png", outboundMediaImage, "image/png"},
		{"gif", "anim.gif", outboundMediaImage, "image/gif"},
		{"webp", "sticker.webp", outboundMediaImage, "image/webp"},
		{"uppercase ext is normalized", "PIC.JPG", outboundMediaImage, "image/jpeg"},
		{"mixed-case ext", "Photo.PnG", outboundMediaImage, "image/png"},

		// videos
		{"mp4", "clip.mp4", outboundMediaVideo, "video/mp4"},
		{"mov", "movie.mov", outboundMediaVideo, "video/quicktime"},
		{"webm", "stream.webm", outboundMediaVideo, "video/webm"},
		{"mkv", "movie.mkv", outboundMediaVideo, "video/x-matroska"},

		// audio
		{"ogg native", "note.ogg", outboundMediaAudio, "audio/ogg; codecs=opus"},
		{"opus alias", "note.opus", outboundMediaAudio, "audio/ogg; codecs=opus"},
		{"mp3 audio", "song.mp3", outboundMediaAudio, "audio/mpeg"},
		{"m4a audio", "podcast.m4a", outboundMediaAudio, "audio/mp4"},
		{"wav audio", "voice.wav", outboundMediaAudio, "audio/wav"},

		// documents
		{"pdf", "doc.pdf", outboundMediaDocument, "application/pdf"},
		{"docx", "report.docx", outboundMediaDocument, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"xlsx", "sheet.xlsx", outboundMediaDocument, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"zip", "archive.zip", outboundMediaDocument, "application/zip"},
		{"txt", "notes.txt", outboundMediaDocument, "text/plain"},

		// fallthrough cases
		{"unknown extension", "file.xyz", outboundMediaDocument, "application/octet-stream"},
		{"no extension", "README", outboundMediaDocument, "application/octet-stream"},
		{"trailing dot", "file.", outboundMediaDocument, "application/octet-stream"},
		{"compound .tar.gz uses last segment", "archive.tar.gz", outboundMediaDocument, "application/octet-stream"},
		{"empty string", "", outboundMediaDocument, "application/octet-stream"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotKind, gotMime := detectOutboundMediaType(tc.path)
			if gotKind != tc.wantKind {
				t.Errorf("kind: got %v, want %v", gotKind, tc.wantKind)
			}
			if gotMime != tc.wantMime {
				t.Errorf("mime: got %q, want %q", gotMime, tc.wantMime)
			}
		})
	}
}

func TestOutboundMediaKindMediaType(t *testing.T) {
	cases := []struct {
		kind outboundMediaKind
		want whatsmeow.MediaType
	}{
		{outboundMediaImage, whatsmeow.MediaImage},
		{outboundMediaVideo, whatsmeow.MediaVideo},
		{outboundMediaAudio, whatsmeow.MediaAudio},
		{outboundMediaDocument, whatsmeow.MediaDocument},
	}
	for _, tc := range cases {
		if got := tc.kind.MediaType(); got != tc.want {
			t.Errorf("kind=%v: got %v, want %v", tc.kind, got, tc.want)
		}
	}
}

func TestOutboundMediaKindString(t *testing.T) {
	cases := []struct {
		kind outboundMediaKind
		want string
	}{
		{outboundMediaImage, "image"},
		{outboundMediaVideo, "video"},
		{outboundMediaAudio, "audio"},
		{outboundMediaDocument, "document"},
	}
	for _, tc := range cases {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("kind=%v: got %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestValidateOutboundMediaPath(t *testing.T) {
	dir := t.TempDir()

	// regular file
	regPath := filepath.Join(dir, "ok.png")
	if err := os.WriteFile(regPath, []byte("fake png"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// nested file (used to test relative-with-traversal cases)
	nestedDir := filepath.Join(dir, "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	nestedFile := filepath.Join(nestedDir, "x.png")
	if err := os.WriteFile(nestedFile, []byte("fake"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	t.Run("valid regular file", func(t *testing.T) {
		got, err := validateOutboundMediaPath(regPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("expected absolute path, got %q", got)
		}
	})

	t.Run("empty path", func(t *testing.T) {
		_, err := validateOutboundMediaPath("")
		if err == nil {
			t.Error("expected error for empty path")
		}
	})

	t.Run("whitespace-only path", func(t *testing.T) {
		_, err := validateOutboundMediaPath("   ")
		if err == nil {
			t.Error("expected error for whitespace path")
		}
	})

	t.Run("path traversal rejected", func(t *testing.T) {
		_, err := validateOutboundMediaPath("../etc/passwd")
		if err == nil {
			t.Error("expected error for path traversal")
		}
		if err != nil && !strings.Contains(err.Error(), "traversal") {
			t.Errorf("expected traversal error, got: %v", err)
		}
	})

	t.Run("nested traversal rejected", func(t *testing.T) {
		_, err := validateOutboundMediaPath(filepath.Join(dir, "..", "..", "x"))
		if err == nil {
			t.Error("expected error for nested traversal")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := validateOutboundMediaPath(filepath.Join(dir, "ghost.png"))
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})

	t.Run("directory rejected", func(t *testing.T) {
		_, err := validateOutboundMediaPath(dir)
		if err == nil {
			t.Error("expected error when path is a directory")
		}
	})
}
