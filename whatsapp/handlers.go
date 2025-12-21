package whatsapp

import (
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-mcp/storage"
)

// process all whatsapp events
func (c *Client) eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		c.handleMessage(v)
	case *events.HistorySync:
		c.handleHistorySync(v)
	case *events.Connected:
		c.log.Infof("Connected to WhatsApp")
	case *events.Disconnected:
		c.log.Warnf("Disconnected from WhatsApp")
	case *events.QR:
		// QR codes são tratados externamente via GetQRChannel
	case *events.PairSuccess:
		c.log.Infof("Successfully paired device")
	}
}

// process incoming messages
func (c *Client) handleMessage(evt *events.Message) {
	info := evt.Info

	c.log.Debugf("Received message: %s from %s in %s",
		info.ID, info.Sender, info.Chat)

	text := extractText(evt.Message)
	if text == "" && evt.Message.GetImageMessage() != nil {
		text = "[Image]"
	} else if text == "" && evt.Message.GetVideoMessage() != nil {
		text = "[Video]"
	} else if text == "" && evt.Message.GetAudioMessage() != nil {
		text = "[Audio]"
	} else if text == "" && evt.Message.GetDocumentMessage() != nil {
		text = "[Document]"
	} else if text == "" {
		text = "[Unknown message type]"
	}

	msgType := getMessageType(evt.Message)

	msg := storage.Message{
		ID:          info.ID,
		ChatJID:     info.Chat.String(),
		SenderJID:   info.Sender.String(),
		SenderName:  info.PushName,
		Text:        text,
		Timestamp:   info.Timestamp,
		IsFromMe:    info.IsFromMe,
		MessageType: msgType,
	}

	if err := c.store.SaveMessage(msg); err != nil {
		c.log.Errorf("Failed to save message: %v", err)
		return
	}

	// Update chat name if we have a PushName
	// For incoming messages (not from me), use the sender's push name
	// For self-messages (to myself), also update with my own push name
	if info.PushName != "" {
		chatName := info.PushName
		isGroup := info.Chat.Server == "g.us"

		// Only update chat name for DMs or self-chats, not for groups
		// (groups have their own names)
		if !isGroup {
			chat := storage.Chat{
				JID:             info.Chat.String(),
				Name:            chatName,
				LastMessageTime: info.Timestamp,
				IsGroup:         false,
			}

			if err := c.store.SaveChat(chat); err != nil {
				c.log.Warnf("Failed to update chat name: %v", err)
			}
		}
	}

	c.log.Infof("Saved message: %s", info.ID)
}

func (c *Client) handleHistorySync(evt *events.HistorySync) {
	c.log.Infof("History sync: %d conversations", len(evt.Data.GetConversations()))

	// Create a map of JID to push name for quick lookup
	pushNameMap := make(map[string]string)
	for _, pushname := range evt.Data.GetPushnames() {
		if pushname.GetPushname() != "" && pushname.GetPushname() != "-" {
			pushNameMap[pushname.GetID()] = pushname.GetPushname()
		}
	}

	var allMessages []storage.Message
	chatNamesMap := make(map[string]string) // Track latest name per chat

	for _, conv := range evt.Data.GetConversations() {
		chatJIDObject, err := types.ParseJID(conv.GetID())
		if err != nil {
			c.log.Errorf("Failed to parse JID: %v", err)
			continue
		}

		chatJID := chatJIDObject.String()
		isGroup := chatJIDObject.Server == "g.us"

		c.log.Infof("Processing chat: %s with %d messages",
			chatJID, len(conv.GetMessages()))

		for _, histMsg := range conv.GetMessages() {
			msg := histMsg.GetMessage()
			if msg == nil {
				continue
			}

			key := msg.GetKey()
			if key == nil {
				continue
			}

			messageID := key.GetID()
			fromMe := key.GetFromMe()
			timestamp := time.Unix(int64(msg.GetMessageTimestamp()), 0)

			// sender (who sent the message)
			var senderJID string
			if fromMe {
				senderJID = c.wa.Store.GetJID().String()
			} else if key.GetParticipant() != "" {
				senderJID = key.GetParticipant()
			} else {
				// DM
				senderJID = key.GetRemoteJID()
			}

			// Get push name from WebMessageInfo or from pushNameMap
			senderName := msg.GetPushName()
			if senderName == "" {
				// Try to get from the pushNameMap using sender JID
				senderName = pushNameMap[senderJID]
			}

			// For DMs (not groups), update the chat name with the contact's push name
			if !isGroup && senderName != "" && !fromMe {
				chatNamesMap[chatJID] = senderName
			}

			text := extractText(msg.GetMessage())
			if text == "" {
				text = "[Media or unknown]"
			}

			msgType := getMessageType(msg.GetMessage())

			allMessages = append(allMessages, storage.Message{
				ID:          messageID,
				ChatJID:     chatJID,
				SenderJID:   senderJID,
				SenderName:  senderName,
				Text:        text,
				Timestamp:   timestamp,
				IsFromMe:    fromMe,
				MessageType: msgType,
			})
		}
	}

	if len(allMessages) > 0 {
		c.log.Infof("Saving %d messages from history sync", len(allMessages))

		if err := c.store.SaveBulk(allMessages); err != nil {
			c.log.Errorf("Failed to save bulk messages: %v", err)
			return
		}

		c.log.Infof("Successfully saved %d messages", len(allMessages))
	}

	// Update chat names for DMs
	if len(chatNamesMap) > 0 {
		c.log.Infof("Updating %d chat names from history sync", len(chatNamesMap))
		for chatJID, chatName := range chatNamesMap {
			chat := storage.Chat{
				JID:     chatJID,
				Name:    chatName,
				IsGroup: false,
			}
			if err := c.store.SaveChat(chat); err != nil {
				c.log.Warnf("Failed to update chat name for %s: %v", chatJID, err)
			}
		}
	}
}

func extractText(msg interface{}) string {
	type conversationGetter interface {
		GetConversation() string
	}

	if conv, ok := msg.(conversationGetter); ok {
		if text := conv.GetConversation(); text != "" {
			return text
		}
	}

	type captionGetter interface {
		GetCaption() string
	}

	type imageGetter interface {
		GetImageMessage() captionGetter
	}
	if img, ok := msg.(imageGetter); ok {
		if imgMsg := img.GetImageMessage(); imgMsg != nil {
			return imgMsg.GetCaption()
		}
	}

	type videoGetter interface {
		GetVideoMessage() captionGetter
	}
	if vid, ok := msg.(videoGetter); ok {
		if vidMsg := vid.GetVideoMessage(); vidMsg != nil {
			return vidMsg.GetCaption()
		}
	}

	type extendedTextGetter interface {
		GetExtendedTextMessage() interface{ GetText() string }
	}
	if ext, ok := msg.(extendedTextGetter); ok {
		if extMsg := ext.GetExtendedTextMessage(); extMsg != nil {
			return extMsg.GetText()
		}
	}

	return ""
}

func getMessageType(msg interface{}) string {
	msgStr := fmt.Sprintf("%T", msg)

	if strings.Contains(msgStr, "Conversation") {
		return "text"
	} else if strings.Contains(msgStr, "ImageMessage") {
		return "image"
	} else if strings.Contains(msgStr, "VideoMessage") {
		return "video"
	} else if strings.Contains(msgStr, "AudioMessage") {
		return "audio"
	} else if strings.Contains(msgStr, "DocumentMessage") {
		return "document"
	}

	return "unknown"
}
