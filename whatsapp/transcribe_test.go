package whatsapp

import (
	"context"
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
