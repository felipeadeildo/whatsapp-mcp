package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// WebhookRegistration represents a registered webhook endpoint.
type WebhookRegistration struct {
	ID         string
	URL        string
	Secret     string   // HMAC signing secret
	EventTypes []string // ["message"]
	Active     bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// DeliveryAttempt represents a webhook delivery attempt.
type DeliveryAttempt struct {
	WebhookID     string
	PayloadID     string
	EventType     string
	AttemptNumber int
	StatusCode    int
	Success       bool
	Error         string
	AttemptedAt   time.Time
}

// DeliveryStats holds statistics about webhook deliveries.
type DeliveryStats struct {
	TotalDeliveries      int
	SuccessfulDeliveries int
	FailedDeliveries     int
	SuccessRate          float64
	LastDeliveryAt       *time.Time
	LastFailureAt        *time.Time
}

// WebhookStore handles database operations for webhook registrations.
type WebhookStore struct {
	db *sql.DB
}

// NewWebhookStore creates a new webhook store.
func NewWebhookStore(db *sql.DB) *WebhookStore {
	return &WebhookStore{db: db}
}

// CreateWebhook inserts a new webhook registration.
func (s *WebhookStore) CreateWebhook(reg WebhookRegistration) error {
	eventTypesJSON, err := json.Marshal(reg.EventTypes)
	if err != nil {
		return fmt.Errorf("failed to marshal event types: %w", err)
	}

	query := `
		INSERT INTO webhook_registrations (id, url, secret, event_types, active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.Exec(query,
		reg.ID,
		reg.URL,
		reg.Secret,
		string(eventTypesJSON),
		reg.Active,
		reg.CreatedAt.Unix(),
		reg.UpdatedAt.Unix(),
	)

	if err != nil {
		return fmt.Errorf("failed to create webhook: %w", err)
	}

	return nil
}

// UpsertWebhook inserts a new webhook or updates an existing one if the ID already exists.
func (s *WebhookStore) UpsertWebhook(reg WebhookRegistration) error {
	eventTypesJSON, err := json.Marshal(reg.EventTypes)
	if err != nil {
		return fmt.Errorf("failed to marshal event types: %w", err)
	}

	query := `
		INSERT INTO webhook_registrations (id, url, secret, event_types, active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			url = excluded.url,
			secret = excluded.secret,
			event_types = excluded.event_types,
			active = excluded.active,
			updated_at = excluded.updated_at
	`

	_, err = s.db.Exec(query,
		reg.ID,
		reg.URL,
		reg.Secret,
		string(eventTypesJSON),
		reg.Active,
		reg.CreatedAt.Unix(),
		reg.UpdatedAt.Unix(),
	)

	if err != nil {
		return fmt.Errorf("failed to upsert webhook: %w", err)
	}

	return nil
}

// GetWebhook retrieves a webhook by ID.
func (s *WebhookStore) GetWebhook(id string) (*WebhookRegistration, error) {
	query := `
		SELECT id, url, secret, event_types, active, created_at, updated_at
		FROM webhook_registrations
		WHERE id = ?
	`

	var reg WebhookRegistration
	var eventTypesJSON string
	var secret sql.NullString
	var createdAt, updatedAt int64

	err := s.db.QueryRow(query, id).Scan(
		&reg.ID,
		&reg.URL,
		&secret,
		&eventTypesJSON,
		&reg.Active,
		&createdAt,
		&updatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("webhook not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get webhook: %w", err)
	}

	if secret.Valid {
		reg.Secret = secret.String
	}

	if err := json.Unmarshal([]byte(eventTypesJSON), &reg.EventTypes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event types: %w", err)
	}

	reg.CreatedAt = time.Unix(createdAt, 0)
	reg.UpdatedAt = time.Unix(updatedAt, 0)

	return &reg, nil
}

// ListWebhooks retrieves all webhooks, optionally filtering by active status.
func (s *WebhookStore) ListWebhooks(activeOnly bool) ([]WebhookRegistration, error) {
	query := `
		SELECT id, url, secret, event_types, active, created_at, updated_at
		FROM webhook_registrations
	`

	if activeOnly {
		query += " WHERE active = 1"
	}

	query += " ORDER BY created_at DESC"

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to list webhooks: %w", err)
	}
	defer rows.Close()

	var webhooks []WebhookRegistration

	for rows.Next() {
		var reg WebhookRegistration
		var eventTypesJSON string
		var secret sql.NullString
		var createdAt, updatedAt int64

		err := rows.Scan(
			&reg.ID,
			&reg.URL,
			&secret,
			&eventTypesJSON,
			&reg.Active,
			&createdAt,
			&updatedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan webhook: %w", err)
		}

		if secret.Valid {
			reg.Secret = secret.String
		}

		if err := json.Unmarshal([]byte(eventTypesJSON), &reg.EventTypes); err != nil {
			return nil, fmt.Errorf("failed to unmarshal event types: %w", err)
		}

		reg.CreatedAt = time.Unix(createdAt, 0)
		reg.UpdatedAt = time.Unix(updatedAt, 0)

		webhooks = append(webhooks, reg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return webhooks, nil
}

// UpdateWebhook updates an existing webhook registration.
func (s *WebhookStore) UpdateWebhook(reg WebhookRegistration) error {
	eventTypesJSON, err := json.Marshal(reg.EventTypes)
	if err != nil {
		return fmt.Errorf("failed to marshal event types: %w", err)
	}

	reg.UpdatedAt = time.Now()

	query := `
		UPDATE webhook_registrations
		SET url = ?, secret = ?, event_types = ?, active = ?, updated_at = ?
		WHERE id = ?
	`

	result, err := s.db.Exec(query,
		reg.URL,
		reg.Secret,
		string(eventTypesJSON),
		reg.Active,
		reg.UpdatedAt.Unix(),
		reg.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update webhook: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("webhook not found: %s", reg.ID)
	}

	return nil
}

// DeleteWebhook removes a webhook registration.
func (s *WebhookStore) DeleteWebhook(id string) error {
	query := `DELETE FROM webhook_registrations WHERE id = ?`

	result, err := s.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete webhook: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("webhook not found: %s", id)
	}

	return nil
}

// RecordDelivery logs a webhook delivery attempt.
func (s *WebhookStore) RecordDelivery(attempt DeliveryAttempt) error {
	query := `
		INSERT INTO webhook_deliveries
		(webhook_id, payload_id, event_type, attempt_number, status_code, success, error, attempted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	var errorMsg sql.NullString
	if attempt.Error != "" {
		errorMsg.String = attempt.Error
		errorMsg.Valid = true
	}

	var statusCode sql.NullInt64
	if attempt.StatusCode > 0 {
		statusCode.Int64 = int64(attempt.StatusCode)
		statusCode.Valid = true
	}

	_, err := s.db.Exec(query,
		attempt.WebhookID,
		attempt.PayloadID,
		attempt.EventType,
		attempt.AttemptNumber,
		statusCode,
		attempt.Success,
		errorMsg,
		attempt.AttemptedAt.Unix(),
	)

	if err != nil {
		return fmt.Errorf("failed to record delivery: %w", err)
	}

	return nil
}

// GetDeliveryStats retrieves delivery statistics for a webhook.
// Note: Delivery records are retained indefinitely for audit purposes.
// TODO: implements a cleanup job if storage becomes a concern (e.g., delete records older than 90 days).
func (s *WebhookStore) GetDeliveryStats(webhookID string, since time.Time) (*DeliveryStats, error) {
	query := `
		SELECT
			COUNT(*) as total,
			SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) as successful,
			SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) as failed,
			MAX(CASE WHEN attempted_at > 0 THEN attempted_at ELSE NULL END) as last_delivery,
			MAX(CASE WHEN success = 0 AND attempted_at > 0 THEN attempted_at ELSE NULL END) as last_failure
		FROM webhook_deliveries
		WHERE webhook_id = ? AND attempted_at >= ?
	`

	var stats DeliveryStats
	var lastDelivery, lastFailure sql.NullInt64

	err := s.db.QueryRow(query, webhookID, since.Unix()).Scan(
		&stats.TotalDeliveries,
		&stats.SuccessfulDeliveries,
		&stats.FailedDeliveries,
		&lastDelivery,
		&lastFailure,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get delivery stats: %w", err)
	}

	if stats.TotalDeliveries > 0 {
		stats.SuccessRate = float64(stats.SuccessfulDeliveries) / float64(stats.TotalDeliveries) * 100
	}

	if lastDelivery.Valid {
		t := time.Unix(lastDelivery.Int64, 0)
		stats.LastDeliveryAt = &t
	}

	if lastFailure.Valid {
		t := time.Unix(lastFailure.Int64, 0)
		stats.LastFailureAt = &t
	}

	return &stats, nil
}
