package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"whatsapp-mcp/config"
	"whatsapp-mcp/storage"
	"whatsapp-mcp/whatsapp"

	"github.com/mark3labs/mcp-go/mcp"
)

// maxInlineImageBytes caps how large an image we return inline in a tool result.
// Base64 inflates ~33% and the bytes land in the model's context, so a multi-MB
// image would blow token budget. WhatsApp images are typically <100KB.
const maxInlineImageBytes = 5 * 1024 * 1024

// maxBatchTranscribe caps how many audios a single batch call will process,
// keeping wall-clock and cost bounded.
const maxBatchTranscribe = 200

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

// toLocalTime converts a UTC timestamp to the configured timezone.
func (m *MCPServer) toLocalTime(t time.Time) time.Time {
	return t.In(m.timezone)
}

// formatDateTime formats a timestamp in the configured timezone for date and time display.
func (m *MCPServer) formatDateTime(t time.Time) string {
	return m.toLocalTime(t).Format("2006-01-02 15:04:05")
}

// formatTime formats a timestamp in the configured timezone for time-only display.
func (m *MCPServer) formatTime(t time.Time) string {
	return m.toLocalTime(t).Format("15:04:05")
}

// parseTimestamp converts an ISO 8601 timestamp string to time.Time in the server's timezone.
// It supports the formats: "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02".
func (m *MCPServer) parseTimestamp(timestampStr string) (time.Time, error) {
	formats := []string{
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.ParseInLocation(format, timestampStr, m.timezone); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("invalid timestamp format: %s (expected ISO 8601 like '2006-01-02T15:04:05' or '2006-01-02')", timestampStr)
}

// detectPatternType determines whether a search query should use GLOB matching.
// It returns true if the query contains glob wildcards: * ? [
func detectPatternType(query string) bool {
	return strings.ContainsAny(query, "*?[")
}

// formatFileSize converts bytes to a human-readable size string.
func formatFileSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	if bytes >= GB {
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	} else if bytes >= MB {
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	} else if bytes >= KB {
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	}
	return fmt.Sprintf("%d B", bytes)
}

// formatDimensions returns a formatted dimensions string from width and height.
func formatDimensions(width, height *int) string {
	if width != nil && height != nil {
		return fmt.Sprintf("%dx%d", *width, *height)
	}
	return ""
}

// formatDuration converts seconds to MM:SS format.
func formatDuration(seconds *int) string {
	if seconds == nil {
		return ""
	}
	s := *seconds
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// handleListChats handles the list_chats tool request.
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

// handleGetChatMessages handles the get_chat_messages tool request.
func (m *MCPServer) handleGetChatMessages(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// get required chat_jid
	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return mcp.NewToolResultError("chat_jid parameter is required"), nil
	}

	// get optional limit
	limit := request.GetFloat("limit", 50.0)
	if limit > 200 {
		limit = 200
	}

	// get optional timestamp filters
	var beforeTime *time.Time
	var afterTime *time.Time

	beforeStr := request.GetString("before_timestamp", "")
	if beforeStr != "" {
		t, err := m.parseTimestamp(beforeStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid before_timestamp: %v", err)), nil
		}
		beforeTime = &t
	}

	afterStr := request.GetString("after_timestamp", "")
	if afterStr != "" {
		t, err := m.parseTimestamp(afterStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid after_timestamp: %v", err)), nil
		}
		afterTime = &t
	}

	// get optional sender filter
	senderJID := request.GetString("from", "")

	// query database
	var messages []storage.MessageWithNames

	if beforeTime != nil || afterTime != nil || senderJID != "" {
		// use new filtered method
		messages, err = m.store.GetChatMessagesWithNamesFiltered(
			chatJID,
			int(limit),
			beforeTime,
			afterTime,
			senderJID,
		)
	} else {
		// backward compatibility: use offset if no timestamp filters
		offset := request.GetFloat("offset", 0.0)
		messages, err = m.store.GetChatMessagesWithNames(chatJID, int(limit), int(offset))
	}

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get messages: %v", err)), nil
	}

	// format response
	var result strings.Builder
	fmt.Fprintf(&result, "Retrieved %d messages from chat %s", len(messages), chatJID)

	if senderJID != "" {
		fmt.Fprintf(&result, " (filtered by sender: %s)", senderJID)
	}
	if beforeTime != nil {
		fmt.Fprintf(&result, " (before: %s)", m.formatDateTime(*beforeTime))
	}
	if afterTime != nil {
		fmt.Fprintf(&result, " (after: %s)", m.formatDateTime(*afterTime))
	}
	result.WriteString(":\n\n")

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

		// show media metadata if present
		if msg.MediaMetadata != nil {
			meta := msg.MediaMetadata
			fmt.Fprintf(&result, "   📎 %s (%s, %s)",
				meta.FileName, meta.MimeType, formatFileSize(meta.FileSize))

			// add dimensions if available
			if dims := formatDimensions(meta.Width, meta.Height); dims != "" {
				fmt.Fprintf(&result, ", %s", dims)
			}

			// add duration if available
			if dur := formatDuration(meta.Duration); dur != "" {
				fmt.Fprintf(&result, ", %s", dur)
			}

			// show download status
			switch meta.DownloadStatus {
			case "downloaded":
				result.WriteString(" [Downloaded]")
				fmt.Fprintf(&result, "\n   Resource: whatsapp://media/%s", msg.ID)
			case "pending":
				result.WriteString(" [Not downloaded]")
			case "failed":
				result.WriteString(" [Download failed]")
			case "expired":
				result.WriteString(" [Expired]")
			}
			result.WriteString("\n")
		}
	}

	return mcp.NewToolResultText(result.String()), nil
}

