package mcp

import (
	"github.com/mark3labs/mcp-go/mcp"
)

// registerTools defines all MCP tools
func (m *MCPServer) registerTools() {
	// 1. list all chats
	m.server.AddTool(
		mcp.NewTool("list_chats",
			mcp.WithDescription("list all WhatsApp conversations (DMs and groups) ordered by recent activity"),
			mcp.WithNumber("limit",
				mcp.Description("maximum number of chats to return (default: 50, max: 100)"),
			),
		),
		m.handleListChats,
	)

	// 2. get messages from specific chat
	m.server.AddTool(
		mcp.NewTool("get_chat_messages",
			mcp.WithDescription("retrieve message history from a specific WhatsApp chat"),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("chat JID (can be PN format like 5582994011841@s.whatsapp.net or LID format)"),
			),
			mcp.WithNumber("limit",
				mcp.Description("maximum number of messages to return (default: 50, max: 200)"),
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
			mcp.WithDescription("search for messages across all chats by text content"),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("search query text (supports partial matches)"),
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
			mcp.WithDescription("find chats by fuzzy search on contact names, push names, or JIDs (case-insensitive substring matching)"),
			mcp.WithString("search",
				mcp.Required(),
				mcp.Description("search term to match against chat names or JIDs (supports partial matches and emojis)"),
			),
		),
		m.handleFindChat,
	)

	// 5. send message
	m.server.AddTool(
		mcp.NewTool("send_message",
			mcp.WithDescription("send a text message to a WhatsApp chat"),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("recipient chat JID (can be PN or LID format)"),
			),
			mcp.WithString("text",
				mcp.Required(),
				mcp.Description("message text to send"),
			),
		),
		m.handleSendMessage,
	)
}
