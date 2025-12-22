package mcp

import (
	"context"
	"fmt"
	"strings"

	"whatsapp-mcp/storage"

	"github.com/mark3labs/mcp-go/mcp"
)

func (m *MCPServer) handleListChats(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// get limit parameter with default
	limit := request.GetFloat("limit", 50.0)
	if limit > 100 {
		limit = 100
	}

	// query database
	chats, err := m.store.ListChats(int(limit))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list chats: %v", err)), nil
	}

	// format response
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Found %d chats:\n\n", len(chats)))

	for i, chat := range chats {
		chatType := "DM"
		if chat.IsGroup {
			chatType = "Group"
		}

		jid := chat.JID
		result.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, chatType, chat.Name))
		result.WriteString(fmt.Sprintf("   JID: %s\n", jid))
		result.WriteString(fmt.Sprintf("   Last message: %s\n", chat.LastMessageTime.Format("2006-01-02 15:04:05")))
		if chat.UnreadCount > 0 {
			result.WriteString(fmt.Sprintf("   Unread: %d\n", chat.UnreadCount))
		}
		result.WriteString("\n")
	}

	return mcp.NewToolResultText(result.String()), nil
}

func (m *MCPServer) handleGetChatMessages(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// get required chat_jid
	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return mcp.NewToolResultError("chat_jid parameter is required"), nil
	}

	// get optional limit and offset
	limit := request.GetFloat("limit", 50.0)
	if limit > 200 {
		limit = 200
	}

	offset := request.GetFloat("offset", 0.0)

	// query database
	messages, err := m.store.GetChatMessages(chatJID, int(limit), int(offset))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get messages: %v", err)), nil
	}

	// format response
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Retrieved %d messages from chat %s:\n\n", len(messages), chatJID))

	for i := len(messages) - 1; i >= 0; i-- { // reverse to show oldest first
		msg := messages[i]

		// prefer contact name over sender name (PushName)
		sender := ""
		if msg.ContactName != nil && *msg.ContactName != "" {
			sender = *msg.ContactName
		} else if msg.SenderName != "" {
			sender = msg.SenderName
		} else {
			sender = msg.SenderJID
		}

		direction := "←"
		if msg.IsFromMe {
			direction = "→"
			sender = "You"
		}

		result.WriteString(fmt.Sprintf("[%s] %s %s: %s\n",
			msg.Timestamp.Format("15:04:05"),
			direction,
			sender,
			msg.Text,
		))
	}

	return mcp.NewToolResultText(result.String()), nil
}

func (m *MCPServer) handleSearchMessages(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// get required query
	query, err := request.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError("query parameter is required"), nil
	}

	// get optional limit
	limit := request.GetFloat("limit", 50.0)
	if limit > 200 {
		limit = 200
	}

	// search database
	messages, err := m.store.SearchMessages(query, int(limit))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
	}

	// format response
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Found %d messages matching '%s':\n\n", len(messages), query))

	for i, msg := range messages {
		// prefer contact name over sender name (PushName)
		sender := ""
		if msg.ContactName != nil && *msg.ContactName != "" {
			sender = *msg.ContactName
		} else if msg.SenderName != "" {
			sender = msg.SenderName
		} else {
			sender = msg.SenderJID
		}

		if msg.IsFromMe {
			sender = "You"
		}

		result.WriteString(fmt.Sprintf("%d. [%s] %s in chat %s:\n",
			i+1,
			msg.Timestamp.Format("2006-01-02 15:04"),
			sender,
			msg.ChatJID,
		))
		result.WriteString(fmt.Sprintf("   %s\n\n", msg.Text))
	}

	return mcp.NewToolResultText(result.String()), nil
}

func (m *MCPServer) handleFindChat(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// get required search parameter
	search, err := request.RequireString("search")
	if err != nil {
		return mcp.NewToolResultError("search parameter is required"), nil
	}

	// list all chats and filter
	chats, err := m.store.ListChats(100)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to search chats: %v", err)), nil
	}

	// filter by name or JID
	var matches []storage.Chat
	searchLower := strings.ToLower(search)
	for _, chat := range chats {
		if strings.Contains(strings.ToLower(chat.Name), searchLower) ||
			strings.Contains(strings.ToLower(chat.JID), searchLower) {
			matches = append(matches, chat)
		}
	}

	// format response
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Found %d matching chats:\n\n", len(matches)))

	for i, chat := range matches {
		chatType := "DM"
		if chat.IsGroup {
			chatType = "Group"
		}

		result.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, chatType, chat.Name))
		result.WriteString(fmt.Sprintf("   JID: %s\n\n", chat.JID))
	}

	return mcp.NewToolResultText(result.String()), nil
}

func (m *MCPServer) handleSendMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// get required parameters
	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return mcp.NewToolResultError("chat_jid parameter is required"), nil
	}

	text, err := request.RequireString("text")
	if err != nil {
		return mcp.NewToolResultError("text parameter is required"), nil
	}

	// check WhatsApp connection
	if !m.wa.IsLoggedIn() {
		return mcp.NewToolResultError("WhatsApp is not connected"), nil
	}

	// send message
	err = m.wa.SendTextMessage(ctx, chatJID, text)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to send message: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Message sent successfully to %s", chatJID)), nil
}