// handleSearchMessages handles the search_messages tool request.
func (m *MCPServer) handleSearchMessages(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// get query (can be empty when using 'from' parameter)
	query := request.GetString("query", "")

	// get optional limit
	limit := request.GetFloat("limit", 50.0)
	if limit > 200 {
		limit = 200
	}

	// get optional sender filter
	senderJID := request.GetString("from", "")

	// validate: must have either query or from
	if query == "" && senderJID == "" {
		return mcp.NewToolResultError("must provide either 'query' (text to search) or 'from' (sender JID) or both"), nil
	}

	// detect pattern type
	useGlob := detectPatternType(query)

	// search database
	messages, err := m.store.SearchMessagesWithNamesFiltered(query, useGlob, senderJID, int(limit))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
	}

	// format response
	var result strings.Builder
	fmt.Fprintf(&result, "Found %d messages matching '%s'", len(messages), query)
	if senderJID != "" {
		fmt.Fprintf(&result, " from sender %s", senderJID)
	}
	if useGlob {
		result.WriteString(" (using pattern matching)")
	}
	result.WriteString(":\n\n")

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
		fmt.Fprintf(&result, "   %s\n", msg.Text)

		// show media metadata if present
		if msg.MediaMetadata != nil {
			meta := msg.MediaMetadata
			fmt.Fprintf(&result, "   📎 %s (%s, %s)",
				meta.FileName, meta.MimeType, formatFileSize(meta.FileSize))

			// add dimensions if available
			if dims := formatDimensions(meta.Width, meta.Height); dims != "" {
				fmt.Fprintf(&result, ", %s", dims)
			}

			// add duration if available
			if dur := formatDuration(meta.Duration); dur != "" {
				fmt.Fprintf(&result, ", %s", dur)
			}

			// show download status
			switch meta.DownloadStatus {
			case "downloaded":
				result.WriteString(" [Downloaded]")
				fmt.Fprintf(&result, "\n   Resource: whatsapp://media/%s", msg.ID)
			case "pending":
				result.WriteString(" [Not downloaded]")
			case "failed":
				result.WriteString(" [Download failed]")
			case "expired":
				result.WriteString(" [Expired]")
			}
			result.WriteString("\n")
		}

		result.WriteString("\n")
	}

	return mcp.NewToolResultText(result.String()), nil
}

// handleFindChat handles the find_chat tool request.
func (m *MCPServer) handleFindChat(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// get required search parameter
	search, err := request.RequireString("search")
	if err != nil {
		return mcp.NewToolResultError("search parameter is required"), nil
	}

	// detect pattern type
	useGlob := detectPatternType(search)

	// search chats in database
	chats, err := m.store.SearchChatsFiltered(search, useGlob, 100)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to search chats: %v", err)), nil
	}

	// format response
	var result strings.Builder
	fmt.Fprintf(&result, "Found %d matching chats", len(chats))
	if useGlob {
		result.WriteString(" (using pattern matching)")
	}
	result.WriteString(":\n\n")

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

// handleSendMessage handles the send_message tool request.
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

