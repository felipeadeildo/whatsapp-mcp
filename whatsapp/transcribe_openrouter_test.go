// whatsapp/transcribe_openrouter_test.go
package whatsapp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAudioFormatFromPath(t *testing.T) {
	cases := map[string]string{
		"/x/a.ogg":               "ogg",
		"/x/a.OGG":               "ogg",
		"voice_note.opus":        "opus",
		"c:\\media\\x.oga":       "oga",
		"clip.wav":               "wav",
		"song.mp3":               "mp3",
		"v.m4a":                  "m4a",
		"noext":                  "ogg",
		"weird.xyz":              "xyz",
	}
	for in, want := range cases {
		if got := audioFormatFromPath(in); got != want {
			t.Errorf("audioFormatFromPath(%q)=%q want %q", in, got, want)
		}
	}
}

func TestBuildTranscriptionBody(t *testing.T) {
	body, err := buildTranscriptionBody("openai/whisper-large-v3", "pt", "QUJD", "ogg")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	var got transcriptionRequest
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body not valid json: %v", err)
	}
	if got.Model != "openai/whisper-large-v3" || got.Language != "pt" {
		t.Errorf("model/lang wrong: %+v", got)
	}
	if got.InputAudio.Data != "QUJD" || got.InputAudio.Format != "ogg" {
		t.Errorf("input_audio wrong: %+v", got.InputAudio)
	}
	// privacy: data_collection MUST be deny (WhatsApp audio is private)
	if got.Provider == nil || got.Provider.DataCollection != "deny" {
		t.Errorf("expected provider.data_collection=deny, got %+v", got.Provider)
	}
}

func TestBuildTranscriptionBodyRequiresModel(t *testing.T) {
	if _, err := buildTranscriptionBody("", "pt", "QUJD", "ogg"); err == nil {
		t.Fatal("expected error when model empty")
	}
}

func TestParseTranscriptionText(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		got, err := parseTranscriptionText([]byte(`{"text":"  olá mundo  ","usage":{"cost":0.0001}}`))
		if err != nil {
			t.Fatal(err)
		}
		if got != "olá mundo" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("error field", func(t *testing.T) {
		_, err := parseTranscriptionText([]byte(`{"error":{"message":"bad model"}}`))
		if err == nil || !strings.Contains(err.Error(), "bad model") {
			t.Fatalf("want error mentioning 'bad model', got %v", err)
		}
	})
	t.Run("garbage", func(t *testing.T) {
		if _, err := parseTranscriptionText([]byte(`not json`)); err == nil {
			t.Fatal("expected decode error")
		}
	})
}

func TestBackendSelection(t *testing.T) {
	cases := []struct {
		name       string
		cfg        TranscribeConfig
		useOR      bool
		localOK    bool
	}{
		{"auto+key", TranscribeConfig{Backend: "auto", OpenRouterKey: "k"}, true, true},
		{"auto+nokey", TranscribeConfig{Backend: "auto"}, false, true},
		{"openrouter forced", TranscribeConfig{Backend: "openrouter", OpenRouterKey: "k"}, true, false},
		{"local forced", TranscribeConfig{Backend: "local", OpenRouterKey: "k"}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.cfg.UseOpenRouter() != c.useOR {
				t.Errorf("UseOpenRouter=%v want %v", c.cfg.UseOpenRouter(), c.useOR)
			}
			if c.cfg.LocalAllowed() != c.localOK {
				t.Errorf("LocalAllowed=%v want %v", c.cfg.LocalAllowed(), c.localOK)
			}
		})
	}
}

func TestTranscribeOpenRouterNoKey(t *testing.T) {
	_, err := transcribeOpenRouter(t.Context(), TranscribeConfig{Model: "m"}, "x.ogg")
	if err == nil {
		t.Fatal("expected error without key")
	}
}
