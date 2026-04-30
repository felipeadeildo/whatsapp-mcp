package whatsapp

import (
	"errors"
	"os"
)

// ErrWhisperNotConfigured is returned when transcription is requested but the
// whisper-cli binary or the ggml model file pointed at by WhisperConfig is
// missing. The error message tells the user which env var to set.
var ErrWhisperNotConfigured = errors.New(
	"whisper.cpp is not configured: set WHISPER_BIN and WHISPER_MODEL to the " +
		"absolute paths of whisper-cli and your ggml-*.bin model (see README)",
)

// detectWhisper validates that the binary and model file referenced by cfg
// actually exist on disk. It does NOT execute the binary -- a successful
// return only means the paths resolve; the first real transcription call
// is what proves whisper itself works.
func detectWhisper(cfg WhisperConfig) (WhisperConfig, error) {
	if cfg.Bin == "" || cfg.Model == "" {
		return WhisperConfig{}, ErrWhisperNotConfigured
	}
	if _, err := os.Stat(cfg.Bin); err != nil {
		return WhisperConfig{}, ErrWhisperNotConfigured
	}
	if _, err := os.Stat(cfg.Model); err != nil {
		return WhisperConfig{}, ErrWhisperNotConfigured
	}
	if cfg.Threads <= 0 {
		cfg.Threads = 4
	}
	if cfg.Language == "" {
		cfg.Language = "pt"
	}
	return cfg, nil
}
