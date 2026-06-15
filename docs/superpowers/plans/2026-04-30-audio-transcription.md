# WhatsApp Voice-Note Transcription Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `transcribe_audio_message` MCP tool that converts WhatsApp voice notes (and any other audio messages) into Brazilian-Portuguese text using a locally-hosted whisper.cpp `small` model — zero per-call cost, fully offline.

**Architecture:** A new `whatsapp/transcribe.go` module shells out to the `whisper-cli` binary after running ffmpeg to convert the source audio to whisper's required 16 kHz mono 16-bit PCM WAV. The module re-uses the existing `detectFFmpeg()` cache from `audio.go` and adds a sibling `detectWhisper()` cache. A new MCP tool looks up the message via `MessageStore.GetMessageByID`, resolves its on-disk path via `MediaStore.GetMediaMetadata` + `paths.GetMediaPath`, and returns the transcript. Configuration is environment-driven (`WHISPER_BIN`, `WHISPER_MODEL`, `WHISPER_LANGUAGE`, `WHISPER_THREADS`) following the same pattern as `MediaConfig`.

**Tech Stack:** Go 1.25, `mark3labs/mcp-go`, ffmpeg (already used for PTT send), [whisper.cpp](https://github.com/ggerganov/whisper.cpp) Windows release binaries, `ggml-small.bin` (multilingual, ~466 MB).

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `whatsapp/transcribe.go` | **Create** | Whisper detection cache, ffmpeg→WAV conversion, whisper-cli invocation, public `Transcribe(ctx, audioPath)` API. |
| `whatsapp/transcribe_test.go` | **Create** | Unit tests for whisper detection, argument assembly, error paths. Mirrors `audio_test.go` style (lookPath injection, no real binary calls). |
| `whatsapp/transcribe_config.go` | **Create** | `WhisperConfig` struct + `LoadWhisperConfig()`, mirroring `whatsapp/config.go`. |
| `whatsapp/client.go` | **Modify** | Add `whisperConfig WhisperConfig` field + accessor; load it in `NewClient`. |
| `mcp/tools.go` | **Modify** | Register the `transcribe_audio_message` tool (becomes tool #13). |
| `mcp/handlers.go` | **Modify** | Add `handleTranscribeAudioMessage` — looks up the message, validates audio media, calls `wa.TranscribeMessage`, returns text. |
| `whatsapp/client.go` | **Modify** | Add `TranscribeMessage(ctx, messageID) (string, error)` method (DB lookup + path resolution + delegate to `Transcribe`). |
| `README.md` | **Modify** | Add tool to the feature list and document `WHISPER_*` env vars. |

---

## Task 1: Install whisper.cpp and the small model (one-time, host-local)

**Files:** None (host setup only — no commits).

- [ ] **Step 1: Download whisper.cpp Windows release**

Run from PowerShell:

```powershell
$dest = "C:\Tools\whisper"
New-Item -ItemType Directory -Force -Path $dest | Out-Null
Invoke-WebRequest `
  -Uri "https://github.com/ggerganov/whisper.cpp/releases/latest/download/whisper-bin-x64.zip" `
  -OutFile "$dest\whisper.zip"
Expand-Archive -Force "$dest\whisper.zip" -DestinationPath $dest
Remove-Item "$dest\whisper.zip"
Get-ChildItem $dest
```

Expected: a folder containing `whisper-cli.exe` (and a few DLLs / `whisper-server.exe`). If the release ships nested under a subfolder, flatten it so `whisper-cli.exe` sits at `C:\Tools\whisper\whisper-cli.exe`.

- [ ] **Step 2: Download the multilingual `small` ggml model**

```powershell
Invoke-WebRequest `
  -Uri "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin" `
  -OutFile "C:\Tools\whisper\ggml-small.bin"
(Get-Item "C:\Tools\whisper\ggml-small.bin").Length / 1MB
```

Expected: file size around **466 MB** (±10 MB). If significantly smaller, the download was truncated — re-run.

- [ ] **Step 3: Smoke-test the binary against the model**

Generate a 3-second silent WAV and confirm whisper produces an empty / near-empty transcript without crashing:

```powershell
ffmpeg -y -f lavfi -i anullsrc=r=16000:cl=mono -t 3 -c:a pcm_s16le C:\Tools\whisper\silence.wav
& "C:\Tools\whisper\whisper-cli.exe" `
  -m "C:\Tools\whisper\ggml-small.bin" `
  -f "C:\Tools\whisper\silence.wav" `
  -l pt -nt --no-prints
```

Expected: exits 0; stdout shows whisper's banner plus an empty (or `[BLANK_AUDIO]`) transcript line. Any non-zero exit, or "failed to load model" / "invalid model file" → re-download model.

- [ ] **Step 4: Record paths in the project's `.env`**

Append (or create) `C:\Projects\whatsapp-mcp\.env`:

```env
WHISPER_BIN=C:\Tools\whisper\whisper-cli.exe
WHISPER_MODEL=C:\Tools\whisper\ggml-small.bin
WHISPER_LANGUAGE=pt
WHISPER_THREADS=4
```

No commit (the `.env` is git-ignored).

---

## Task 2: Add `WhisperConfig` env loader

**Files:**
- Create: `whatsapp/transcribe_config.go`
- Test: covered indirectly by Task 4's tests; no dedicated test file.

- [ ] **Step 1: Write the config file**

```go
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
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /c/Projects/whatsapp-mcp && go build ./whatsapp/...
```

Expected: no output, exit code 0.

- [ ] **Step 3: Commit**

```bash
cd /c/Projects/whatsapp-mcp
git add whatsapp/transcribe_config.go
git commit -m "feat(whatsapp): add WhisperConfig env loader"
```

---

## Task 3: Add whisper detection (TDD)

**Files:**
- Create: `whatsapp/transcribe.go` (initially: detection only)
- Create: `whatsapp/transcribe_test.go`

- [ ] **Step 1: Write the failing test**

```go
// whatsapp/transcribe_test.go
package whatsapp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectWhisperBinAndModel(t *testing.T) {
	t.Run("missing bin", func(t *testing.T) {
		cfg := WhisperConfig{Bin: "", Model: "anything"}
		_, err := detectWhisper(cfg)
		if !errors.Is(err, ErrWhisperNotConfigured) {
			t.Fatalf("want ErrWhisperNotConfigured, got %v", err)
		}
	})

	t.Run("bin does not exist", func(t *testing.T) {
		cfg := WhisperConfig{Bin: filepath.Join(t.TempDir(), "missing.exe"), Model: "x"}
		_, err := detectWhisper(cfg)
		if !errors.Is(err, ErrWhisperNotConfigured) {
			t.Fatalf("want ErrWhisperNotConfigured, got %v", err)
		}
	})

	t.Run("model does not exist", func(t *testing.T) {
		dir := t.TempDir()
		bin := filepath.Join(dir, "fake.exe")
		if err := os.WriteFile(bin, []byte("x"), 0755); err != nil {
			t.Fatal(err)
		}
		cfg := WhisperConfig{Bin: bin, Model: filepath.Join(dir, "missing.bin")}
		_, err := detectWhisper(cfg)
		if !errors.Is(err, ErrWhisperNotConfigured) {
			t.Fatalf("want ErrWhisperNotConfigured, got %v", err)
		}
	})

	t.Run("both present returns resolved paths", func(t *testing.T) {
		dir := t.TempDir()
		bin := filepath.Join(dir, "whisper-cli")
		model := filepath.Join(dir, "ggml.bin")
		for _, p := range []string{bin, model} {
			if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		cfg := WhisperConfig{Bin: bin, Model: model, Language: "pt", Threads: 2}
		got, err := detectWhisper(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Bin != bin || got.Model != model {
			t.Errorf("unexpected resolved paths: %+v", got)
		}
		if got.Language != "pt" || got.Threads != 2 {
			t.Errorf("unexpected language/threads: %+v", got)
		}
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd /c/Projects/whatsapp-mcp && go test ./whatsapp/ -run TestDetectWhisperBinAndModel -v
```

Expected: build failure with `undefined: detectWhisper` and `undefined: ErrWhisperNotConfigured`.

- [ ] **Step 3: Write minimal implementation**

```go
// whatsapp/transcribe.go
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
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd /c/Projects/whatsapp-mcp && go test ./whatsapp/ -run TestDetectWhisperBinAndModel -v
```

Expected: `PASS`, four sub-test cases all green.

- [ ] **Step 5: Commit**

```bash
cd /c/Projects/whatsapp-mcp
git add whatsapp/transcribe.go whatsapp/transcribe_test.go
git commit -m "feat(whatsapp): detect whisper.cpp bin and model"
```

---

## Task 4: Add ffmpeg→WAV conversion for whisper input (TDD)

**Files:**
- Modify: `whatsapp/transcribe.go`
- Modify: `whatsapp/transcribe_test.go`

Whisper.cpp expects **16 kHz, mono, 16-bit signed PCM in a WAV container**. We re-use the cached `detectFFmpeg()` from `audio.go`.

- [ ] **Step 1: Write the failing test**

Append to `whatsapp/transcribe_test.go`:

```go
func TestConvertToWhisperWAVReturnsErrWhenFFmpegMissing(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() {
		lookPath = origLookPath
		resetFFmpegStateForTesting()
	})
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	resetFFmpegStateForTesting()

	_, cleanup, err := convertToWhisperWAV(context.Background(), "anything.ogg")
	if cleanup != nil {
		cleanup()
	}
	if !errors.Is(err, ErrFFmpegNotAvailable) {
		t.Fatalf("want ErrFFmpegNotAvailable, got %v", err)
	}
}
```

(Add `"context"` to the test file's imports.)

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd /c/Projects/whatsapp-mcp && go test ./whatsapp/ -run TestConvertToWhisperWAV -v
```

Expected: build error `undefined: convertToWhisperWAV`.

- [ ] **Step 3: Write minimal implementation**

Append to `whatsapp/transcribe.go`:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

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
```

- [ ] **Step 4: Run all whatsapp tests to verify**

```bash
cd /c/Projects/whatsapp-mcp && go test ./whatsapp/ -v
```

Expected: every existing test still passes, the new `TestConvertToWhisperWAVReturnsErrWhenFFmpegMissing` passes. `go test` exit code 0.

- [ ] **Step 5: Commit**

```bash
cd /c/Projects/whatsapp-mcp
git add whatsapp/transcribe.go whatsapp/transcribe_test.go
git commit -m "feat(whatsapp): add ffmpeg WAV conversion for whisper input"
```

---

## Task 5: Add whisper-cli invocation and public `Transcribe` API (TDD)

**Files:**
- Modify: `whatsapp/transcribe.go`
- Modify: `whatsapp/transcribe_test.go`

We split the orchestration: `runWhisper(ctx, cfg, wavPath)` invokes the binary with `-otxt -of <prefix>` and returns the read text; `Transcribe(ctx, cfg, audioPath)` is the public entry point that orchestrates conversion + invocation. Argument assembly is unit-tested in isolation so we don't need a real binary in CI.

- [ ] **Step 1: Write the failing test**

Append to `whatsapp/transcribe_test.go`:

```go
func TestBuildWhisperArgs(t *testing.T) {
	cfg := WhisperConfig{
		Bin:      "/x/whisper-cli",
		Model:    "/x/ggml-small.bin",
		Language: "pt",
		Threads:  6,
	}
	got := buildWhisperArgs(cfg, "/tmp/in.wav", "/tmp/out")

	want := []string{
		"-m", "/x/ggml-small.bin",
		"-f", "/tmp/in.wav",
		"-l", "pt",
		"-t", "6",
		"-nt",
		"--no-prints",
		"-otxt",
		"-of", "/tmp/out",
	}
	if len(got) != len(want) {
		t.Fatalf("arg count: got %d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestTranscribeReturnsErrWhenWhisperMissing(t *testing.T) {
	cfg := WhisperConfig{Bin: "", Model: ""}
	_, err := Transcribe(context.Background(), cfg, "anything.ogg")
	if !errors.Is(err, ErrWhisperNotConfigured) {
		t.Fatalf("want ErrWhisperNotConfigured, got %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd /c/Projects/whatsapp-mcp && go test ./whatsapp/ -run "TestBuildWhisperArgs|TestTranscribeReturns" -v
```

Expected: build errors `undefined: buildWhisperArgs` and `undefined: Transcribe`.

- [ ] **Step 3: Write minimal implementation**

Append to `whatsapp/transcribe.go`:

```go
import "strconv"

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
```

- [ ] **Step 4: Run the new tests to verify they pass**

```bash
cd /c/Projects/whatsapp-mcp && go test ./whatsapp/ -run "TestBuildWhisperArgs|TestTranscribeReturns" -v
```

Expected: both tests `PASS`.

- [ ] **Step 5: Run the full whatsapp test suite**

```bash
cd /c/Projects/whatsapp-mcp && go test ./whatsapp/ -v
```

Expected: every test passes; nothing in the existing audio path regressed.

- [ ] **Step 6: Commit**

```bash
cd /c/Projects/whatsapp-mcp
git add whatsapp/transcribe.go whatsapp/transcribe_test.go
git commit -m "feat(whatsapp): add whisper-cli invocation and public Transcribe API"
```

---

## Task 6: Wire `WhisperConfig` into `Client` and add `TranscribeMessage` method

**Files:**
- Modify: `whatsapp/client.go`

The MCP handler will call `m.wa.TranscribeMessage(ctx, messageID)`. That method does the DB lookup → file path resolution → delegate to `Transcribe`. Keeping it on `*Client` follows the same pattern as `SendAudioMessage`, `SendFile`, etc.

- [ ] **Step 1: Locate the existing fields and constructor**

```bash
cd /c/Projects/whatsapp-mcp && grep -n "mediaConfig\|NewClient\b\|func NewClient" whatsapp/client.go
```

Expected: lines showing `mediaConfig MediaConfig` field and `func NewClient(...)`. Note the line numbers for the field block and the field initialisation in the constructor.

- [ ] **Step 2: Add `whisperConfig` field**

In `whatsapp/client.go`, add to the `Client` struct (next to `mediaConfig MediaConfig`):

```go
	whisperConfig WhisperConfig
```

- [ ] **Step 3: Initialise it in `NewClient`**

Wherever `mediaConfig: LoadMediaConfig(),` is set inside `NewClient`, add the parallel line below it:

```go
		whisperConfig: LoadWhisperConfig(),
```

- [ ] **Step 4: Add the `TranscribeMessage` method**

Append to `whatsapp/client.go` (or to a fitting section near `SendAudioMessage`):

```go
// TranscribeMessage transcribes the audio attached to messageID using the
// configured whisper.cpp pipeline. It looks up the message via the message
// store wired into the Client (same store the MCP server holds) and reads
// the already-downloaded media from disk. It returns an error if the
// message doesn't exist, isn't audio, or hasn't been downloaded yet.
func (c *Client) TranscribeMessage(ctx context.Context, messageID string) (string, error) {
	if c.messageStore == nil || c.mediaStore == nil {
		return "", fmt.Errorf("client not wired with stores")
	}
	msg, err := c.messageStore.GetMessageByID(messageID)
	if err != nil {
		return "", fmt.Errorf("lookup message %s: %w", messageID, err)
	}
	if msg == nil {
		return "", fmt.Errorf("message %s not found", messageID)
	}

	meta, err := c.mediaStore.GetMediaMetadata(messageID)
	if err != nil {
		return "", fmt.Errorf("lookup media for %s: %w", messageID, err)
	}
	if meta == nil {
		return "", fmt.Errorf("message %s has no media attachment", messageID)
	}
	if !strings.HasPrefix(meta.MimeType, "audio/") {
		return "", fmt.Errorf("message %s is not audio (mime=%s)", messageID, meta.MimeType)
	}
	if meta.DownloadStatus != "downloaded" || meta.FilePath == "" {
		return "", fmt.Errorf(
			"audio for message %s is not on disk (status=%s); wait for auto-download or re-fetch",
			messageID, meta.DownloadStatus,
		)
	}

	abs := paths.GetMediaPath(meta.FilePath)
	return Transcribe(ctx, c.whisperConfig, abs)
}
```

- [ ] **Step 5: Confirm imports and store-field names match the actual `client.go`**

```bash
cd /c/Projects/whatsapp-mcp && grep -nE "messageStore|mediaStore|MessageStore|MediaStore" whatsapp/client.go
```

If the fields are named differently (e.g. `store` instead of `messageStore`), update the method body and the import list (`"strings"`, `"whatsapp-mcp/paths"`) to match. Do not invent fields — use what the file already has.

- [ ] **Step 6: Build to verify no compilation errors**

```bash
cd /c/Projects/whatsapp-mcp && go build ./...
```

Expected: exit code 0, no output.

- [ ] **Step 7: Commit**

```bash
cd /c/Projects/whatsapp-mcp
git add whatsapp/client.go
git commit -m "feat(whatsapp): add TranscribeMessage on Client"
```

---

## Task 7: Register the `transcribe_audio_message` MCP tool

**Files:**
- Modify: `mcp/tools.go`

- [ ] **Step 1: Add the tool registration**

In `mcp/tools.go`, append a new block at the end of `registerTools()` (right before the closing brace of the function), after the existing tool 12:

```go
	// 13. transcribe an audio message (voice note or generic audio) to text
	m.server.AddTool(
		mcp.NewTool("transcribe_audio_message",
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithDescription("Transcribe a WhatsApp audio message (voice note or audio file) to text using a locally-hosted whisper.cpp model. The audio must already be downloaded (auto-download is on for audio by default). Default language is Brazilian Portuguese; configure via WHISPER_LANGUAGE."),
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("ID of the audio message to transcribe (from get_chat_messages or search_messages)"),
			),
		),
		m.handleTranscribeAudioMessage,
	)
```

- [ ] **Step 2: Build to verify the symbol resolves later**

```bash
cd /c/Projects/whatsapp-mcp && go build ./mcp/...
```

Expected: build error `undefined: m.handleTranscribeAudioMessage` — confirms the registration compiled and is now waiting on the handler in Task 8. (Don't commit yet; Task 8's handler completes the change.)

---

## Task 8: Implement the `handleTranscribeAudioMessage` handler

**Files:**
- Modify: `mcp/handlers.go`

- [ ] **Step 1: Append the handler**

At the end of `mcp/handlers.go`, add:

```go
// handleTranscribeAudioMessage handles the transcribe_audio_message tool request.
func (m *MCPServer) handleTranscribeAudioMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	messageID, err := request.RequireString("message_id")
	if err != nil {
		return mcp.NewToolResultError("message_id parameter is required"), nil
	}

	transcript, err := m.wa.TranscribeMessage(ctx, messageID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to transcribe %s: %v", messageID, err)), nil
	}

	if transcript == "" {
		return mcp.NewToolResultText(fmt.Sprintf("Message %s transcribed but produced no text (silent audio?)", messageID)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Transcript of message %s:\n\n%s", messageID, transcript)), nil
}
```

- [ ] **Step 2: Build the whole project**

```bash
cd /c/Projects/whatsapp-mcp && go build ./...
```

Expected: exit code 0.

- [ ] **Step 3: Run all tests**

```bash
cd /c/Projects/whatsapp-mcp && go test ./...
```

Expected: every package passes. No new test was added in this task because the handler is a thin wrapper around already-tested logic (Tasks 3-5 cover the whisper layer; the storage methods used by `TranscribeMessage` are already covered by the storage test suite).

- [ ] **Step 4: Commit**

```bash
cd /c/Projects/whatsapp-mcp
git add mcp/tools.go mcp/handlers.go
git commit -m "feat(mcp): expose transcribe_audio_message tool"
```

---

## Task 9: Build the production binary and smoke-test against a real voice note

**Files:** None (verification only).

- [ ] **Step 1: Build the executable**

```bash
cd /c/Projects/whatsapp-mcp && go build -o whatsapp-mcp.exe .
```

Expected: exit 0; `whatsapp-mcp.exe` mtime updated.

- [ ] **Step 2: Restart the MCP server via mcpproxy**

mcpproxy holds an old process handle, so a rebuild alone won't help. Restart the WhatsApp upstream:

```bash
curl -s -X POST "http://localhost:8085/api/v1/servers/whatsapp/restart?apikey=$(cat ~/.mcpproxy/api_key 2>/dev/null)" || \
  echo "If apikey lookup failed, restart mcpproxy itself: ~/bin/start-mcpproxy.bat"
```

(If the `whatsapp` server isn't yet wired into mcpproxy's roster — per `cluster-notes.md` it isn't as of 2026-04-30 — wire it first by adding it to `~/.mcpproxy/mcp_config.json` via the API, then `POST /restart`. That sub-task is out of scope for this plan; document it as a follow-up.)

- [ ] **Step 3: Pick a real audio message ID for the smoke test**

In Claude Code, with the mcpproxy roster loaded, ask the model to:

```
Use search_messages from='<your-own-jid>' limit=200, then list any messages whose
media type starts with audio/. Report the first message_id and chat_jid.
```

Note the `message_id`.

- [ ] **Step 4: Invoke transcribe_audio_message**

In Claude Code:

```
Use transcribe_audio_message with that message_id.
```

Expected outputs (any of):
- a Brazilian-Portuguese transcript that matches the spoken content;
- "Message X transcribed but produced no text (silent audio?)" — only valid for actually silent clips;
- a clear error mentioning `WHISPER_BIN`, `WHISPER_MODEL`, `ffmpeg`, or "not on disk" — those are the actionable failure modes; anything else is a bug to investigate.

- [ ] **Step 5: Sanity-check transcription quality on three different voice notes**

Run the same flow on three voice notes of varying length and noise level. Note the rough seconds-per-second-of-audio ratio.

Expected: small-q5_0 on this hardware should land somewhere between 1× and 3× realtime. If it's >5× realtime, drop `WHISPER_THREADS` to match physical cores or consider the `tiny` model.

- [ ] **Step 6: No commit needed**

(Smoke-test results live in this conversation, not the repo.)

---

## Task 10: Update README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Find where the existing tool list ends**

```bash
cd /c/Projects/whatsapp-mcp && grep -n "send_audio_message\|edit_message\|delete_message\|## " README.md | head -30
```

Note the line where the tool list ends and the section that documents environment variables.

- [ ] **Step 2: Add the new tool to the tool-list section**

Insert immediately after the `delete_message` entry (or at the end of the tool table — match whichever style the README already uses):

```markdown
- **transcribe_audio_message** — Transcribe a WhatsApp voice note or audio
  message to text using a locally-hosted whisper.cpp model. Default language
  is Brazilian Portuguese.
```

- [ ] **Step 3: Add the `WHISPER_*` env vars to the configuration section**

```markdown
### Audio transcription (optional)

| Variable | Default | Description |
|---|---|---|
| `WHISPER_BIN` | _(unset)_ | Absolute path to the `whisper-cli` (or `whisper-cli.exe`) binary from a [whisper.cpp](https://github.com/ggerganov/whisper.cpp) release. |
| `WHISPER_MODEL` | _(unset)_ | Absolute path to a `ggml-*.bin` model. The `small` multilingual model is recommended for CPU-only hosts (~466 MB). |
| `WHISPER_LANGUAGE` | `pt` | ISO 639-1 language code passed to whisper. |
| `WHISPER_THREADS` | `4` | CPU threads for whisper. Set to your physical core count. |

If `WHISPER_BIN` or `WHISPER_MODEL` is missing, `transcribe_audio_message`
returns a clear error pointing at this section. Other tools are unaffected.
```

- [ ] **Step 4: Commit**

```bash
cd /c/Projects/whatsapp-mcp
git add README.md
git commit -m "docs: document transcribe_audio_message and WHISPER_* env vars"
```

---

## Out of scope (deliberate cuts)

- **No transcript caching / persistence.** Re-running the tool re-transcribes. Add a `transcripts` table later if cost or latency becomes an issue.
- **No on-demand re-download of expired media.** If the voice note isn't on disk, the user gets a clear error and can wait for auto-download. Bridging to `whatsmeow.Download` from the MCP layer is a follow-up.
- **No streaming output.** Voice notes are short; batch is fine. If long-form audio shows up, switch to `--print-progress` and stream stderr.
- **No alternate-engine support (Vosk, faster-whisper, hosted APIs).** Single backend keeps the surface area small. Adding more is straightforward — extract a `Transcriber` interface only when there's a second concrete implementation.
- **No PT-BR finetuned ggml model.** Vanilla `small` with `-l pt` is the v1 baseline. If accuracy disappoints on a meaningful sample, swap the file at `WHISPER_MODEL` — no code changes.
