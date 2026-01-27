package mcp

import (
	"github.com/mark3labs/mcp-go/mcp"
)

// registerTools defines all MCP tools available to clients.
func (m *MCPServer) registerTools() {
	// 1. list all chats
	m.server.AddTool(
		mcp.NewTool("list_chats",
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
			mcp.WithDescription("Get your own WhatsApp profile information including JID, display name, status/bio, and profile picture URL."),
		),
		m.handleGetMyInfo,
	)

	// 8. send image
	m.server.AddTool(
		mcp.NewTool("send_image",
			mcp.WithDescription("Send an image to a WhatsApp chat (JPEG, PNG, WebP, max 5MB). Supports both local file paths and base64-encoded data."),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("recipient chat JID from find_chat or list_chats"),
			),
			mcp.WithString("file_path",
				mcp.Description("local path to image file (use for Claude Code/local access)"),
			),
			mcp.WithString("file_data",
				mcp.Description("base64-encoded image data (use for Web Apps/remote access)"),
			),
			mcp.WithString("caption",
				mcp.Description("optional caption text for the image"),
			),
		),
		m.handleSendImage,
	)

	// 9. send video
	m.server.AddTool(
		mcp.NewTool("send_video",
			mcp.WithDescription("Send a video to a WhatsApp chat (MP4, 3GP, max 100MB). Supports both local file paths and base64-encoded data."),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("recipient chat JID from find_chat or list_chats"),
			),
			mcp.WithString("file_path",
				mcp.Description("local path to video file"),
			),
			mcp.WithString("file_data",
				mcp.Description("base64-encoded video data"),
			),
			mcp.WithString("caption",
				mcp.Description("optional caption"),
			),
		),
		m.handleSendVideo,
	)

	// 10. send document
	m.server.AddTool(
		mcp.NewTool("send_document",
			mcp.WithDescription("Send a document to a WhatsApp chat (PDF, DOCX, etc., max 2GB). Supports both local file paths and base64-encoded data."),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("recipient chat JID from find_chat or list_chats"),
			),
			mcp.WithString("file_path",
				mcp.Description("local path to document file"),
			),
			mcp.WithString("file_data",
				mcp.Description("base64-encoded document data"),
			),
			mcp.WithString("filename",
				mcp.Description("filename for the document (required if using file_data)"),
			),
		),
		m.handleSendDocument,
	)

	// 11. send audio
	m.server.AddTool(
		mcp.NewTool("send_audio",
			mcp.WithDescription("Send an audio file to a WhatsApp chat (MP3, OGG, AAC, M4A, max 16MB). Supports both local file paths and base64-encoded data."),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("recipient chat JID from find_chat or list_chats"),
			),
			mcp.WithString("file_path",
				mcp.Description("local path to audio file"),
			),
			mcp.WithString("file_data",
				mcp.Description("base64-encoded audio data"),
			),
		),
		m.handleSendAudio,
	)
}
