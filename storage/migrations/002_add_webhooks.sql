-- Migration: 002_add_webhooks
-- Description: add webhooks
-- Previous: 001
-- Version: 002
-- Created: 2026-01-04

-- Webhook registrations table
CREATE TABLE IF NOT EXISTS webhook_registrations (
    id TEXT PRIMARY KEY,                    -- UUID
    url TEXT NOT NULL,                      -- Target webhook URL
    secret TEXT,                            -- Optional HMAC signing secret
    event_types TEXT NOT NULL,              -- JSON array: ["message"]
    active BOOLEAN NOT NULL DEFAULT 1,      -- Enable/disable flag
    created_at INTEGER NOT NULL,            -- Unix timestamp
    updated_at INTEGER NOT NULL             -- Unix timestamp
);

-- Index for active webhooks lookup
CREATE INDEX IF NOT EXISTS idx_webhooks_active ON webhook_registrations(active);

-- Webhook delivery log table
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    webhook_id TEXT NOT NULL,               -- FK to webhook_registrations
    payload_id TEXT NOT NULL,               -- Event UUID
    event_type TEXT NOT NULL,               -- "message.received", etc.
    attempt_number INTEGER NOT NULL,        -- 1, 2, 3
    status_code INTEGER,                    -- HTTP status code
    success BOOLEAN NOT NULL,               -- true/false
    error TEXT,                             -- Error message if failed
    attempted_at INTEGER NOT NULL,          -- Unix timestamp

    FOREIGN KEY (webhook_id) REFERENCES webhook_registrations(id) ON DELETE CASCADE
);

-- Indexes for delivery tracking
CREATE INDEX IF NOT EXISTS idx_deliveries_webhook ON webhook_deliveries(webhook_id);
CREATE INDEX IF NOT EXISTS idx_deliveries_payload ON webhook_deliveries(payload_id);
CREATE INDEX IF NOT EXISTS idx_deliveries_timestamp ON webhook_deliveries(attempted_at DESC);
