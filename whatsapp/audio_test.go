package whatsapp

import (
	"context"
	"errors"
	"testing"
)

func TestParseDurationSeconds(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    uint32
		wantErr bool
	}{
		{"integer seconds", "60", 60, false},
		{"fractional rounds up", "5.5", 6, false},
		{"fractional rounds down", "5.4", 5, false},
		{"zero", "0", 0, false},
		{"negative becomes zero", "-1.5", 0, false},
		{"large value", "3600.0", 3600, false},
		{"surrounding whitespace", "\n  12.3  \n", 12, false},
		{"non-numeric", "foo", 0, true},
		{"empty", "", 0, true},
		{"whitespace only", "   ", 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDurationSeconds(tc.raw)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil (value=%d)", got)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDetectFFmpegInjected(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() {
		lookPath = origLookPath
		resetFFmpegStateForTesting()
	})

	t.Run("ffmpeg unavailable", func(t *testing.T) {
		lookPath = func(name string) (string, error) {
			return "", errors.New("not found")
		}
		resetFFmpegStateForTesting()

		ffmpeg, ffprobe, available := detectFFmpeg()
		if available {
			t.Error("expected ffmpeg unavailable")
		}
		if ffmpeg != "" || ffprobe != "" {
			t.Errorf("expected empty paths, got ffmpeg=%q ffprobe=%q", ffmpeg, ffprobe)
		}
	})

	t.Run("both available", func(t *testing.T) {
		lookPath = func(name string) (string, error) {
			return "/usr/local/bin/" + name, nil
		}
		resetFFmpegStateForTesting()

		ffmpeg, ffprobe, available := detectFFmpeg()
		if !available {
			t.Error("expected available")
		}
		if ffmpeg != "/usr/local/bin/ffmpeg" {
			t.Errorf("ffmpeg path: got %q", ffmpeg)
		}
		if ffprobe != "/usr/local/bin/ffprobe" {
			t.Errorf("ffprobe path: got %q", ffprobe)
		}
	})

	t.Run("ffmpeg only, ffprobe missing", func(t *testing.T) {
		lookPath = func(name string) (string, error) {
			if name == "ffmpeg" {
				return "/opt/ffmpeg", nil
			}
			return "", errors.New("not found")
		}
		resetFFmpegStateForTesting()

		ffmpeg, ffprobe, available := detectFFmpeg()
		if !available {
			t.Error("expected ffmpeg available even without ffprobe")
		}
		if ffmpeg != "/opt/ffmpeg" {
			t.Errorf("ffmpeg path: got %q", ffmpeg)
		}
		if ffprobe != "" {
			t.Errorf("expected empty ffprobe path, got %q", ffprobe)
		}
	})

	t.Run("subsequent calls are cached", func(t *testing.T) {
		callCount := 0
		lookPath = func(name string) (string, error) {
			callCount++
			return "/x/" + name, nil
		}
		resetFFmpegStateForTesting()

		_, _, _ = detectFFmpeg()
		first := callCount
		_, _, _ = detectFFmpeg()
		_, _, _ = detectFFmpeg()
		if callCount != first {
			t.Errorf("expected detection to run once (got %d calls, first run was %d)", callCount, first)
		}
	})
}

func TestConvertToOpusOggReturnsErrWhenFFmpegMissing(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() {
		lookPath = origLookPath
		resetFFmpegStateForTesting()
	})

	lookPath = func(name string) (string, error) {
		return "", errors.New("not found")
	}
	resetFFmpegStateForTesting()

	_, cleanup, err := convertToOpusOgg(context.Background(), "anything.mp3")
	if cleanup != nil {
		cleanup()
	}
	if !errors.Is(err, ErrFFmpegNotAvailable) {
		t.Errorf("expected ErrFFmpegNotAvailable, got %v", err)
	}
}

func TestProbeAudioDurationReturnsZeroWhenFFprobeMissing(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() {
		lookPath = origLookPath
		resetFFmpegStateForTesting()
	})

	// ffmpeg present but ffprobe missing -- duration probing must degrade
	// gracefully so the message can still be sent.
	lookPath = func(name string) (string, error) {
		if name == "ffmpeg" {
			return "/opt/ffmpeg", nil
		}
		return "", errors.New("not found")
	}
	resetFFmpegStateForTesting()

	dur, err := probeAudioDurationSeconds(context.Background(), "anything.ogg")
	if err != nil {
		t.Errorf("expected no error when ffprobe is unavailable, got %v", err)
	}
	if dur != 0 {
		t.Errorf("expected 0 duration, got %d", dur)
	}
}
