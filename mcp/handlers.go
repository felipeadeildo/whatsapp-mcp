package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"whatsapp-mcp/storage"

	"github.com/mark3labs/mcp-go/mcp"
)

// getDisplayName returns the best available name for a chat
// Priority: ContactName > PushName > JID
func getDisplayName(chat storage.Chat) string {
	if chat.ContactName != "" {
		return chat.ContactName
	}
	if chat.PushName != "" {
		return chat.PushName
	}
	return chat.JID
}

// getSenderDisplayName returns the best available name for a message sender
// Priority: ContactName > PushName > JID
func getSenderDisplayName(msg storage.MessageWithNames) string {
	if msg.SenderContactName != "" {
		return msg.SenderContactName
	}
	if msg.SenderPushName != "" {
		return msg.SenderPushName
	}
	return msg.SenderJID
}

// converts a UTC timestamp to the configured timezone
func (m *MCPServer) toLocalTime(t time.Time) time.Time {
	return t.In(m.timezone)
}

// formats a timestamp in the configured timezone (for date + time display)
func (m *MCPServer) formatDateTime(t time.Time) string {
	return m.toLocalTime(t).Format("2006-01-02 15:04:05")
}

// formats a timestamp in the configured timezone (for time-only display)
func (m *MCPServer) formatTime(t time.Time) string {
	return m.toLocalTime(t).Format("15:04:05")
}

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
	fmt.Fprintf(&result, "Found %d chats:\n\n", len(chats))

	for i, chat := range chats {
		chatType := "DM"
		if chat.IsGroup {
			chatType = "Group"
		}

		jid := chat.JID
		displayName := getDisplayName(chat)
		fmt.Fprintf(&result, "%d. [%s] %s\n", i+1, chatType, displayName)
		fmt.Fprintf(&result, "   JID: %s\n", jid)
		if chat.ContactName != "" && chat.PushName != "" && chat.ContactName != chat.PushName {
			fmt.Fprintf(&result, "   (Contact: %s, Push: %s)\n", chat.ContactName, chat.PushName)
		}
		fmt.Fprintf(&result, "   Last message: %s\n", m.formatDateTime(chat.LastMessageTime))
		if chat.UnreadCount > 0 {
			fmt.Fprintf(&result, "   Unread: %d\n", chat.UnreadCount)
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

	// query database (with sender names from view)
	messages, err := m.store.GetChatMessagesWithNames(chatJID, int(limit), int(offset))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get messages: %v", err)), nil
	}

	// format response
	var result strings.Builder
	fmt.Fprintf(&result, "Retrieved %d messages from chat %s:\n\n", len(messages), chatJID)

	for i := len(messages) - 1; i >= 0; i-- { // reverse to show oldest first
		msg := messages[i]
		sender := getSenderDisplayName(msg)

		direction := "←"
		if msg.IsFromMe {
			direction = "→"
			sender = "You"
		}

		fmt.Fprintf(&result, "[%s] %s %s: %s\n",
			m.formatTime(msg.Timestamp),
			direction,
			sender,
			msg.Text)
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

	// search database (with sender names from view)
	messages, err := m.store.SearchMessagesWithNames(query, int(limit))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
	}

	// format response
	var result strings.Builder
	fmt.Fprintf(&result, "Found %d messages matching '%s':\n\n", len(messages), query)

	for i, msg := range messages {
		sender := getSenderDisplayName(msg)

		if msg.IsFromMe {
			sender = "You"
		}

		fmt.Fprintf(&result, "%d. [%s] %s in chat %s:\n",
			i+1,
			m.formatDateTime(msg.Timestamp),
			sender,
			msg.ChatJID)
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

	// search chats in database with fuzzy matching
	chats, err := m.store.SearchChats(search, 100)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to search chats: %v", err)), nil
	}

	// format response
	var result strings.Builder
	fmt.Fprintf(&result, "Found %d matching chats:\n\n", len(chats))

	for i, chat := range chats {
		chatType := "DM"
		if chat.IsGroup {
			chatType = "Group"
		}

		displayName := getDisplayName(chat)
		fmt.Fprintf(&result, "%d. [%s] %s\n", i+1, chatType, displayName)
		fmt.Fprintf(&result, "   JID: %s\n", chat.JID)
		if chat.ContactName != "" && chat.PushName != "" && chat.ContactName != chat.PushName {
			fmt.Fprintf(&result, "   (Contact: %s, Push: %s)\n", chat.ContactName, chat.PushName)
		}
		result.WriteString("\n")
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

func (m *MCPServer) handleLoadMoreMessages(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// get required chat_jid
	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return mcp.NewToolResultError("chat_jid parameter is required"), nil
	}

	// get optional count (default 50, max 200)
	count := int(request.GetFloat("count", 50.0))
	if count > 200 {
		count = 200
	}
	if count < 1 {
		count = 1
	}

	// get optional wait_for_sync (default true)
	waitForSync := request.GetBool("wait_for_sync", true)

	// check WhatsApp connection
	if !m.wa.IsLoggedIn() {
		return mcp.NewToolResultError("WhatsApp is not connected"), nil
	}

	// request history sync
	messages, err := m.wa.RequestHistorySync(ctx, chatJID, count, waitForSync)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load messages: %v", err)), nil
	}

	// format response
	var result strings.Builder

	if waitForSync {
		fmt.Fprintf(&result, "Loaded %d additional messages from chat %s:\n\n", len(messages), chatJID)

		// format messages (oldest first, like get_chat_messages)
		for i := len(messages) - 1; i >= 0; i-- {
			msg := messages[i]
			sender := getSenderDisplayName(msg)

			direction := "←"
			if msg.IsFromMe {
				direction = "→"
				sender = "You"
			}

			fmt.Fprintf(&result, "[%s] %s %s: %s\n",
				m.formatTime(msg.Timestamp),
				direction,
				sender,
				msg.Text)
		}
	} else {
		fmt.Fprintf(&result, "History sync request sent for chat %s (%d messages). Messages will load in the background. Use get_chat_messages to see them once loaded.", chatJID, count)
	}

	return mcp.NewToolResultText(result.String()), nil
}
