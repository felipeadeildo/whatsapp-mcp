package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

// convertToWhisperWAV transcodes srcPath to a 16 kHz / mono / 16-bit PCM WAV
// at a fresh temp path -- the format whisper.cpp accepts as input. The
// returned cleanup removes the temp file and must be called by the caller
// (typically via defer) regardless of whether transcription succeeds.
//
// Returns ErrFFmpegNotAvailable if ffmpeg cannot be found.
func convertToWhisperWAV(ctx context.Context, srcPath string) (wavPath string, cleanup func(), err error) {
	ffmpeg, _, ok := detectFFmpeg()
	if !ok {
		return "", func() {}, ErrFFmpegNotAvailable
	}

	tmpFile, err := os.CreateTemp("", "wamcp-whisper-*.wav")
	if err != nil {
		return "", func() {}, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()

	cleanup = func() { _ = os.Remove(tmpPath) }

	// flags chosen to match whisper.cpp's expected input format:
	//   -y                  overwrite the output file we just created
	//   -i src              input
	//   -vn                 drop any video / cover-art stream
	//   -ac 1               mono
	//   -ar 16000           16 kHz (whisper's native sample rate)
	//   -c:a pcm_s16le      16-bit signed little-endian PCM
	//   -f wav              force WAV container
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-y",
		"-i", srcPath,
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		"-c:a", "pcm_s16le",
		"-f", "wav",
		tmpPath,
	)

	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		cleanup()
		return "", func() {}, fmt.Errorf(
			"ffmpeg conversion failed: %w (output: %s)",
			runErr, strings.TrimSpace(string(output)),
		)
	}

	info, statErr := os.Stat(tmpPath)
	if statErr != nil || info.Size() == 0 {
		cleanup()
		return "", func() {}, fmt.Errorf("ffmpeg produced empty WAV output")
	}

	return tmpPath, cleanup, nil
}
