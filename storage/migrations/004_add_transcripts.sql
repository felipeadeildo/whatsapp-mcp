-- Migration: 004_add_transcripts
-- Description: cache audio transcriptions to avoid re-running whisper
-- Previous: 003
-- Version: 004
-- Created: 2026-04-30

-- One row per message that has been transcribed at least once.
-- Re-transcribing is just a DELETE then re-call.
CREATE TABLE IF NOT EXISTS transcripts (
    message_id TEXT PRIMARY KEY,
    text       TEXT NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
