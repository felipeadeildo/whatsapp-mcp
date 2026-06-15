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
			mcp.WithDescription("Transcribe a WhatsApp audio message (voice note or audio file) to text using a locally-hosted whisper.cpp model. The audio must already be downloaded (auto-download is on for audio by default). Default language is Brazilian Portuguese; configure via WHISPER_LANGUAGE."),
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("ID of the audio message to transcribe (from get_chat_messages or search_messages)"),
			),
		),
		m.handleTranscribeAudioMessage,
	)
}
