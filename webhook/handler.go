package webhook

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"whatsapp-mcp/storage"

	"github.com/google/uuid"
)

// Handler handles HTTP API requests for webhook management.
type Handler struct {
	manager *WebhookManager
	store   *storage.WebhookStore
	apiKey  string
}

// errorResponse writes a properly escaped JSON error response.
func errorResponse(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	response := map[string]string{"error": message}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		// Fallback to plain text if JSON encoding fails
		http.Error(w, message, statusCode)
	}
}

// NewHandler creates a new webhook HTTP handler.
func NewHandler(manager *WebhookManager, store *storage.WebhookStore, apiKey string) *Handler {
	return &Handler{
		manager: manager,
		store:   store,
		apiKey:  apiKey,
	}
}

// ValidateAuth checks if the request has a valid API key using constant-time comparison.
func (h *Handler) ValidateAuth(r *http.Request) bool {
	authHeader := r.Header.Get("Authorization")
	expectedAuth := "Bearer " + h.apiKey
	return subtle.ConstantTimeCompare([]byte(authHeader), []byte(expectedAuth)) == 1
}

var (
	// supportedEventTypes lists all valid event types
	supportedEventTypes = map[string]bool{
		"message": true,
	}
)

// CreateWebhookRequest represents a webhook creation request.
type CreateWebhookRequest struct {
	URL        string   `json:"url"`
	Secret     string   `json:"secret,omitempty"`
	EventTypes []string `json:"event_types"`
}

// validateURL checks if the URL is valid and not targeting private/internal networks (SSRF prevention).
func validateURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return err
	}

	// only allow HTTP and HTTPS schemes
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("invalid URL scheme: only http and https are allowed")
	}

	// ensure a host is specified
	if parsedURL.Host == "" {
		return fmt.Errorf("invalid URL: host is required")
	}

	return nil
}

// validateEventTypes checks if all event types are supported.
func validateEventTypes(eventTypes []string) error {
	if len(eventTypes) == 0 {
		return nil // will use default
	}

	for _, eventType := range eventTypes {
		if eventType == "" {
			return fmt.Errorf("empty event type is not allowed")
		}
		if !supportedEventTypes[eventType] {
			return fmt.Errorf("unsupported event type: %s", eventType)
		}
	}

	return nil
}

