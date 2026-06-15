// whatsapp/transcribe_backend.go
package whatsapp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"whatsapp-mcp/config"
)

// TranscribeConfig selects and configures the transcription backend.
//
// Backend selection (TRANSCRIBE_BACKEND):
//   - "auto" (default): use OpenRouter when a key is present, else local whisper.cpp.
//   - "openrouter": force OpenRouter (errors if no key).
//   - "local": force local whisper.cpp (ignores any OpenRouter key).
//
// OpenRouter is preferred because it offloads the CPU-bound whisper.cpp work to a
// remote STT endpoint that parallelizes naturally — fixing the burst bottleneck
// where many concurrent transcribe calls thrash a single CPU and time out.
type TranscribeConfig struct {
	Backend          string        // auto | openrouter | local
	OpenRouterKey    string        // OPENROUTER_API_KEY (secret; never logged/committed)
	Model            string        // OPENROUTER_STT_MODEL (default openai/whisper-large-v3)
	Language         string        // ISO-639-1, default "pt"
	BatchConcurrency int           // max concurrent OpenRouter calls in a batch
	Whisper          WhisperConfig // local fallback config
}

// LoadTranscribeConfig reads transcription settings from the environment.
// The OpenRouter key is read from OPENROUTER_API_KEY only — we deliberately do
// NOT read ~/.openrouter-api-key from disk here so the server's secret surface
// is the (gitignored) .env, not an implicit home-dir file.
func LoadTranscribeConfig() TranscribeConfig {
	return TranscribeConfig{
		Backend:          strings.ToLower(config.GetEnv("TRANSCRIBE_BACKEND", "auto")),
		OpenRouterKey:    strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")),
		Model:            config.GetEnv("OPENROUTER_STT_MODEL", "openai/whisper-large-v3"),
		Language:         config.GetEnv("WHISPER_LANGUAGE", "pt"),
		BatchConcurrency: clampInt(config.GetEnvInt("TRANSCRIBE_BATCH_CONCURRENCY", 8), 1, 32),
		Whisper:          LoadWhisperConfig(),
	}
}

// UseOpenRouter reports whether the OpenRouter backend should be attempted.
func (c TranscribeConfig) UseOpenRouter() bool {
	switch c.Backend {
	case "local":
		return false
	case "openrouter":
		return true
	default: // auto
		return c.OpenRouterKey != ""
	}
}

// LocalAllowed reports whether the local whisper.cpp fallback may be used.
// In "openrouter" mode we never fall back to local (explicit user intent).
func (c TranscribeConfig) LocalAllowed() bool {
	return c.Backend != "openrouter"
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// TranscribeWithBackend transcribes audioPath using the configured backend,
// returning the transcript and a label of the backend actually used.
// In "auto"/"openrouter" mode it tries OpenRouter first; in "auto" it falls back
// to local whisper.cpp on any OpenRouter failure. In "local" mode it goes
// straight to whisper.cpp.
func TranscribeWithBackend(ctx context.Context, cfg TranscribeConfig, audioPath string) (text string, backend string, err error) {
	if cfg.UseOpenRouter() {
		text, err = transcribeOpenRouter(ctx, cfg, audioPath)
		if err == nil {
			return text, "openrouter", nil
		}
		if !cfg.LocalAllowed() {
			return "", "openrouter", err
		}
		orErr := err
		text, err = Transcribe(ctx, cfg.Whisper, audioPath)
		if err != nil {
			return "", "local", fmt.Errorf("openrouter failed (%v); local fallback also failed: %w", orErr, err)
		}
		return text, "local(fallback)", nil
	}
	text, err = Transcribe(ctx, cfg.Whisper, audioPath)
	return text, "local", err
}
