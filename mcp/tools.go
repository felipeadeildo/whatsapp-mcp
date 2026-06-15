package mcp

import (
	"github.com/mark3labs/mcp-go/mcp"
)

// registerTools defines all MCP tools available to clients.
func (m *MCPServer) registerTools() {
	// 1. list all chats
	m.server.AddTool(
		mcp.NewTool("list_chats",
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithDescription("List WhatsApp conversations ordered by most recent activity. Returns chat details including JID, name, last message timestamp, and unread count."),
			mcp.WithNumber("limit",
				mcp.Description("maximum number of chats to return (default: 50, max: 100)"),
			),
		),
		m.handleListChats,
	)

	// 2. get messages from specific chat
	m.server.AddTool(
		mcp.NewTool("get_chat_messages",
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithDescription("Retrieve message history from a specific WhatsApp chat. Supports pagination via timestamps or offset, and can filter by sender."),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("chat JID (WhatsApp identifier) from find_chat or list_chats"),
			),
			mcp.WithNumber("limit",
				mcp.Description("maximum number of messages to return (default: 50, max: 200)"),
			),
			mcp.WithString("before_timestamp",
				mcp.Description("get messages before this timestamp (ISO 8601 format)"),
			),
			mcp.WithString("after_timestamp",
				mcp.Description("get messages after this timestamp (ISO 8601 format)"),
			),
			mcp.WithString("from",
				mcp.Description("filter messages by sender JID (e.g., for filtering one person's messages in a group chat)"),
			),
			mcp.WithNumber("offset",
				mcp.Description("number of messages to skip for pagination (default: 0)"),
			),
		),
		m.handleGetChatMessages,
	)

	// 3. search messages by text
	m.server.AddTool(
		mcp.NewTool("search_messages",
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithDescription("Search for messages across all WhatsApp chats by text content or sender. Supports pattern matching with wildcards (*, ?, [abc])."),
			mcp.WithString("query",
				mcp.Description("text pattern to search for (optional: can be omitted when using only 'from' parameter)"),
			),
			mcp.WithString("from",
				mcp.Description("filter by sender JID to find all messages from a specific person across all chats"),
			),
			mcp.WithNumber("limit",
				mcp.Description("maximum number of results to return (default: 50, max: 200)"),
			),
		),
		m.handleSearchMessages,
	)

	// 4. find chat by name or JID
	m.server.AddTool(
		mcp.NewTool("find_chat",
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithDescription("Find WhatsApp chats by searching names or JIDs. Supports pattern matching with wildcards. Returns matching chats with their JIDs."),
			mcp.WithString("search",
				mcp.Required(),
				mcp.Description("search pattern (supports wildcards: *, ?, [abc])"),
			),
		),
		m.handleFindChat,
	)

	// 5. send message
	m.server.AddTool(
		mcp.NewTool("send_message",
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithDescription("Send a text message to a WhatsApp chat (DM or group)."),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("recipient chat JID from find_chat or list_chats"),
			),
			mcp.WithString("text",
				mcp.Required(),
				mcp.Description("message text to send"),
			),
		),
		m.handleSendMessage,
	)

	// 6. load more messages on-demand
	m.server.AddTool(
		mcp.NewTool("load_more_messages",
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithDescription("Fetch additional message history from WhatsApp servers for a specific chat. Use when you need older messages not yet in the database."),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("chat JID to fetch history for"),
			),
			mcp.WithNumber("count",
				mcp.Description("number of messages to fetch (default: 50, max: 200)"),
			),
			mcp.WithBoolean("wait_for_sync",
				mcp.Description("if true (default), waits for messages to arrive before returning. If false, messages load in background."),
			),
		),
		m.handleLoadMoreMessages,
	)

	// 7. get my info
	m.server.AddTool(
		mcp.NewTool("get_my_info",
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithDescription("Get your own WhatsApp profile information including JID, display name, status/bio, and profile picture URL."),
		),
		m.handleGetMyInfo,
	)

	// 8. send a file (image, video, audio, document) to a chat
	m.server.AddTool(
		mcp.NewTool("send_file",
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithDescription("Send a local file as an image, video, audio or document message. The kind is decided from the file extension; unknown extensions are sent as generic documents. Caption is shown on images, videos and documents."),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("recipient chat JID from find_chat or list_chats"),
			),
			mcp.WithString("file_path",
				mcp.Required(),
				mcp.Description("absolute or relative path to a local file (no path traversal allowed)"),
			),
			mcp.WithString("caption",
				mcp.Description("optional caption to display alongside the file (images/videos/documents only)"),
			),
		),
		m.handleSendFile,
	)

	// 9. send an audio file as a voice note (PTT)
	m.server.AddTool(
		mcp.NewTool("send_audio_message",
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithDescription("Send an audio file as a WhatsApp voice note (PTT). Files in .ogg or .opus format are sent as-is; other formats (mp3, wav, m4a, etc.) are converted to ogg-opus via ffmpeg first. Returns an error mentioning ffmpeg if it isn't installed."),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("recipient chat JID"),
			),
			mcp.WithString("audio_path",
				mcp.Required(),
				mcp.Description("path to a local audio file to send as a voice note"),
			),
		),
		m.handleSendAudioMessage,
	)

	// 10. react to an existing message with an emoji (or remove a reaction)
	m.server.AddTool(
		mcp.NewTool("send_reaction",
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithDescription("React to an existing message with an emoji. Pass an empty emoji to remove your previous reaction. The message_id can come from get_chat_messages or search_messages."),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("chat JID where the target message lives"),
			),
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("ID of the message to react to"),
			),
			mcp.WithString("emoji",
				mcp.Required(),
				mcp.Description("emoji to react with; pass an empty string to remove your reaction"),
			),
			mcp.WithString("sender_jid",
				mcp.Description("JID of the original message's sender; omit for your own messages"),
			),
		),
		m.handleSendReaction,
	)

	// 11. edit a previously sent message
	m.server.AddTool(
		mcp.NewTool("edit_message",
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(true),
			mcp.WithDescription("Edit the text of a message you sent. WhatsApp only accepts edits within roughly 20 minutes of the original send; older edits will fail with a server error."),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("chat JID where the message was sent"),
			),
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("ID of the message to edit"),
			),
			mcp.WithString("new_text",
				mcp.Required(),
				mcp.Description("replacement text"),
			),
		),
		m.handleEditMessage,
	)

	// 12. delete (revoke) a message for everyone
	m.server.AddTool(
		mcp.NewTool("delete_message",
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(true),
			mcp.WithDescription("Delete a message for everyone in the chat (revoke). For your own messages, omit sender_jid. For deleting another user's message in a group where you are admin, pass their JID as sender_jid."),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("chat JID where the message lives"),
			),
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("ID of the message to delete"),
			),
			mcp.WithString("sender_jid",
				mcp.Description("JID of the original message's sender; omit when deleting your own message"),
			),
		),
		m.handleDeleteMessage,
	)

	// 13. transcribe an audio message (voice note or generic audio) to text
	m.server.AddTool(
		mcp.NewTool("transcribe_audio_message",
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithDescription("Transcribe a WhatsApp audio message (voice note or audio file) to text. Uses OpenRouter STT (whisper-large-v3) when configured, falling back to a local whisper.cpp model. If the audio isn't on disk yet, the server fetches it from the CDN on demand. Default language is Brazilian Portuguese (WHISPER_LANGUAGE)."),
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("ID of the audio message to transcribe (from get_chat_messages or search_messages)"),
			),
		),
		m.handleTranscribeAudioMessage,
	)

	// 13b. transcribe MANY audio messages concurrently (batch) — fixes the burst
	// bottleneck by fanning out to OpenRouter with a bounded worker pool.
	m.server.AddTool(
		mcp.NewTool("transcribe_audios_batch",
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithDescription("Transcribe MANY WhatsApp audio messages at once, concurrently. Provide either 'message_ids' (an explicit array) OR 'chat_jid' to auto-collect that chat's recent voice notes/audios (up to 'limit'). Returns per-message transcripts plus a failures section; partial results are returned if some fail. Uses the transcript cache (already-transcribed messages are free). Capped at 200 per call."),
			mcp.WithArray("message_ids",
				mcp.Description("Explicit list of audio message IDs to transcribe. Takes precedence over chat_jid."),
				mcp.Items(map[string]any{"type": "string"}),
			),
			mcp.WithString("chat_jid",
				mcp.Description("If message_ids is omitted, auto-collect audio/voice-note messages from this chat JID (e.g. '5521998568008@s.whatsapp.net')."),
			),
			mcp.WithNumber("limit",
				mcp.Description("Max audios to collect when using chat_jid (default 50, hard cap 200)."),
			),
		),
		m.handleTranscribeAudiosBatch,
	)

	// 14. force-download a media attachment (audio/image/video/document) on demand
	m.server.AddTool(
		mcp.NewTool("download_media",
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithDescription("Force-download a media attachment by message_id. Use when auto-download was skipped, failed, or the file was deleted by flush_media_cache. Re-fetches from the WhatsApp CDN using media_key+direct_path stored in the local DB and decrypts to data/media/. CDN URLs expire after some weeks (returns 'expired' status when that happens)."),
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("ID of the message whose media should be downloaded"),
			),
			mcp.WithBoolean("force",
				mcp.Description("Re-download even if the file is already on disk (default: false). Use when you suspect on-disk corruption."),
			),
		),
		m.handleDownloadMedia,
	)

	// 15. delete cached media files (and optionally reset DB so they can be re-fetched)
	m.server.AddTool(
		mcp.NewTool("flush_media_cache",
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(true),
			mcp.WithDescription("Delete cached media files in data/media/ to free disk space. By default, resets download_status='skipped' on each affected row so download_media can re-fetch on demand (CDN expiry permitting). All filters are optional and combine with AND. Always run with dry_run=true first to preview."),
			mcp.WithString("chat_jid",
				mcp.Description("Limit flush to one chat (e.g. '5521998568008@s.whatsapp.net')"),
			),
			mcp.WithString("media_type",
				mcp.Description("Limit by media type: image | video | audio | ptt | sticker | document"),
			),
			mcp.WithString("before_date",
				mcp.Description("Only flush files whose message timestamp is older than this ISO date (e.g. '2026-04-01')"),
			),
			mcp.WithBoolean("reset_state",
				mcp.Description("After deleting, set download_status='skipped' so on-demand re-fetch is possible. Default: true."),
			),
			mcp.WithBoolean("dry_run",
				mcp.Description("Preview what would be removed without touching disk or DB. Default: false."),
			),
		),
		m.handleFlushMediaCache,
	)
}
