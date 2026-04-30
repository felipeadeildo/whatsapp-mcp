// whatsapp/transcribe_config.go
package whatsapp

import "whatsapp-mcp/config"

// WhisperConfig holds settings for the local whisper.cpp transcription pipeline.
// Populated from environment variables (WHISPER_BIN, WHISPER_MODEL,
// WHISPER_LANGUAGE, WHISPER_THREADS). All fields are optional at load time;
// callers that try to transcribe without a usable Bin/Model will receive
// ErrWhisperNotConfigured at runtime, not at startup, so the rest of the
// server can still run for users who never invoke transcription.
type WhisperConfig struct {
	Bin      string // absolute path to whisper-cli executable
	Model    string // absolute path to ggml-*.bin model file
	Language string // ISO 639-1 code passed to whisper -l (default: "pt")
	Threads  int    // CPU threads passed to whisper -t (default: 4)
}

// LoadWhisperConfig reads WHISPER_* environment variables and returns a
// WhisperConfig. Missing variables produce zero values rather than errors --
// detection happens lazily in detectWhisper().
func LoadWhisperConfig() WhisperConfig {
	return WhisperConfig{
		Bin:      config.GetEnv("WHISPER_BIN", ""),
		Model:    config.GetEnv("WHISPER_MODEL", ""),
		Language: config.GetEnv("WHISPER_LANGUAGE", "pt"),
		Threads:  int(config.GetEnvInt64("WHISPER_THREADS", 4)),
	}
}
