package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// ErrFFmpegNotAvailable is returned when send_audio_message is invoked but
// ffmpeg is not installed on the host. The error message points the user at
// the install path or the send_file alternative for already-converted .ogg
// files.
var ErrFFmpegNotAvailable = errors.New(
	"ffmpeg is not installed or not in PATH; install ffmpeg to send voice notes, " +
		"or pre-convert your audio file to .ogg/.opus and use send_file instead",
)

// lookPath is a package-level indirection over exec.LookPath so tests can
// inject a deterministic stub. It is not exported.
var lookPath = exec.LookPath

// ffmpegState caches the result of the one-time detection done in detectFFmpeg.
type ffmpegState struct {
	once        sync.Once
	available   bool
	ffmpegPath  string
	ffprobePath string
}

var ffmpegInfo ffmpegState

// detectFFmpeg performs a one-shot probe for ffmpeg and ffprobe in PATH.
// Subsequent calls return the cached result.
//
// Both binaries normally ship together. ffmpeg is required for the actual
// conversion; ffprobe is optional and only used to populate the duration on
// the outgoing AudioMessage proto. If only ffmpeg is found the message is
// still sent successfully, just without a Seconds field.
func detectFFmpeg() (ffmpegPath, ffprobePath string, available bool) {
	ffmpegInfo.once.Do(func() {
		if p, err := lookPath("ffmpeg"); err == nil {
			ffmpegInfo.ffmpegPath = p
			ffmpegInfo.available = true
		}
		if p, err := lookPath("ffprobe"); err == nil {
			ffmpegInfo.ffprobePath = p
		}
	})
	return ffmpegInfo.ffmpegPath, ffmpegInfo.ffprobePath, ffmpegInfo.available
}

// resetFFmpegStateForTesting clears the cached detection so a test can
// re-run with a different lookPath. Not exported; only callers in this
// package's tests should use it.
func resetFFmpegStateForTesting() {
	ffmpegInfo = ffmpegState{}
}

// convertToOpusOgg invokes ffmpeg to transcode srcPath into a WhatsApp-
// compatible voice-note file (ogg container, libopus codec, mono, 16 kbps,
// 48 kHz) at a fresh temp path. The returned cleanup function removes the
// temp file and must be called by the caller (typically via defer) regardless
// of whether the send succeeds.
//
// Returns ErrFFmpegNotAvailable if ffmpeg cannot be found.
func convertToOpusOgg(ctx context.Context, srcPath string) (oggPath string, cleanup func(), err error) {
	ffmpeg, _, ok := detectFFmpeg()
	if !ok {
		return "", func() {}, ErrFFmpegNotAvailable
	}

	tmpFile, err := os.CreateTemp("", "wamcp-audio-*.ogg")
	if err != nil {
		return "", func() {}, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()

	cleanup = func() { _ = os.Remove(tmpPath) }

	// flags chosen to mirror what the WhatsApp client itself produces for PTT:
	//   -y                  overwrite the output file we just created
	//   -i src              input
	//   -vn                 drop any video/cover-art stream
	//   -ac 1               mono
	//   -ar 48000           48 kHz (Opus default sample rate)
	//   -c:a libopus        opus codec (only one libopus accepts in ogg container)
	//   -b:a 16k            16 kbps (matches WhatsApp's PTT bitrate)
	//   -application voip   tell libopus to prioritize voice intelligibility
	//   -f ogg              force ogg container regardless of output extension
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-y",
		"-i", srcPath,
		"-vn",
		"-ac", "1",
		"-ar", "48000",
		"-c:a", "libopus",
		"-b:a", "16k",
		"-application", "voip",
		"-f", "ogg",
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
		return "", func() {}, fmt.Errorf("ffmpeg produced empty output file")
	}

	return tmpPath, cleanup, nil
}

// probeAudioDurationSeconds queries ffprobe for the duration of audioPath and
// returns the value rounded to whole seconds. The duration is metadata-only,
// so when ffprobe is unavailable the function returns (0, nil) rather than an
// error -- the caller can still send the message without a duration.
func probeAudioDurationSeconds(ctx context.Context, audioPath string) (uint32, error) {
	_, ffprobe, _ := detectFFmpeg()
	if ffprobe == "" {
		return 0, nil
	}

	cmd := exec.CommandContext(ctx, ffprobe,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		audioPath,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe failed: %w", err)
	}

	return parseDurationSeconds(string(output))
}

// parseDurationSeconds converts the textual duration ffprobe writes (a
// floating-point number of seconds as ASCII, optionally surrounded by
// whitespace) into a uint32 second count, rounded to the nearest whole
// second. Negative or zero values yield (0, nil); a non-numeric input
// yields an error.
func parseDurationSeconds(raw string) (uint32, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, fmt.Errorf("empty duration")
	}

	sec, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", trimmed, err)
	}
	if sec <= 0 {
		return 0, nil
	}
	// round to nearest whole second
	return uint32(sec + 0.5), nil
}