// handleLoadMoreMessages handles the load_more_messages tool request.
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

			// show media metadata if present
			if msg.MediaMetadata != nil {
				meta := msg.MediaMetadata
				fmt.Fprintf(&result, "   📎 %s (%s, %s)",
					meta.FileName, meta.MimeType, formatFileSize(meta.FileSize))

				// add dimensions if available
				if dims := formatDimensions(meta.Width, meta.Height); dims != "" {
					fmt.Fprintf(&result, ", %s", dims)
				}

				// add duration if available
				if dur := formatDuration(meta.Duration); dur != "" {
					fmt.Fprintf(&result, ", %s", dur)
				}

				// show download status
				switch meta.DownloadStatus {
				case "downloaded":
					result.WriteString(" [Downloaded]")
				case "pending":
					result.WriteString(" [Not downloaded]")
				case "failed":
					result.WriteString(" [Download failed]")
				case "expired":
					result.WriteString(" [Expired]")
				}
				result.WriteString("\n")
			}
		}
	} else {
		fmt.Fprintf(&result, "History sync request sent for chat %s (%d messages). Messages will load in the background. Use get_chat_messages to see them once loaded.", chatJID, count)
	}

	return mcp.NewToolResultText(result.String()), nil
}

// handleSendFile handles the send_file tool request.
func (m *MCPServer) handleSendFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return mcp.NewToolResultError("chat_jid parameter is required"), nil
	}
	filePath, err := request.RequireString("file_path")
	if err != nil {
		return mcp.NewToolResultError("file_path parameter is required"), nil
	}
	caption := request.GetString("caption", "")

	if !m.wa.IsLoggedIn() {
		return mcp.NewToolResultError("WhatsApp is not connected"), nil
	}

	messageID, err := m.wa.SendFile(ctx, chatJID, filePath, caption)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to send file: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf(
		"File sent to %s (message_id: %s)", chatJID, messageID,
	)), nil
}

// handleSendAudioMessage handles the send_audio_message tool request.
func (m *MCPServer) handleSendAudioMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return mcp.NewToolResultError("chat_jid parameter is required"), nil
	}
	audioPath, err := request.RequireString("audio_path")
	if err != nil {
		return mcp.NewToolResultError("audio_path parameter is required"), nil
	}

	if !m.wa.IsLoggedIn() {
		return mcp.NewToolResultError("WhatsApp is not connected"), nil
	}

	messageID, err := m.wa.SendAudioMessage(ctx, chatJID, audioPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to send voice note: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf(
		"Voice note sent to %s (message_id: %s)", chatJID, messageID,
	)), nil
}

// handleSendReaction handles the send_reaction tool request.
func (m *MCPServer) handleSendReaction(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return mcp.NewToolResultError("chat_jid parameter is required"), nil
	}
	messageID, err := request.RequireString("message_id")
	if err != nil {
		return mcp.NewToolResultError("message_id parameter is required"), nil
	}
	// emoji is required even when empty (for explicit removal). RequireString
	// rejects unset, but allows the empty string as a deliberate signal.
	emoji, err := request.RequireString("emoji")
	if err != nil {
		return mcp.NewToolResultError("emoji parameter is required (pass empty string to remove a reaction)"), nil
	}
	senderJID := request.GetString("sender_jid", "")

	if !m.wa.IsLoggedIn() {
		return mcp.NewToolResultError("WhatsApp is not connected"), nil
	}

	if err := m.wa.SendReaction(ctx, chatJID, messageID, senderJID, emoji); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to send reaction: %v", err)), nil
	}

	if emoji == "" {
		return mcp.NewToolResultText(fmt.Sprintf("Reaction removed from message %s", messageID)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Reacted %s to message %s", emoji, messageID)), nil
}

// handleEditMessage handles the edit_message tool request.
func (m *MCPServer) handleEditMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return mcp.NewToolResultError("chat_jid parameter is required"), nil
	}
	messageID, err := request.RequireString("message_id")
	if err != nil {
		return mcp.NewToolResultError("message_id parameter is required"), nil
	}
	newText, err := request.RequireString("new_text")
	if err != nil {
		return mcp.NewToolResultError("new_text parameter is required"), nil
	}

	if !m.wa.IsLoggedIn() {
		return mcp.NewToolResultError("WhatsApp is not connected"), nil
	}

	if err := m.wa.EditMessage(ctx, chatJID, messageID, newText); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to edit message: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Message %s edited", messageID)), nil
}

// handleDeleteMessage handles the delete_message tool request.
func (m *MCPServer) handleDeleteMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return mcp.NewToolResultError("chat_jid parameter is required"), nil
	}
	messageID, err := request.RequireString("message_id")
	if err != nil {
		return mcp.NewToolResultError("message_id parameter is required"), nil
	}
	senderJID := request.GetString("sender_jid", "")

	if !m.wa.IsLoggedIn() {
		return mcp.NewToolResultError("WhatsApp is not connected"), nil
	}

	if err := m.wa.DeleteMessage(ctx, chatJID, messageID, senderJID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to delete message: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Message %s deleted for everyone", messageID)), nil
}

