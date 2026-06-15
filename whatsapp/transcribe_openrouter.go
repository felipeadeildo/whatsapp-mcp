// whatsapp/transcribe_openrouter.go
package whatsapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const openRouterTranscribeURL = "https://openrouter.ai/api/v1/audio/transcriptions"

// ErrOpenRouterNotConfigured is returned when the OpenRouter backend is selected
// but no API key is available.
var errOpenRouterNoKey = fmt.Errorf("OpenRouter STT requested but OPENROUTER_API_KEY is empty")

// audioFormatFromPath maps a file extension to the OpenRouter `format` field.
// WhatsApp voice notes are Ogg/Opus, which the endpoint accepts directly (verified),
// so no ffmpeg transcode is needed on this path. Unknown extensions default to "ogg".
func audioFormatFromPath(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "ogg", "oga", "opus", "wav", "mp3", "m4a", "mp4", "webm", "flac", "aac":
		return ext
	case "":
		return "ogg"
	default:
		return ext
	}
}

// transcriptionRequest is the JSON body for POST /audio/transcriptions.
type transcriptionRequest struct {
	InputAudio inputAudio       `json:"input_audio"`
	Model      string           `json:"model"`
	Language   string           `json:"language,omitempty"`
	Provider   *providerControl `json:"provider,omitempty"`
}

type inputAudio struct {
	Data   string `json:"data"`   // base64
	Format string `json:"format"` // ogg, wav, mp3, ...
}

// providerControl carries privacy controls. data_collection:"deny" tells OpenRouter
// to only route to providers that will NOT train on / retain the audio — required
// because WhatsApp voice notes are private data.
type providerControl struct {
	DataCollection string `json:"data_collection"`
}

// buildTranscriptionBody assembles the request JSON. Pulled out as a pure function
// so it is unit-testable without any network call.
func buildTranscriptionBody(model, language, audioB64, format string) ([]byte, error) {
	if model == "" {
		return nil, fmt.Errorf("model is required")
	}
	req := transcriptionRequest{
		InputAudio: inputAudio{Data: audioB64, Format: format},
		Model:      model,
		Language:   language,
		Provider:   &providerControl{DataCollection: "deny"},
	}
	return json.Marshal(req)
}

// parseTranscriptionText extracts the transcript from a successful response body.
func parseTranscriptionText(body []byte) (string, error) {
	var resp struct {
		Text  string `json:"text"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("decode transcription response: %w (body: %s)", err, truncate(string(body), 200))
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return "", fmt.Errorf("openrouter error: %s", resp.Error.Message)
	}
	return strings.TrimSpace(resp.Text), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// transcribeOpenRouter reads audioPath, base64-encodes it, and POSTs it to the
// OpenRouter transcription endpoint. No disk temp / no ffmpeg: WhatsApp Ogg/Opus
// is sent as-is. Honors ctx for cancellation/timeout.
func transcribeOpenRouter(ctx context.Context, cfg TranscribeConfig, audioPath string) (string, error) {
	if cfg.OpenRouterKey == "" {
		return "", errOpenRouterNoKey
	}
	raw, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("read audio %s: %w", audioPath, err)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("audio file %s is empty", audioPath)
	}
	b64 := base64.StdEncoding.EncodeToString(raw)
	body, err := buildTranscriptionBody(cfg.Model, cfg.Language, b64, audioFormatFromPath(audioPath))
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterTranscribeURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+cfg.OpenRouterKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-OpenRouter-Title", "whatsapp-mcp")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("openrouter request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openrouter HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 300))
	}
	return parseTranscriptionText(respBody)
}
