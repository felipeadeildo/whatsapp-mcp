package mcp

import (
	"github.com/mark3labs/mcp-go/mcp"
)

// registerTools defines all MCP tools
func (m *MCPServer) registerTools() {
	// 1. list all chats
	m.server.AddTool(
		mcp.NewTool("list_chats",
			mcp.WithDescription("List all WhatsApp conversations (DMs and groups) ordered by most recent activity.\n\nWHEN TO USE:\n- When you need to see all recent conversations\n- When browsing available chats\n- When you need to get JIDs for multiple chats\n\nWHEN NOT TO USE:\n- DON'T use this to find a specific chat by name - use find_chat instead (much faster)\n\nRETURNS: List of chats with JID, name, last message time, unread count"),
			mcp.WithNumber("limit",
				mcp.Description("maximum number of chats to return (default: 50, max: 100)"),
			),
		),
		m.handleListChats,
	)

	// 2. get messages from specific chat
	m.server.AddTool(
		mcp.NewTool("get_chat_messages",
			mcp.WithDescription("Retrieve message history from ONE SPECIFIC WhatsApp chat (DM or group).\n\nWHEN TO USE:\n- When you need messages from a SPECIFIC chat (you already have the chat_jid)\n- When browsing conversation history in one chat\n- When you need messages in chronological order from one chat\n\nWHEN NOT TO USE:\n- DON'T use this to find messages from a person across MULTIPLE chats - use search_messages with 'from' parameter instead\n- DON'T use this to search by content - use search_messages instead\n\nPAGINATION:\n- Use before_timestamp/after_timestamp for stable timestamp-based pagination (recommended)\n- Or use offset for simple offset-based pagination (legacy)\n\nFILTERING IN GROUPS:\n- Use 'from' parameter ONLY when you want to see messages from a specific person IN THIS SPECIFIC GROUP CHAT\n- Example: In a group chat, filter to see only messages from one participant\n\nTIMESTAMP FORMAT: ISO 8601 (e.g., \"2024-12-31T15:04:05\" or \"2024-12-31\")\nTimes are interpreted in server's timezone: "+m.timezone.String()),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("chat JID from find_chat or list_chats (e.g., '5582994011841@s.whatsapp.net'). NEVER guess or construct JIDs manually"),
			),
			mcp.WithNumber("limit",
				mcp.Description("maximum number of messages to return (default: 50, max: 200)"),
			),
			mcp.WithString("before_timestamp",
				mcp.Description("get messages before this timestamp (ISO 8601 format, e.g., '2024-12-31T15:04:05')"),
			),
			mcp.WithString("after_timestamp",
				mcp.Description("get messages after this timestamp (ISO 8601 format, e.g., '2024-12-31T15:04:05')"),
			),
			mcp.WithString("from",
				mcp.Description("filter messages by sender JID WITHIN THIS CHAT ONLY (e.g., '5582994011841@s.whatsapp.net'). Use this to filter one person's messages in a group chat. For searching across ALL chats, use search_messages with 'from' instead"),
			),
			mcp.WithNumber("offset",
				mcp.Description("number of messages to skip for pagination (default: 0, DEPRECATED: use timestamps instead)"),
			),
		),
		m.handleGetChatMessages,
	)

	// 3. search messages by text
	m.server.AddTool(
		mcp.NewTool("search_messages",
			mcp.WithDescription("Search for messages across ALL chats (DMs and groups) by text content or sender.\n\nCRITICAL USE CASE - Finding ALL messages from a person:\nWhen user asks \"find messages from [person] in all my WhatsApp\" or \"search messages from [person] everywhere\":\n→ Use: search_messages(from=\"person's JID\") with NO query parameter\n→ Example: search_messages(from=\"558293093900@s.whatsapp.net\") ← finds ALL Edeilson's messages everywhere\n\nWHEN TO USE:\n- When user wants to see ALL messages from someone across MULTIPLE chats (use ONLY 'from' parameter, NO query)\n- When searching for specific text/keywords across all conversations (use 'query' parameter)\n- When combining: find specific content from specific person (use both 'query' and 'from')\n\nWHEN NOT TO USE:\n- DON'T use this for browsing messages in chronological order from one chat - use get_chat_messages instead\n- DON'T use get_chat_messages when user wants messages from someone across multiple chats - use THIS tool with 'from'\n\nPATTERN MATCHING:\n- Default: case-insensitive substring matching (e.g., \"meeting\")\n- Wildcards: Use * for any characters, ? for single character (e.g., \"*meeting*\", \"test?\")\n- Character classes: Use [abc] for alternatives (e.g., \"[Hh]ello\")\n\nEXAMPLES:\n- MOST COMMON: from=\"558293093900@s.whatsapp.net\" ← ALL messages from Edeilson everywhere (no query needed!)\n- query=\"budget\" → finds \"budget\", \"Budget Meeting\", \"2024 budget\" across all chats\n- query=\"*TODO*\" → finds any message containing \"TODO\" (case-sensitive)\n- query=\"meeting\", from=\"5582994011841@s.whatsapp.net\" → \"meeting\" mentions by this specific person"),
			mcp.WithString("query",
				mcp.Description("OPTIONAL: text to search for (supports wildcards: *, ?, [abc]). Leave empty/omit when using ONLY 'from' parameter to get ALL messages from someone"),
			),
			mcp.WithString("from",
				mcp.Description("RECOMMENDED FOR FINDING PERSON'S MESSAGES: sender JID to find ALL messages from this person across ALL chats (DMs, groups, everywhere). Get JID from find_chat. Example: '558293093900@s.whatsapp.net'"),
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
			mcp.WithDescription("Find specific chats by searching names or JIDs. STEP 1 before sending messages or getting messages.\n\nWHEN TO USE:\n- When you need to find a chat's JID to use with other tools\n- When looking for a specific person or group by name\n- When you need to identify chats matching certain criteria\n\nWHEN NOT TO USE:\n- DON'T use this to browse all chats - use list_chats instead\n\nWORKFLOW:\n1. Use find_chat to get the JID\n2. Use that JID with get_chat_messages, send_message, or search_messages\n\nPATTERN MATCHING:\n- Default: case-insensitive substring matching (e.g., \"john\")\n- Wildcards: Use * for any characters, ? for single character\n- Case-sensitive when using wildcards\n- Searches contact names, push names, and JIDs\n\nEXAMPLES:\n- search=\"john\" → finds \"John Doe\", \"johnny\", \"JOHN SMITH\" (case-insensitive)\n- search=\"John*\" → finds \"John Doe\", \"Johnny\" (case-sensitive with wildcard)\n- search=\"*group*\" → finds any chat with \"group\" in the name (case-sensitive)\n- search=\"Fundação Estudar\" → finds chats with this text in parentheses or anywhere in name\n\nRETURNS: List of matching chats with JID, name, and type (DM/Group)"),
			mcp.WithString("search",
				mcp.Required(),
				mcp.Description("search pattern (supports wildcards: *, ?, [abc] for advanced matching). Searches contact names, push names, and JIDs"),
			),
		),
		m.handleFindChat,
	)

	// 5. send message
	m.server.AddTool(
		mcp.NewTool("send_message",
			mcp.WithDescription("Send a text message to a WhatsApp chat (DM or group).\n\nIMPORTANT:\n- ALWAYS use find_chat FIRST to get the chat_jid - NEVER guess or construct JIDs manually\n- DON'T send messages without user confirmation\n- Preserve the user's exact wording in the message text\n\nWORKFLOW:\n1. Use find_chat to get the recipient's JID\n2. Confirm with user if needed\n3. Send the message with this tool\n\nRETURNS: Confirmation that message was sent successfully"),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("recipient chat JID from find_chat or list_chats (e.g., '5582994011841@s.whatsapp.net'). NEVER guess or construct JIDs manually"),
			),
			mcp.WithString("text",
				mcp.Required(),
				mcp.Description("exact message text to send (preserve user's wording)"),
			),
		),
		m.handleSendMessage,
	)

	// 6. load more messages on-demand
	m.server.AddTool(
		mcp.NewTool("load_more_messages",
			mcp.WithDescription("fetch additional message history from WhatsApp for a specific chat. This requests messages from your primary WhatsApp device that haven't been synced yet. Use wait_for_sync=true (recommended) for immediate results, or wait_for_sync=false to load messages in the background. Note: Only works if you have messages already in the database for this chat."),
			mcp.WithString("chat_jid",
				mcp.Required(),
				mcp.Description("chat JID to fetch history for (can be PN or LID format)"),
			),
			mcp.WithNumber("count",
				mcp.Description("number of messages to fetch (default: 50, max: 200)"),
			),
			mcp.WithBoolean("wait_for_sync",
				mcp.Description("IMPORTANT: if true (default), waits for messages to arrive before returning (recommended for immediate results, takes 2-10 seconds). If false, messages load in background and you must call get_chat_messages again to see them (use for non-urgent bulk loading)."),
			),
		),
		m.handleLoadMoreMessages,
	)
}
