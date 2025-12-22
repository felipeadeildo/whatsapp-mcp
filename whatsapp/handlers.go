package whatsapp

import (
	"context"
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
		// QR codes are handled externally via GetQRChannel
	case *events.PairSuccess:
		c.log.Infof("Successfully paired device")
	case *events.GroupInfo:
		c.handleGroupInfo(v)
	}
}

// extractJIDPair extracts both PN and LID representations from JID objects
// returns (pnPtr, lidPtr) for storage
// for groups (@g.us), stores group JID in PN column, LID as nil
// ALWAYS returns at least one non-nil value to satisfy CHECK constraint
func (c *Client) extractJIDPair(canonical types.JID, alternative types.JID) (*string, *string) {
	canonicalStr := canonical.String()

	// groups don't have PN/LID variations - store group JID as PN
	if canonical.Server == "g.us" || canonical.Server == "broadcast" {
		groupJID := canonicalStr
		return &groupJID, nil
	}

	var pnJID, lidJID *string

	if strings.HasSuffix(canonicalStr, "@s.whatsapp.net") {
		// canonical is PN format
		pn := canonicalStr
		pnJID = &pn
		if alternative.User != "" {
			lid := alternative.String()
			lidJID = &lid
		}
	} else if strings.HasSuffix(canonicalStr, "@lid") {
		// canonical is LID format
		lid := canonicalStr
		lidJID = &lid
		if alternative.User != "" {
			pn := alternative.String()
			pnJID = &pn
		}
	} else if canonicalStr != "" {
		// for unknown formats (newsletters, status, etc), store in PN column as fallback
		// this ensures at least one JID is non-nil to satisfy CHECK constraint
		fallback := canonicalStr
		pnJID = &fallback
	}

	return pnJID, lidJID
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

	// extract sender JID with alternatives using Info.SenderAlt
	senderPN, senderLID := c.extractJIDPair(info.Sender, info.SenderAlt)

	// extract chat JID with alternatives
	// for DMs, check if we have RecipientAlt (not always available)
	chatPN, chatLID := c.extractJIDPair(info.Chat, types.EmptyJID)

	// save/update chat BEFORE saving message (for foreign key constraint)
	isGroup := info.Chat.Server == "g.us"
	chatName := ""

	// for DMs, use sender's push name as chat name
	if !isGroup && info.PushName != "" && !info.IsFromMe {
		chatName = info.PushName
	} else if isGroup {
		// for groups, fetch group info to get the name
		ctx := context.Background()
		groupInfo, err := c.wa.GetGroupInfo(ctx, info.Chat)
		if err != nil {
			c.log.Debugf("Failed to get group info for %s: %v", info.Chat, err)
		} else {
			chatName = groupInfo.Name
		}
	}

	chat := storage.Chat{
		JIDPN:           chatPN,
		JIDLID:          chatLID,
		Name:            chatName,
		LastMessageTime: info.Timestamp,
		IsGroup:         isGroup,
	}

	if err := c.store.SaveChat(chat); err != nil {
		c.log.Errorf("Failed to save chat (PN=%v, LID=%v, IsFromMe=%v): %v",
			chatPN, chatLID, info.IsFromMe, err)
		return
	}

	msg := storage.Message{
		ID:           info.ID,
		ChatJIDPN:    chatPN,
		ChatJIDLID:   chatLID,
		SenderJIDPN:  senderPN,
		SenderJIDLID: senderLID,
		SenderName:   info.PushName,
		Text:         text,
		Timestamp:    info.Timestamp,
		IsFromMe:     info.IsFromMe,
		MessageType:  msgType,
	}

	if err := c.store.SaveMessage(msg); err != nil {
		c.log.Errorf("Failed to save message (ID=%s, ChatPN=%v, ChatLID=%v, IsFromMe=%v): %v",
			info.ID, chatPN, chatLID, info.IsFromMe, err)
		return
	}

	c.log.Infof("Saved message: %s", info.ID)
}

// handle group info updates (name, topic, settings changes)
func (c *Client) handleGroupInfo(evt *events.GroupInfo) {
	// update group name if changed
	if evt.Name != nil {
		groupPN, groupLID := c.extractJIDPair(evt.JID, types.EmptyJID)

		chat := storage.Chat{
			JIDPN:           groupPN,
			JIDLID:          groupLID,
			Name:            evt.Name.Name,
			LastMessageTime: evt.Timestamp,
			IsGroup:         true,
		}

		if err := c.store.SaveChat(chat); err != nil {
			c.log.Errorf("Failed to update group name: %v", err)
			return
		}

		c.log.Infof("Updated group name: %s -> %s", evt.JID, evt.Name.Name)
	}
}

