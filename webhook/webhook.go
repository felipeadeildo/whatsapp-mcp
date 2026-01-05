package webhook

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"whatsapp-mcp/storage"

	"github.com/google/uuid"
)

// WebhookPayload represents the JSON structure sent to webhook endpoints.
type WebhookPayload struct {
	ID        string           `json:"id"`         // Event UUID
	EventType string           `json:"event_type"` // "message.received" or "message.sent"
	Timestamp time.Time        `json:"timestamp"`
	Data      MessageEventData `json:"data"`
}

// MessageEventData contains the message event details.
type MessageEventData struct {
	MessageID         string          `json:"message_id"`
	ChatJID           string          `json:"chat_jid"`
	SenderJID         string          `json:"sender_jid"`
	Text              string          `json:"text"`
	Timestamp         time.Time       `json:"timestamp"`
	IsFromMe          bool            `json:"is_from_me"`
	MessageType       string          `json:"message_type"`
	ChatName          string          `json:"chat_name,omitempty"`
	SenderPushName    string          `json:"sender_push_name,omitempty"`
	SenderContactName string          `json:"sender_contact_name,omitempty"`
	IsGroup           bool            `json:"is_group"`
	MediaMetadata     *MediaReference `json:"media_metadata,omitempty"`
}

// MediaReference contains metadata about media attachments.
type MediaReference struct {
	MessageID string `json:"message_id"` // Reference for API fetch
	FileName  string `json:"file_name"`
	FileSize  int64  `json:"file_size"`
	MimeType  string `json:"mime_type"`
	HasMedia  bool   `json:"has_media"`
}

// deliveryTask represents a webhook delivery job.
type deliveryTask struct {
	webhook storage.WebhookRegistration
	payload WebhookPayload
	attempt int
}

// Logger defines the logging interface for the webhook manager.
type Logger interface {
	Printf(format string, v ...any)
	Println(v ...any)
}

// WebhookManager manages webhook deliveries with retry logic.
type WebhookManager struct {
	store        *storage.WebhookStore
	config       *Config
	deliveryChan chan *deliveryTask
	httpClient   *http.Client
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	log          Logger
}

// NewWebhookManager creates a new webhook manager.
func NewWebhookManager(store *storage.WebhookStore, config *Config, logger Logger) *WebhookManager {
	ctx, cancel := context.WithCancel(context.Background())

	httpClient := &http.Client{
		Timeout: config.DeliveryTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	return &WebhookManager{
		store:        store,
		config:       config,
		deliveryChan: make(chan *deliveryTask, config.ChannelBufferSize),
		httpClient:   httpClient,
		ctx:          ctx,
		cancel:       cancel,
		log:          logger,
	}
}

// Start launches the webhook delivery workers.
func (m *WebhookManager) Start() {
	for i := 0; i < m.config.WorkerPoolSize; i++ {
		m.wg.Add(1)
		go m.worker(i)
	}
	m.log.Printf("Started %d webhook delivery workers", m.config.WorkerPoolSize)
}

// Stop gracefully shuts down the webhook manager.
func (m *WebhookManager) Stop() {
	m.log.Println("Stopping webhook manager...")
	m.cancel() // Signal workers to stop

	// Close the delivery channel to signal no more tasks will be sent
	close(m.deliveryChan)

	// Wait for workers to finish current tasks (with timeout)
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.log.Println("All webhook workers stopped gracefully")
	case <-time.After(30 * time.Second):
		m.log.Println("Warning: Webhook workers did not stop within timeout")
	}
}

// EmitMessageEvent emits a message event to all registered webhooks.
func (m *WebhookManager) EmitMessageEvent(msg storage.MessageWithNames) error {
	webhooks, err := m.store.ListWebhooks(true) // active only
	if err != nil {
		return err
	}

	payload := m.buildMessagePayload(msg)

	for _, webhook := range webhooks {
		// Filter by event types
		if !contains(webhook.EventTypes, "message") {
			continue
		}

		// Enqueue delivery task (non-blocking)
		task := &deliveryTask{
			webhook: webhook,
			payload: payload,
			attempt: 1,
		}

		select {
		case m.deliveryChan <- task:
			// Enqueued successfully
		default:
			// Channel full - log warning but don't block message processing
			m.log.Printf("Warning: Webhook delivery queue full, dropping event for webhook %s", webhook.ID)
		}
	}

	return nil
}

// buildMessagePayload converts a storage message to a webhook payload.
func (m *WebhookManager) buildMessagePayload(msg storage.MessageWithNames) WebhookPayload {
	eventType := "message.received"
	if msg.IsFromMe {
		eventType = "message.sent"
	}

	data := MessageEventData{
		MessageID:         msg.ID,
		ChatJID:           msg.ChatJID,
		SenderJID:         msg.SenderJID,
		Text:              msg.Text,
		Timestamp:         msg.Timestamp,
		IsFromMe:          msg.IsFromMe,
		MessageType:       msg.MessageType,
		ChatName:          msg.ChatName,
		SenderPushName:    msg.SenderPushName,
		SenderContactName: msg.SenderContactName,
		IsGroup:           strings.Contains(msg.ChatJID, "@g.us"),
	}

	// Add media metadata if present
	if msg.MediaMetadata != nil {
		data.MediaMetadata = &MediaReference{
			MessageID: msg.MediaMetadata.MessageID,
			FileName:  msg.MediaMetadata.FileName,
			FileSize:  msg.MediaMetadata.FileSize,
			MimeType:  msg.MediaMetadata.MimeType,
			HasMedia:  msg.MediaMetadata.FilePath != "",
		}
	}

	return WebhookPayload{
		ID:        uuid.New().String(),
		EventType: eventType,
		Timestamp: time.Now(),
		Data:      data,
	}
}

// worker processes delivery tasks from the queue.
func (m *WebhookManager) worker(id int) {
	defer m.wg.Done()

	m.log.Printf("Worker %d started", id)

	for {
		select {
		case task := <-m.deliveryChan:
			m.log.Printf("Worker %d processing webhook %s", id, task.webhook.ID)
			if err := m.deliverWebhook(task.webhook, task.payload, task.attempt); err != nil {
				// Schedule retry if attempts remain and backoff configuration is available
				if task.attempt < m.config.MaxRetries && task.attempt < len(m.config.RetryBackoff) {
					backoff := m.config.RetryBackoff[task.attempt]
					task.attempt++

					// Schedule the retry without blocking this worker goroutine.
					go func(t *deliveryTask, delay time.Duration) {
						select {
						case <-time.After(delay):
							// Only enqueue if context is still active.
							select {
							case m.deliveryChan <- t:
								// retry enqueued
							case <-m.ctx.Done():
								// context canceled; stop processing
							}
						case <-m.ctx.Done():
							// context canceled during backoff
						}
					}(task, backoff)
				}
			}
		case <-m.ctx.Done():
			return
		}
	}
}

// contains checks if a slice contains a specific string.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// TestDelivery sends a test webhook payload for manual testing purposes.
// This is a synchronous operation that bypasses the worker queue.
func (m *WebhookManager) TestDelivery(webhook storage.WebhookRegistration, payload WebhookPayload) error {
	return m.deliverWebhook(webhook, payload, 1)
}