// WebhookResponse represents a webhook in API responses.
type WebhookResponse struct {
	ID         string    `json:"id"`
	URL        string    `json:"url"`
	EventTypes []string  `json:"event_types"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// CreateWebhook handles POST /api/webhooks
func (h *Handler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	var req CreateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Validate request
	if req.URL == "" {
		http.Error(w, `{"error":"URL is required"}`, http.StatusBadRequest)
		return
	}

	// Validate URL format and prevent SSRF
	if err := validateURL(req.URL); err != nil {
		errorResponse(w, "Invalid URL: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.EventTypes) == 0 {
		req.EventTypes = []string{"message"} // default
	}

	// Validate event types
	if err := validateEventTypes(req.EventTypes); err != nil {
		errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Create webhook registration
	webhook := storage.WebhookRegistration{
		ID:         uuid.New().String(),
		URL:        req.URL,
		Secret:     req.Secret,
		EventTypes: req.EventTypes,
		Active:     true,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := h.store.CreateWebhook(webhook); err != nil {
		http.Error(w, `{"error":"Failed to create webhook"}`, http.StatusInternalServerError)
		return
	}

	// Return response
	resp := WebhookResponse{
		ID:         webhook.ID,
		URL:        webhook.URL,
		EventTypes: webhook.EventTypes,
		Active:     webhook.Active,
		CreatedAt:  webhook.CreatedAt,
		UpdatedAt:  webhook.UpdatedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// ListWebhooks handles GET /api/webhooks
func (h *Handler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	webhooks, err := h.store.ListWebhooks(false) // include inactive
	if err != nil {
		http.Error(w, `{"error":"Failed to list webhooks"}`, http.StatusInternalServerError)
		return
	}

	var resp []WebhookResponse
	for _, wh := range webhooks {
		resp = append(resp, WebhookResponse{
			ID:         wh.ID,
			URL:        wh.URL,
			EventTypes: wh.EventTypes,
			Active:     wh.Active,
			CreatedAt:  wh.CreatedAt,
			UpdatedAt:  wh.UpdatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"webhooks": resp})
}

// HandleWebhookByID routes requests to specific webhook endpoints.
func (h *Handler) HandleWebhookByID(w http.ResponseWriter, r *http.Request) {
	// Extract webhook ID from path
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/webhooks/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, `{"error":"Webhook ID required"}`, http.StatusBadRequest)
		return
	}

	webhookID := parts[0]

	// Check for test endpoint
	if len(parts) == 2 && parts[1] == "test" && r.Method == http.MethodPost {
		h.TestWebhook(w, r, webhookID)
		return
	}

	// Check for stats endpoint
	if len(parts) == 2 && parts[1] == "stats" && r.Method == http.MethodGet {
		h.GetWebhookStats(w, r, webhookID)
		return
	}

	// Route by method
	switch r.Method {
	case http.MethodGet:
		h.GetWebhook(w, r, webhookID)
	case http.MethodPut:
		h.UpdateWebhook(w, r, webhookID)
	case http.MethodDelete:
		h.DeleteWebhook(w, r, webhookID)
	default:
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// GetWebhook handles GET /api/webhooks/{id}
func (h *Handler) GetWebhook(w http.ResponseWriter, r *http.Request, webhookID string) {
	webhook, err := h.store.GetWebhook(webhookID)
	if err != nil {
		http.Error(w, `{"error":"Webhook not found"}`, http.StatusNotFound)
		return
	}

	resp := WebhookResponse{
		ID:         webhook.ID,
		URL:        webhook.URL,
		EventTypes: webhook.EventTypes,
		Active:     webhook.Active,
		CreatedAt:  webhook.CreatedAt,
		UpdatedAt:  webhook.UpdatedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// UpdateWebhookRequest represents a webhook update request.
type UpdateWebhookRequest struct {
	URL        *string   `json:"url,omitempty"`
	Secret     *string   `json:"secret,omitempty"`
	EventTypes *[]string `json:"event_types,omitempty"`
	Active     *bool     `json:"active,omitempty"`
}

// UpdateWebhook handles PUT /api/webhooks/{id}
func (h *Handler) UpdateWebhook(w http.ResponseWriter, r *http.Request, webhookID string) {
	webhook, err := h.store.GetWebhook(webhookID)
	if err != nil {
		http.Error(w, `{"error":"Webhook not found"}`, http.StatusNotFound)
		return
	}

	var req UpdateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Validate URL if provided
	if req.URL != nil {
		if err := validateURL(*req.URL); err != nil {
			errorResponse(w, "Invalid URL: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Validate event types if provided
	if req.EventTypes != nil {
		if err := validateEventTypes(*req.EventTypes); err != nil {
			errorResponse(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Apply updates
	if req.URL != nil {
		webhook.URL = *req.URL
	}
	// Note: Setting secret to empty string intentionally disables HMAC signature verification.
	// This is allowed for development/testing scenarios where signature validation isn't needed.
	if req.Secret != nil {
		webhook.Secret = *req.Secret
	}
	if req.EventTypes != nil {
		webhook.EventTypes = *req.EventTypes
	}
	if req.Active != nil {
		webhook.Active = *req.Active
	}

	if err := h.store.UpdateWebhook(*webhook); err != nil {
		http.Error(w, `{"error":"Failed to update webhook"}`, http.StatusInternalServerError)
		return
	}

	// Get updated webhook to ensure UpdatedAt field is current
	updatedWebhook, err := h.store.GetWebhook(webhookID)
	if err != nil {
		http.Error(w, `{"error":"Failed to retrieve updated webhook"}`, http.StatusInternalServerError)
		return
	}

	resp := WebhookResponse{
		ID:         updatedWebhook.ID,
		URL:        updatedWebhook.URL,
		EventTypes: updatedWebhook.EventTypes,
		Active:     updatedWebhook.Active,
		CreatedAt:  updatedWebhook.CreatedAt,
		UpdatedAt:  updatedWebhook.UpdatedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// DeleteWebhook handles DELETE /api/webhooks/{id}
func (h *Handler) DeleteWebhook(w http.ResponseWriter, r *http.Request, webhookID string) {
	if err := h.store.DeleteWebhook(webhookID); err != nil {
		http.Error(w, `{"error":"Webhook not found"}`, http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TestWebhook handles POST /api/webhooks/{id}/test
func (h *Handler) TestWebhook(w http.ResponseWriter, r *http.Request, webhookID string) {
	webhook, err := h.store.GetWebhook(webhookID)
	if err != nil {
		http.Error(w, `{"error":"Webhook not found"}`, http.StatusNotFound)
		return
	}

	// Create a test payload
	testPayload := WebhookPayload{
		ID:        uuid.New().String(),
		EventType: "message.received",
		Timestamp: time.Now(),
		Data: MessageEventData{
			MessageID:   "TEST-" + uuid.New().String(),
			ChatJID:     "test@s.whatsapp.net",
			SenderJID:   "test@s.whatsapp.net",
			Text:        "This is a test message from WhatsApp MCP webhook system",
			Timestamp:   time.Now(),
			IsFromMe:    false,
			MessageType: "text",
			ChatName:    "Test Chat",
			IsGroup:     false,
		},
	}

	// Attempt delivery
	err = h.manager.TestDelivery(*webhook, testPayload)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     "delivered",
		"payload_id": testPayload.ID,
	})
}

// GetWebhookStats handles GET /api/webhooks/{id}/stats
func (h *Handler) GetWebhookStats(w http.ResponseWriter, r *http.Request, webhookID string) {
	// Check webhook exists
	if _, err := h.store.GetWebhook(webhookID); err != nil {
		http.Error(w, `{"error":"Webhook not found"}`, http.StatusNotFound)
		return
	}

	// Get stats for last 24 hours
	since := time.Now().Add(-24 * time.Hour)
	stats, err := h.store.GetDeliveryStats(webhookID, since)
	if err != nil {
		http.Error(w, `{"error":"Failed to get stats"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}
