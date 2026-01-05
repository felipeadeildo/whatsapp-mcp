package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"whatsapp-mcp/storage"
)

// deliverWebhook sends a webhook payload via HTTP POST with retry logic.
func (m *WebhookManager) deliverWebhook(webhook storage.WebhookRegistration, payload WebhookPayload, attempt int) error {
	m.log.Printf("Delivering webhook: webhook_id=%s payload_id=%s attempt=%d url=%s",
		webhook.ID, payload.ID, attempt, webhook.URL)

	// Serialize payload to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return m.recordFailure(webhook, payload, attempt, 0, fmt.Errorf("failed to marshal payload: %w", err))
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", webhook.URL, bytes.NewBuffer(jsonData))
	if err != nil {
		return m.recordFailure(webhook, payload, attempt, 0, fmt.Errorf("failed to create request: %w", err))
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "WhatsApp-MCP-Webhook/1.0")
	req.Header.Set("X-Webhook-ID", webhook.ID)
	req.Header.Set("X-Event-ID", payload.ID)

	// Calculate HMAC signature if secret is configured
	if webhook.Secret != "" {
		signature := calculateSignature(jsonData, webhook.Secret)
		req.Header.Set("X-Webhook-Signature", signature)
	}

	// Send request with timeout
	client := &http.Client{
		Timeout: m.config.DeliveryTimeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		return m.recordFailure(webhook, payload, attempt, 0, fmt.Errorf("request failed: %w", err))
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read response body only for error reporting (with 1MB limit to prevent memory exhaustion)
		limitedReader := io.LimitReader(resp.Body, 1024*1024)
		body, _ := io.ReadAll(limitedReader)
		return m.recordFailure(webhook, payload, attempt, resp.StatusCode,
			fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body)))
	}

	// Success!
	m.log.Printf("Webhook delivered successfully: webhook_id=%s payload_id=%s status=%d",
		webhook.ID, payload.ID, resp.StatusCode)

	// Record successful delivery
	deliveryAttempt := storage.DeliveryAttempt{
		WebhookID:     webhook.ID,
		PayloadID:     payload.ID,
		EventType:     payload.EventType,
		AttemptNumber: attempt,
		StatusCode:    resp.StatusCode,
		Success:       true,
		AttemptedAt:   time.Now(),
	}

	if err := m.store.RecordDelivery(deliveryAttempt); err != nil {
		m.log.Printf("Warning: Failed to record successful delivery: %v", err)
	}

	return nil
}

// recordFailure logs a failed delivery attempt.
func (m *WebhookManager) recordFailure(webhook storage.WebhookRegistration, payload WebhookPayload, attempt int, statusCode int, err error) error {
	m.log.Printf("Webhook delivery failed: webhook_id=%s payload_id=%s attempt=%d error=%v",
		webhook.ID, payload.ID, attempt, err)

	deliveryAttempt := storage.DeliveryAttempt{
		WebhookID:     webhook.ID,
		PayloadID:     payload.ID,
		EventType:     payload.EventType,
		AttemptNumber: attempt,
		StatusCode:    statusCode,
		Success:       false,
		Error:         err.Error(),
		AttemptedAt:   time.Now(),
	}

	if dbErr := m.store.RecordDelivery(deliveryAttempt); dbErr != nil {
		m.log.Printf("Warning: Failed to record delivery failure: %v", dbErr)
	}

	return err
}

// calculateSignature computes HMAC-SHA256 signature for webhook authenticity.
func calculateSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