// handleGetMyInfo handles the get_my_info tool request.
func (m *MCPServer) handleGetMyInfo(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// check WhatsApp connection
	if !m.wa.IsLoggedIn() {
		return mcp.NewToolResultError("WhatsApp is not connected"), nil
	}

	// get user info
	myInfo, err := m.wa.GetMyInfo(ctx)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get user info: %v", err)), nil
	}

	// format response
	var result strings.Builder
	fmt.Fprintf(&result, "Your WhatsApp Profile:\n\n")
	fmt.Fprintf(&result, "JID: %s\n", myInfo.JID)

	if myInfo.PushName != "" {
		fmt.Fprintf(&result, "Display Name: %s\n", myInfo.PushName)
	}

	if myInfo.Status != "" {
		fmt.Fprintf(&result, "Status/Bio: %s\n", myInfo.Status)
	} else {
		fmt.Fprintf(&result, "Status/Bio: (not set)\n")
	}

	if myInfo.BusinessName != "" {
		fmt.Fprintf(&result, "Business Name: %s\n", myInfo.BusinessName)
	}

	if myInfo.PictureURL != "" {
		fmt.Fprintf(&result, "\nProfile Picture:\n")
		fmt.Fprintf(&result, "  Picture ID: %s\n", myInfo.PictureID)
		fmt.Fprintf(&result, "  URL: %s\n", myInfo.PictureURL)
	} else {
		fmt.Fprintf(&result, "\nProfile Picture: (not set)\n")
	}

	return mcp.NewToolResultText(result.String()), nil
}

// handleTranscribeAudioMessage handles the transcribe_audio_message tool request.
// Decoupled from the MCP RPC ctx so whisper isn't killed by the upstream
// proxy's request timeout (mcpproxy defaults to ~30s, whisper-cli with the
// small model can run 60-120s on longer voice notes). Configurable via
// TRANSCRIBE_TIMEOUT_SECONDS (default 300s).
func (m *MCPServer) handleTranscribeAudioMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	messageID, err := request.RequireString("message_id")
	if err != nil {
		return mcp.NewToolResultError("message_id parameter is required"), nil
	}

	transcribeTimeout := time.Duration(config.GetEnvInt64("TRANSCRIBE_TIMEOUT_SECONDS", 300)) * time.Second
	transcribeCtx, cancel := context.WithTimeout(context.Background(), transcribeTimeout)
	defer cancel()

	transcript, err := m.wa.TranscribeMessage(transcribeCtx, messageID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to transcribe %s: %v", messageID, err)), nil
	}

	if transcript == "" {
		return mcp.NewToolResultText(fmt.Sprintf("Message %s transcribed but produced no text (silent audio?)", messageID)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Transcript of message %s:\n\n%s", messageID, transcript)), nil
}

// handleTranscribeAudiosBatch transcribes many audio messages concurrently.
// Accepts either an explicit message_ids array or a chat_jid (+ optional limit)
// to auto-collect the chat's audio/voice-note messages. Returns partial results:
// per-message transcripts plus a failures section. Bounded by maxBatchTranscribe.
func (m *MCPServer) handleTranscribeAudiosBatch(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()

	var ids []string
	if raw, ok := args["message_ids"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				ids = append(ids, strings.TrimSpace(s))
			}
		}
	}

	chatJID := request.GetString("chat_jid", "")
	limit := 50
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}

	if len(ids) == 0 && chatJID != "" {
		collected, err := m.wa.CollectAudioMessageIDs(chatJID, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to collect audios for chat %s: %v", chatJID, err)), nil
		}
		ids = collected
	}

	if len(ids) == 0 {
		return mcp.NewToolResultError("provide either 'message_ids' (array) or 'chat_jid' with at least one audio message"), nil
	}
	if len(ids) > maxBatchTranscribe {
		ids = ids[:maxBatchTranscribe]
	}

	// Generous timeout: bounded by item count, capped. Per-item HTTP has its own 120s.
	batchTimeout := time.Duration(300+len(ids)*10) * time.Second
	if batchTimeout > 15*time.Minute {
		batchTimeout = 15 * time.Minute
	}
	bctx, cancel := context.WithTimeout(context.Background(), batchTimeout)
	defer cancel()

	items := m.wa.TranscribeBatch(bctx, ids)

	var ok, failed int
	var body, failures strings.Builder
	for _, it := range items {
		if it.Error != "" {
			failed++
			fmt.Fprintf(&failures, "  [%s] %s\n", it.MessageID, it.Error)
			continue
		}
		ok++
		text := it.Text
		if text == "" {
			text = "(no text — silent audio?)"
		}
		fmt.Fprintf(&body, "[%s]\n%s\n\n", it.MessageID, text)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "Transcribed %d/%d audios (failures: %d).\n\n", ok, len(items), failed)
	out.WriteString(body.String())
	if failed > 0 {
		out.WriteString("Failures:\n")
		out.WriteString(failures.String())
	}
	return mcp.NewToolResultText(out.String()), nil
}

