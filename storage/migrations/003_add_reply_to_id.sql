-- Migration: 003_add_reply_to_id
-- Description: Add reply_to_id field for reactions and replies
-- Previous: 002_add_webhooks
-- Version: 003
-- Created: 2026-01-25

-- Add reply_to_id column to messages table
-- This stores the ID of the message being reacted to or replied to
ALTER TABLE messages ADD COLUMN reply_to_id TEXT;

-- Index for finding all reactions/replies to a specific message
CREATE INDEX IF NOT EXISTS idx_reply_to_id ON messages(reply_to_id) WHERE reply_to_id IS NOT NULL;
