package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerPrompts defines all MCP prompts for common workflows.
func (m *MCPServer) registerPrompts() {
	m.server.AddPrompt(
		mcp.NewPrompt("search_person_messages",
			mcp.WithPromptDescription("Find ALL messages from a specific person across all WhatsApp chats to understand context about them"),
			mcp.WithArgument("contact_name",
				mcp.ArgumentDescription("Name of the person whose messages you want to find"),
				mcp.RequiredArgument(),
			),
		),
		m.handleSearchPersonMessagesPrompt,
	)

	m.server.AddPrompt(
		mcp.NewPrompt("get_context_about_person",
			mcp.WithPromptDescription("Get comprehensive context about someone by analyzing all their messages"),
			mcp.WithArgument("contact_name",
				mcp.ArgumentDescription("Name of the person to analyze"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("focus",
				mcp.ArgumentDescription("Focus area: 'recent' for recent activity only, 'all' for complete history (default: all)"),
			),
		),
		m.handleGetContextAboutPersonPrompt,
	)

	m.server.AddPrompt(
		mcp.NewPrompt("analyze_conversation",
			mcp.WithPromptDescription("Analyze recent messages from a specific conversation"),
			mcp.WithArgument("contact_name",
				mcp.ArgumentDescription("Name of the contact or group"),
				mcp.RequiredArgument(),
			),
		),
		m.handleAnalyzeConversationPrompt,
	)

	m.server.AddPrompt(
		mcp.NewPrompt("search_keyword",
			mcp.WithPromptDescription("Search for specific text or keywords across all WhatsApp conversations"),
			mcp.WithArgument("keyword",
				mcp.ArgumentDescription("Text or keyword to search for"),
				mcp.RequiredArgument(),
			),
		),
		m.handleSearchKeywordPrompt,
	)
}

// handleSearchPersonMessagesPrompt handles the search_person_messages prompt request.
func (m *MCPServer) handleSearchPersonMessagesPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	contactName := req.Params.Arguments["contact_name"]
	if contactName == "" {
		contactName = "[contact name]"
	}

	promptText := `I want to find ALL messages from ` + contactName + ` across ALL my WhatsApp conversations to understand context about them.

**CRITICAL WORKFLOW:**

1. First, use find_chat to get their JID:
   - find_chat(search="` + contactName + `")
   - This returns their unique identifier (JID)

2. Then, use search_messages with ONLY the 'from' parameter (NO query parameter):
   - search_messages(from="[the JID from step 1]")
   - **DO NOT** include a query parameter - we want ALL their messages
   - This searches across ALL chats (DMs, groups, everywhere)

3. Show me all the messages found, organized by:
   - Date and time
   - Which chat they're from
   - Message content

This helps me understand:
- What topics they discuss
- Their communication patterns
- Context from all our interactions
- Recent vs historical messages`

	return mcp.NewGetPromptResult(
		"Find all messages from "+contactName,
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(
				mcp.RoleUser,
				mcp.NewTextContent(promptText),
			),
		},
	), nil
}

// handleGetContextAboutPersonPrompt handles the get_context_about_person prompt request.
func (m *MCPServer) handleGetContextAboutPersonPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	contactName := req.Params.Arguments["contact_name"]
	if contactName == "" {
		contactName = "[contact name]"
	}

	focus := req.Params.Arguments["focus"]
	if focus == "" {
		focus = "all"
	}

	focusGuidance := "all messages"
	if focus == "recent" {
		focusGuidance = "recent messages (last 50-100)"
	}

	promptText := `I want to get comprehensive context about ` + contactName + ` by analyzing their WhatsApp messages.

**Workflow:**

1. Use find_chat to find ` + contactName + `:
   - find_chat(search="` + contactName + `")
   - Get their JID (WhatsApp identifier)

2. Get ` + focusGuidance + `:
   - search_messages(from="[JID from step 1]")
   - This finds all their messages across all conversations

3. Analyze and provide:
   - **Communication patterns**: How often do they message? What times?
   - **Main topics**: What do they usually talk about?
   - **Sentiment/tone**: Are they formal, casual, friendly?
   - **Recent activity**: What have they discussed lately?
   - **Key information**: Any important details to remember about them?

Please provide a comprehensive summary that helps me understand who ` + contactName + ` is and our relationship context.`

	return mcp.NewGetPromptResult(
		"Get context about "+contactName,
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(
				mcp.RoleUser,
				mcp.NewTextContent(promptText),
			),
		},
	), nil
}

// handleAnalyzeConversationPrompt handles the analyze_conversation prompt request.
func (m *MCPServer) handleAnalyzeConversationPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	contactName := req.Params.Arguments["contact_name"]
	if contactName == "" {
		contactName = "[contact name]"
	}

	promptText := `I want to analyze my recent WhatsApp conversation with ` + contactName + `.

**Workflow:**

1. Find the conversation:
   - find_chat(search="` + contactName + `")
   - This will give you the chat_jid

2. Get recent messages:
   - get_chat_messages(chat_jid="[from step 1]", limit=50)
   - Retrieve the last 50 messages from this specific conversation

3. Analyze and provide:
   - **Main topics discussed**: What are we talking about?
   - **Action items**: Any pending tasks or requests?
   - **Important dates/events**: Any deadlines or meetings mentioned?
   - **Conversation tone**: Is it professional, casual, friendly?
   - **Key takeaways**: What are the most important points?

Please provide a structured summary of our recent conversation.`

	return mcp.NewGetPromptResult(
		"Analyze conversation with "+contactName,
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(
				mcp.RoleUser,
				mcp.NewTextContent(promptText),
			),
		},
	), nil
}

// handleSearchKeywordPrompt handles the search_keyword prompt request.
func (m *MCPServer) handleSearchKeywordPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	keyword := req.Params.Arguments["keyword"]
	if keyword == "" {
		keyword = "[keyword]"
	}

	promptText := `I want to search for "` + keyword + `" across all my WhatsApp conversations.

**Workflow:**

1. Use search_messages with the keyword:
   - search_messages(query="` + keyword + `")
   - This searches across ALL chats (DMs and groups)

2. Show me the results organized by:
   - Which chat/contact the message is from
   - When it was sent (date and time)
   - The message content (with the keyword highlighted in context)
   - Relevance (most recent or most relevant first)

**Tips:**
- Use wildcards for broader search: *` + keyword + `* for case-sensitive
- Combine with sender if needed: search_messages(query="` + keyword + `", from="[JID]")
- Search is case-insensitive by default

Please show me all instances of "` + keyword + `" in my WhatsApp messages.`

	return mcp.NewGetPromptResult(
		"Search for '"+keyword+"' across all chats",
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(
				mcp.RoleUser,
				mcp.NewTextContent(promptText),
			),
		},
	), nil
}