// handleDownloadMedia handles on-demand media downloads for messages whose
// auto-download was skipped, failed, or where the on-disk file was deleted.
// Falls back to CDN fetch + decrypt using media_key/direct_path stored in the DB.
func (m *MCPServer) handleDownloadMedia(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	messageID, err := request.RequireString("message_id")
	if err != nil {
		return mcp.NewToolResultError("message_id parameter is required"), nil
	}
	force := request.GetBool("force", false)

	result, err := m.wa.EnsureMediaDownloaded(ctx, messageID, force)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("download failed for %s: %v", messageID, err)), nil
	}

	var sb strings.Builder
	if result.WasAlreadyOnDisk {
		fmt.Fprintf(&sb, "Media %s already on disk (status was '%s').\n", messageID, result.ExistingStatus)
	} else {
		fmt.Fprintf(&sb, "Media %s downloaded (was '%s', %d bytes written).\n",
			messageID, result.ExistingStatus, result.BytesWritten)
	}
	fmt.Fprintf(&sb, "File path (relative): %s\n", result.FilePath)
	fmt.Fprintf(&sb, "Absolute path: %s\n", result.AbsolutePath)
	fmt.Fprintf(&sb, "MIME type: %s\n", result.ResolvedMimeType)

	// For images, return the bytes INLINE so the model can actually see the image,
	// not just a path it can't open. Guarded by size to protect the context budget.
	if strings.HasPrefix(result.ResolvedMimeType, "image/") {
		if data, rerr := os.ReadFile(result.AbsolutePath); rerr == nil && len(data) > 0 {
			if len(data) <= maxInlineImageBytes {
				b64 := base64.StdEncoding.EncodeToString(data)
				fmt.Fprintf(&sb, "(image returned inline, %d bytes)", len(data))
				return mcp.NewToolResultImage(sb.String(), b64, result.ResolvedMimeType), nil
			}
			fmt.Fprintf(&sb, "(image too large to inline: %d bytes > %d cap; read from the path above)", len(data), maxInlineImageBytes)
		}
	}
	return mcp.NewToolResultText(sb.String()), nil
}

// handleFlushMediaCache deletes downloaded media files matching the filter
// and resets their download_status so they can be re-fetched on demand.
func (m *MCPServer) handleFlushMediaCache(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	filter := whatsapp.FlushFilter{
		ChatJID:    request.GetString("chat_jid", ""),
		MediaType:  request.GetString("media_type", ""),
		DryRun:     request.GetBool("dry_run", false),
		ResetState: request.GetBool("reset_state", true),
	}
	if before := request.GetString("before_date", ""); before != "" {
		t, err := m.parseTimestamp(before)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid before_date: %v", err)), nil
		}
		filter.BeforeDate = t
	}

	result, err := m.wa.FlushMediaCache(ctx, filter)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("flush failed: %v", err)), nil
	}

	var sb strings.Builder
	if result.DryRun {
		fmt.Fprintf(&sb, "DRY RUN — no files deleted.\n")
	}
	fmt.Fprintf(&sb, "Files removed: %d\n", result.FilesRemoved)
	fmt.Fprintf(&sb, "Bytes freed: %s (%d bytes)\n", formatFileSize(result.BytesFreed), result.BytesFreed)
	fmt.Fprintf(&sb, "DB rows reset (download_status='skipped'): %d\n", result.DBRowsUpdated)
	if len(result.SampleRemoved) > 0 {
		fmt.Fprintf(&sb, "Sample paths (%d shown):\n", len(result.SampleRemoved))
		for _, p := range result.SampleRemoved {
			fmt.Fprintf(&sb, "  - %s\n", p)
		}
	}
	if !result.DryRun && result.DBRowsUpdated > 0 {
		fmt.Fprintf(&sb, "\nNote: media_metadata rows kept (media_key/direct_path preserved). Use download_media to re-fetch any of these on demand while the CDN URL is still valid.\n")
	}
	return mcp.NewToolResultText(sb.String()), nil
}
