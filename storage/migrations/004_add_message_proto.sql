-- Add message_proto column to store serialized protobuf for retry receipt handling.
-- When a recipient can't decrypt a message, WhatsApp sends a retry receipt
-- requesting re-encryption. This column stores the original protobuf so the
-- server can fulfill retry requests even after restart.
ALTER TABLE messages ADD COLUMN message_proto BLOB;