func (c *Client) handleHistorySync(evt *events.HistorySync) {
	c.log.Infof("History sync: %d conversations", len(evt.Data.GetConversations()))

	ctx := context.Background()

	// create a map of JID to push name for quick lookup
	pushNameMap := make(map[string]string)
	for _, pushname := range evt.Data.GetPushnames() {
		if pushname.GetPushname() != "" && pushname.GetPushname() != "-" {
			pushNameMap[pushname.GetID()] = pushname.GetPushname()
		}
	}

	var allMessages []storage.Message
	chatMap := make(map[string]*storage.Chat) // track chats by canonical JID

	for _, conv := range evt.Data.GetConversations() {
		chatJIDObject, err := types.ParseJID(conv.GetID())
		if err != nil {
			c.log.Errorf("Failed to parse JID: %v", err)
			continue
		}

		// get alternative JID for chat (if not a group)
		var chatAltJID types.JID
		if chatJIDObject.Server != "g.us" {
			chatAltJID, err = c.wa.Store.GetAltJID(ctx, chatJIDObject)
			if err != nil {
				c.log.Debugf("No alt JID for chat %s: %v", chatJIDObject, err)
			}
		}

		chatPN, chatLID := c.extractJIDPair(chatJIDObject, chatAltJID)
		isGroup := chatJIDObject.Server == "g.us"

		// fetch group name if this is a group
		var groupName string
		if isGroup {
			groupInfo, err := c.wa.GetGroupInfo(ctx, chatJIDObject)
			if err != nil {
				c.log.Debugf("Failed to get group info for %s: %v", chatJIDObject, err)
			} else {
				groupName = groupInfo.Name
			}
		}

		c.log.Infof("Processing chat: %s with %d messages",
			chatJIDObject.String(), len(conv.GetMessages()))

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

			// determine sender JID object
			var senderJIDObject types.JID
			if fromMe {
				senderJIDObject = *c.wa.Store.ID
			} else if key.GetParticipant() != "" {
				senderJIDObject, _ = types.ParseJID(key.GetParticipant())
			} else {
				// DM
				senderJIDObject, _ = types.ParseJID(key.GetRemoteJID())
			}

			// get alternative JID for sender
			var senderAltJID types.JID
			if senderJIDObject.Server != "g.us" {
				senderAltJID, err = c.wa.Store.GetAltJID(ctx, senderJIDObject)
				if err != nil {
					c.log.Debugf("No alt JID for sender %s: %v", senderJIDObject, err)
				}
			}

			senderPN, senderLID := c.extractJIDPair(senderJIDObject, senderAltJID)

			// get push name from WebMessageInfo or from pushNameMap
			senderName := msg.GetPushName()
			if senderName == "" {
				senderName = pushNameMap[senderJIDObject.String()]
			}

			// track ALL chats (for foreign key constraint)
			// use first PN or LID as key
			chatKey := ""
			if chatPN != nil {
				chatKey = *chatPN
			} else if chatLID != nil {
				chatKey = *chatLID
			}

			if chatKey != "" {
				// check if chat already exists in map
				existingChat, exists := chatMap[chatKey]
				if exists {
					// update last message time if this message is newer
					if timestamp.After(existingChat.LastMessageTime) {
						existingChat.LastMessageTime = timestamp
					}
					// update name if we have a name and existing doesn't
					if existingChat.Name == "" {
						if isGroup && groupName != "" {
							existingChat.Name = groupName
						} else if !isGroup && senderName != "" && !fromMe {
							existingChat.Name = senderName
						}
					}
				} else {
					// create new chat entry
					chatName := ""
					if isGroup {
						// use group name fetched from API
						chatName = groupName
					} else if senderName != "" && !fromMe {
						// for DMs, use sender's push name
						chatName = senderName
					}
					chatMap[chatKey] = &storage.Chat{
						JIDPN:           chatPN,
						JIDLID:          chatLID,
						Name:            chatName,
						LastMessageTime: timestamp,
						IsGroup:         isGroup,
					}
				}
			}

			text := extractText(msg.GetMessage())
			if text == "" {
				text = "[Media or unknown]"
			}

			msgType := getMessageType(msg.GetMessage())

			allMessages = append(allMessages, storage.Message{
				ID:           messageID,
				ChatJIDPN:    chatPN,
				ChatJIDLID:   chatLID,
				SenderJIDPN:  senderPN,
				SenderJIDLID: senderLID,
				SenderName:   senderName,
				Text:         text,
				Timestamp:    timestamp,
				IsFromMe:     fromMe,
				MessageType:  msgType,
			})
		}
	}

	// save chats BEFORE messages (for foreign key constraint)
	if len(chatMap) > 0 {
		c.log.Infof("Updating %d chat names from history sync", len(chatMap))
		for _, chat := range chatMap {
			if err := c.store.SaveChat(*chat); err != nil {
				c.log.Warnf("Failed to update chat (PN=%v, LID=%v): %v",
					chat.JIDPN, chat.JIDLID, err)
			}
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
