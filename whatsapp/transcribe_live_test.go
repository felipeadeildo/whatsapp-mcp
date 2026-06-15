// whatsapp/transcribe_live_test.go
package whatsapp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLiveOpenRouterTranscribe exercises the REAL production path
// (TranscribeWithBackend -> transcribeOpenRouter -> HTTP) against a real on-disk
// WhatsApp .ogg voice note. Skipped unless OPENROUTER_API_KEY is set and a
// non-empty .ogg exists under ../data/media/audio. Costs a fraction of a cent.
func TestLiveOpenRouterTranscribe(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("OPENROUTER_API_KEY not set; skipping live network test")
	}
	matches, _ := filepath.Glob(filepath.Join("..", "data", "media", "audio", "*.ogg"))
	var path string
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.Size() > 0 {
			path = m
			break
		}
	}
	if path == "" {
		t.Skip("no non-empty .ogg under ../data/media/audio")
	}

	cfg := TranscribeConfig{
		Backend:       "openrouter",
		OpenRouterKey: key,
		Model:         "openai/whisper-large-v3",
		Language:      "pt",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	text, backend, err := TranscribeWithBackend(ctx, cfg, path)
	if err != nil {
		t.Fatalf("TranscribeWithBackend failed: %v", err)
	}
	if text == "" {
		t.Fatalf("got empty transcript for %s", filepath.Base(path))
	}
	t.Logf("OK backend=%s file=%s transcript[:80]=%.80q", backend, filepath.Base(path), text)
}
