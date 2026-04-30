package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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

// buildWhisperArgs assembles the whisper-cli argv (excluding the binary
// itself). Pulled out into a pure function so it can be unit-tested without
// invoking ffmpeg or whisper. The order of flags mirrors what is documented
// in whisper.cpp's README so future readers can map them 1:1.
func buildWhisperArgs(cfg WhisperConfig, wavPath, outPrefix string) []string {
	return []string{
		"-m", cfg.Model,
		"-f", wavPath,
		"-l", cfg.Language,
		"-t", strconv.Itoa(cfg.Threads),
		"-nt",          // no timestamps in output
		"--no-prints",  // suppress whisper's banner / progress on stderr
		"-otxt",        // write a .txt sidecar
		"-of", outPrefix,
	}
}

// runWhisper invokes whisper-cli, waits for it to finish, and returns the
// content of the sidecar .txt that whisper writes at <outPrefix>.txt. The
// caller is responsible for cleaning up wavPath; runWhisper cleans up the
// .txt sidecar itself.
func runWhisper(ctx context.Context, cfg WhisperConfig, wavPath string) (string, error) {
	tmpPrefix, err := os.CreateTemp("", "wamcp-whisper-out-*")
	if err != nil {
		return "", fmt.Errorf("failed to create whisper output temp: %w", err)
	}
	prefixPath := tmpPrefix.Name()
	_ = tmpPrefix.Close()
	_ = os.Remove(prefixPath) // whisper recreates with .txt suffix; we just want a unique prefix
	txtPath := prefixPath + ".txt"
	defer os.Remove(txtPath)

	cmd := exec.CommandContext(ctx, cfg.Bin, buildWhisperArgs(cfg, wavPath, prefixPath)...)
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return "", fmt.Errorf(
			"whisper-cli failed: %w (output: %s)",
			runErr, strings.TrimSpace(string(output)),
		)
	}

	data, err := os.ReadFile(txtPath)
	if err != nil {
		return "", fmt.Errorf("whisper produced no transcript file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// Transcribe is the public entry point. It validates the whisper config,
// converts audioPath to whisper-compatible WAV via ffmpeg, runs whisper-cli,
// and returns the transcript text. Returns ErrWhisperNotConfigured or
// ErrFFmpegNotAvailable when the underlying tooling is missing.
func Transcribe(ctx context.Context, cfg WhisperConfig, audioPath string) (string, error) {
	resolved, err := detectWhisper(cfg)
	if err != nil {
		return "", err
	}

	wavPath, cleanup, err := convertToWhisperWAV(ctx, audioPath)
	if err != nil {
		return "", err
	}
	defer cleanup()

	return runWhisper(ctx, resolved, wavPath)
}
