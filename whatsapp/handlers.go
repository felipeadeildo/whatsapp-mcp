package whatsapp

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
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
	case *events.Contact:
		c.handleContact(v)
	case *events.PushName:
		c.handlePushName(v)
	case *events.Connected:
		c.log.Infof("Connected to WhatsApp (JID: %s)", c.wa.Store.ID)
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

// normalizeJID converts any JID to canonical string format
// groups/broadcasts/newsletters return as-is
// user JIDs are normalized to non-AD format
func (c *Client) normalizeJID(jid types.JID) string {
	if jid.IsEmpty() {
		return ""
	}

	// groups, broadcasts, and newsletters don't have PN/LID variations
	if jid.Server == "g.us" || jid.Server == "broadcast" || jid.Server == "newsletter" {
		return jid.String()
	}

	// for user JIDs, normalize to non-AD format
	// this handles both PN (@s.whatsapp.net) and LID (@lid) formats
	return jid.ToNonAD().String()
}

// messageData holds parsed message information for processing
type messageData struct {
	MessageID   string
	ChatJID     types.JID
	SenderJID   types.JID
	Text        string
	Timestamp   time.Time
	IsFromMe    bool
	MessageType string
	PushName    string // sender's WhatsApp display name from message
	IsGroup     bool
}

// fetches group info with database caching to avoid excessive API calls
func (c *Client) getGroupInfoCached(ctx context.Context, groupJID types.JID) (string, error) {
	// try to load from database first
	chatJID := c.normalizeJID(groupJID)
	existingChat, err := c.store.GetChatByJID(chatJID)
	if err == nil && existingChat != nil && existingChat.PushName != "" {
		// use cached name
		c.log.Debugf("Using cached group name for %s: %s", groupJID, existingChat.PushName)
		return existingChat.PushName, nil
	}

	// fetch from API if not cached or empty
	groupInfo, err := c.wa.GetGroupInfo(ctx, groupJID)
	if err != nil {
		return "", err
	}

	return groupInfo.Name, nil
}

// gets sender's WhatsApp display name with fallbacks
// priority: PushName from message > Contact store (for groups only)
func (c *Client) getSenderPushName(ctx context.Context, senderJID types.JID, messagePushName string, isGroup bool, isFromMe bool) string {
	if isFromMe {
		return ""
	}

	if messagePushName != "" {
		return messagePushName
	}

	// for group messages, try contact store as fallback
	if isGroup && c.wa.Store.Contacts != nil {
		contactInfo, err := c.wa.Store.Contacts.GetContact(ctx, senderJID)
		if err == nil && contactInfo.Found {
			// priority: PushName > FullName > BusinessName
			if contactInfo.PushName != "" {
				return contactInfo.PushName
			} else if contactInfo.FullName != "" {
				return contactInfo.FullName
			} else if contactInfo.BusinessName != "" {
				return contactInfo.BusinessName
			}
		}
	}

	return ""
}

// gets chat display names (both push name and contact name for DMs)
func (c *Client) getChatInfo(ctx context.Context, chatJID types.JID, isGroup bool, messagePushName string) (pushName string, contactName string) {
	if isGroup {
		// for groups, fetch group name (with caching)
		groupName, err := c.getGroupInfoCached(ctx, chatJID)
		if err != nil {
			c.log.Debugf("Failed to get group info for %s: %v", chatJID, err)
			return "", ""
		}
		return groupName, ""
	}

	// for DMs, get contact name from contact store
	if c.wa.Store.Contacts != nil {
		contactInfo, err := c.wa.Store.Contacts.GetContact(ctx, chatJID)
		if err == nil && contactInfo.Found {
			// priority: FullName (saved contact) > FirstName > BusinessName
			if contactInfo.FullName != "" {
				contactName = contactInfo.FullName
			} else if contactInfo.FirstName != "" {
				contactName = contactInfo.FirstName
			} else if contactInfo.BusinessName != "" {
				contactName = contactInfo.BusinessName
			}
		}
	}

	// for DMs, push name comes from the message (if not from me)
	if messagePushName != "" {
		pushName = messagePushName
	}

	return pushName, contactName
}

// handles the common logic for saving messages and chats
// returns error if save fails
func (c *Client) processMessageData(ctx context.Context, data messageData) error {
	// normalize JIDs to canonical format
	chatJID := c.normalizeJID(data.ChatJID)
	senderJID := c.normalizeJID(data.SenderJID)

	// get chat info (group name or DM contact/push names)
	chatPushName, chatContactName := c.getChatInfo(ctx, data.ChatJID, data.IsGroup, data.PushName)

	// save/update chat BEFORE message (for foreign key constraint)
	chat := storage.Chat{
		JID:             chatJID,
		PushName:        chatPushName,
		ContactName:     chatContactName,
		LastMessageTime: data.Timestamp,
		IsGroup:         data.IsGroup,
	}

	if err := c.store.SaveChat(chat); err != nil {
		c.log.Errorf("Failed to save chat %s: %v", chatJID, err)
		return err
	}

	// save message
	msg := storage.Message{
		ID:          data.MessageID,
		ChatJID:     chatJID,
		SenderJID:   senderJID,
		Text:        data.Text,
		Timestamp:   data.Timestamp,
		IsFromMe:    data.IsFromMe,
		MessageType: data.MessageType,
	}

	if err := c.store.SaveMessage(msg); err != nil {
		c.log.Errorf("Failed to save message %s in chat %s: %v",
			data.MessageID, chatJID, err)
		return err
	}

	// get and save sender push name
	senderPushName := c.getSenderPushName(ctx, data.SenderJID, data.PushName, data.IsGroup, data.IsFromMe)
	if senderPushName != "" {
		pushNames := map[string]string{data.SenderJID.String(): senderPushName}
		if err := c.store.SavePushNames(pushNames); err != nil {
			c.log.Debugf("Failed to save push name for %s: %v", data.SenderJID, err)
		}
	}

	c.log.Infof("Saved message %s from %s (IsFromMe=%v, Type=%s)",
		data.MessageID, data.SenderJID, data.IsFromMe, data.MessageType)

	return nil
}

// parses a WebMessageInfo from history sync into messageData
// returns nil if message cannot be parsed
func (c *Client) parseHistoryMessage(chatJID types.JID, msg *waWeb.WebMessageInfo, pushNameMap map[string]string) *messageData {
	// try ParseWebMessage first
	parsedMsg, parseErr := c.wa.ParseWebMessage(chatJID, msg)
	if parseErr == nil {
		// successfully parsed - use the parsed info
		info := parsedMsg.Info

		// get push name from parsed message or pushNameMap
		pushName := info.PushName
		if pushName == "" {
			pushName = pushNameMap[info.Sender.String()]
		}

		text := extractText(msg.GetMessage())
		if text == "" {
			text = "[Media or unknown]"
		}

		return &messageData{
			MessageID:   info.ID,
			ChatJID:     chatJID,
			SenderJID:   info.Sender,
			Text:        text,
			Timestamp:   info.Timestamp,
			IsFromMe:    info.IsFromMe,
			MessageType: getMessageType(msg.GetMessage()),
			PushName:    pushName,
			IsGroup:     chatJID.Server == "g.us",
		}
	}

	// fallback to manual parsing
	key := msg.GetKey()
	if key == nil {
		return nil
	}

	messageID := key.GetID()
	fromMe := key.GetFromMe()
	timestamp := time.Unix(int64(msg.GetMessageTimestamp()), 0)

	// determine sender JID
	var senderJID types.JID
	if fromMe {
		senderJID = *c.wa.Store.ID
	} else if key.GetParticipant() != "" {
		var err error
		senderJID, err = types.ParseJID(key.GetParticipant())
		if err != nil {
			c.log.Debugf("Failed to parse participant JID: %v", err)
			return nil
		}
	} else {
		// DM
		var err error
		senderJID, err = types.ParseJID(key.GetRemoteJID())
		if err != nil {
			c.log.Debugf("Failed to parse remote JID: %v", err)
			return nil
		}
	}

	// get push name from WebMessageInfo or from pushNameMap
	pushName := msg.GetPushName()
	if pushName == "" {
		pushName = pushNameMap[senderJID.String()]
	}

	text := extractText(msg.GetMessage())
	if text == "" {
		text = "[Media or unknown]"
	}

	return &messageData{
		MessageID:   messageID,
		ChatJID:     chatJID,
		SenderJID:   senderJID,
		Text:        text,
		Timestamp:   timestamp,
		IsFromMe:    fromMe,
		MessageType: getMessageType(msg.GetMessage()),
		PushName:    pushName,
		IsGroup:     chatJID.Server == "g.us",
	}
}

// process incoming messages
func (c *Client) handleMessage(evt *events.Message) {
	info := evt.Info
	ctx := context.Background()

	c.log.Debugf("Received message: %s from %s in %s",
		info.ID, info.Sender, info.Chat)

	text := extractText(evt.Message)
	if text == "" {
		if evt.Message.GetImageMessage() != nil {
			text = "[Image]"
		} else if evt.Message.GetVideoMessage() != nil {
			text = "[Video]"
		} else if evt.Message.GetAudioMessage() != nil {
			text = "[Audio]"
		} else if evt.Message.GetDocumentMessage() != nil {
			text = "[Document]"
		} else {
			text = "[Unknown message type]"
		}
	}

	data := messageData{
		MessageID:   info.ID,
		ChatJID:     info.Chat,
		SenderJID:   info.Sender,
		Text:        text,
		Timestamp:   info.Timestamp,
		IsFromMe:    info.IsFromMe,
		MessageType: getMessageType(evt.Message),
		PushName:    info.PushName,
		IsGroup:     info.Chat.Server == "g.us",
	}

	if err := c.processMessageData(ctx, data); err != nil {
		return
	}
}

// handles group info updates (name, topic, settings changes)
func (c *Client) handleGroupInfo(evt *events.GroupInfo) {
	// update group name if changed
	if evt.Name != nil {
		groupJID := c.normalizeJID(evt.JID)

		chat := storage.Chat{
			JID:             groupJID,
			PushName:        evt.Name.Name, // group name goes in PushName
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

// handles contact info updates from app state sync
func (c *Client) handleContact(evt *events.Contact) {
	c.log.Debugf("Contact info updated: %s (FullName: %s, FirstName: %s)",
		evt.JID, evt.Action.GetFullName(), evt.Action.GetFirstName())
	// contact info is automatically stored by whatsmeow in the contact store
	// no additional action needed - getChatInfo() will retrieve it
}

// handles push name updates
func (c *Client) handlePushName(evt *events.PushName) {
	c.log.Debugf("Push name updated: %s -> %s", evt.JID, evt.NewPushName)
	// push name is automatically stored by whatsmeow in the contact store
	// no additional action needed - getChatInfo() will retrieve it
}

func (c *Client) handleHistorySync(evt *events.HistorySync) {
	c.log.Infof("Starting history sync: %d conversations to process", len(evt.Data.GetConversations()))

	ctx := context.Background()

	pushNameMap, err := c.store.LoadAllPushNames()
	if err != nil {
		c.log.Errorf("Failed to load existing push names: %v", err)
		pushNameMap = make(map[string]string)
	}
	existingCount := len(pushNameMap)

	// add new push names from this HistorySync event
	newPushNames := make(map[string]string)
	for _, pushname := range evt.Data.GetPushnames() {
		if pushname.GetPushname() != "" && pushname.GetPushname() != "-" {
			jid := pushname.GetID()
			pushNameMap[jid] = pushname.GetPushname()
			newPushNames[jid] = pushname.GetPushname()
		}
	}

	// save new push names to database
	if len(newPushNames) > 0 {
		if err := c.store.SavePushNames(newPushNames); err != nil {
			c.log.Errorf("Failed to save push names to database: %v", err)
		} else {
			c.log.Infof("Saved %d new push names to database (total: %d)", len(newPushNames), len(pushNameMap))
		}
	} else {
		c.log.Infof("No new push names in this HistorySync event (using %d existing from database)", existingCount)
	}

	var allMessages []storage.Message
	chatMap := make(map[string]*storage.Chat)      // track chats by canonical JID
	additionalPushNames := make(map[string]string) // collect push names from messages

	for idx, conv := range evt.Data.GetConversations() {
		chatJID, err := types.ParseJID(conv.GetID())
		if err != nil {
			c.log.Errorf("Failed to parse JID: %v", err)
			continue
		}

		c.log.Infof("Processing chat [%d/%d]: %s (%d messages)",
			idx+1, len(evt.Data.GetConversations()),
			chatJID.String(), len(conv.GetMessages()))

		for _, histMsg := range conv.GetMessages() {
			msg := histMsg.GetMessage()
			if msg == nil {
				continue
			}

			// parse message using helper function
			msgData := c.parseHistoryMessage(chatJID, msg, pushNameMap)
			if msgData == nil {
				c.log.Debugf("Failed to parse message, skipping")
				continue
			}

			// normalize JIDs to canonical format
			normalizedChatJID := c.normalizeJID(msgData.ChatJID)
			normalizedSenderJID := c.normalizeJID(msgData.SenderJID)

			// collect push name for later saving
			if msgData.PushName != "" && !msgData.IsFromMe {
				additionalPushNames[msgData.SenderJID.String()] = msgData.PushName
			}

			// get enhanced sender push name (with contact fallback for groups)
			senderPushName := c.getSenderPushName(ctx, msgData.SenderJID, msgData.PushName, msgData.IsGroup, msgData.IsFromMe)
			if senderPushName != "" && !msgData.IsFromMe {
				additionalPushNames[msgData.SenderJID.String()] = senderPushName
			}

			// track chat for batch saving
			if normalizedChatJID != "" {
				existingChat, exists := chatMap[normalizedChatJID]
				if exists {
					// update last message time if newer
					if msgData.Timestamp.After(existingChat.LastMessageTime) {
						existingChat.LastMessageTime = msgData.Timestamp
					}
				} else {
					// create new chat entry (will be saved in batch later)
					chatPushName, chatContactName := c.getChatInfo(ctx, msgData.ChatJID, msgData.IsGroup, msgData.PushName)
					chatMap[normalizedChatJID] = &storage.Chat{
						JID:             normalizedChatJID,
						PushName:        chatPushName,
						ContactName:     chatContactName,
						LastMessageTime: msgData.Timestamp,
						IsGroup:         msgData.IsGroup,
					}
				}
			}

			// add message to batch
			allMessages = append(allMessages, storage.Message{
				ID:          msgData.MessageID,
				ChatJID:     normalizedChatJID,
				SenderJID:   normalizedSenderJID,
				Text:        msgData.Text,
				Timestamp:   msgData.Timestamp,
				IsFromMe:    msgData.IsFromMe,
				MessageType: msgData.MessageType,
			})
		}
	}

	// save chats BEFORE messages (for foreign key constraint)
	if len(chatMap) > 0 {
		c.log.Infof("Updating %d chat names from history sync", len(chatMap))
		for _, chat := range chatMap {
			if err := c.store.SaveChat(*chat); err != nil {
				c.log.Warnf("Failed to update chat %s: %v", chat.JID, err)
			}
		}
	}

	if len(allMessages) > 0 {
		c.log.Infof("Saving %d messages from history sync", len(allMessages))

		if err := c.store.SaveBulk(allMessages); err != nil {
			c.log.Errorf("Failed to save bulk messages: %v", err)
			return
		}

		c.log.Infof("History sync complete: %d chats updated, %d messages saved",
			len(chatMap), len(allMessages))
	}

	// save additional push names collected from messages
	if len(additionalPushNames) > 0 {
		if err := c.store.SavePushNames(additionalPushNames); err != nil {
			c.log.Errorf("Failed to save additional push names: %v", err)
		} else {
			c.log.Infof("Saved %d additional push names from messages", len(additionalPushNames))
		}
	}
}

func extractText(msg any) string {
	if msg == nil {
		return ""
	}

	// plain text conversation
	type conversationGetter interface {
		GetConversation() string
	}
	if conv, ok := msg.(conversationGetter); ok {
		if text := conv.GetConversation(); text != "" {
			return text
		}
	}

	type extendedTextGetter interface {
		GetExtendedTextMessage() interface{ GetText() string }
	}
	if ext, ok := msg.(extendedTextGetter); ok {
		if extMsg := ext.GetExtendedTextMessage(); extMsg != nil {
			if text := extMsg.GetText(); text != "" {
				return text
			}
		}
	}

	type imageGetter interface {
		GetImageMessage() interface{ GetCaption() string }
	}
	if img, ok := msg.(imageGetter); ok {
		if imgMsg := img.GetImageMessage(); imgMsg != nil {
			if caption := imgMsg.GetCaption(); caption != "" {
				return caption
			}
		}
	}

	type videoGetter interface {
		GetVideoMessage() interface{ GetCaption() string }
	}
	if vid, ok := msg.(videoGetter); ok {
		if vidMsg := vid.GetVideoMessage(); vidMsg != nil {
			if caption := vidMsg.GetCaption(); caption != "" {
				return caption
			}
		}
	}

	// document caption
	type documentGetter interface {
		GetDocumentMessage() interface{ GetCaption() string }
	}
	if doc, ok := msg.(documentGetter); ok {
		if docMsg := doc.GetDocumentMessage(); docMsg != nil {
			if caption := docMsg.GetCaption(); caption != "" {
				return caption
			}
		}
	}

	return ""
}

// returns the high-level message type (text, media, reaction, poll)
// based on whatsmeow's internal implementation
func getTypeFromMessage(msg *waE2E.Message) string {
	if msg == nil {
		return "unknown"
	}

	switch {
	case msg.ViewOnceMessage != nil:
		return getTypeFromMessage(msg.ViewOnceMessage.Message)
	case msg.ViewOnceMessageV2 != nil:
		return getTypeFromMessage(msg.ViewOnceMessageV2.Message)
	case msg.ViewOnceMessageV2Extension != nil:
		return getTypeFromMessage(msg.ViewOnceMessageV2Extension.Message)
	case msg.EphemeralMessage != nil:
		return getTypeFromMessage(msg.EphemeralMessage.Message)
	case msg.DocumentWithCaptionMessage != nil:
		return getTypeFromMessage(msg.DocumentWithCaptionMessage.Message)
	case msg.ReactionMessage != nil, msg.EncReactionMessage != nil:
		return "reaction"
	case msg.PollCreationMessage != nil, msg.PollUpdateMessage != nil:
		return "poll"
	case getMediaTypeFromMessage(msg) != "":
		return "media"
	case msg.Conversation != nil, msg.ExtendedTextMessage != nil, msg.ProtocolMessage != nil:
		return "text"
	default:
		return "unknown"
	}
}

// returns the specific media type (image, video, audio, etc.)
// based on whatsmeow's internal implementation
func getMediaTypeFromMessage(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}

	switch {
	case msg.ViewOnceMessage != nil:
		return getMediaTypeFromMessage(msg.ViewOnceMessage.Message)
	case msg.ViewOnceMessageV2 != nil:
		return getMediaTypeFromMessage(msg.ViewOnceMessageV2.Message)
	case msg.ViewOnceMessageV2Extension != nil:
		return getMediaTypeFromMessage(msg.ViewOnceMessageV2Extension.Message)
	case msg.EphemeralMessage != nil:
		return getMediaTypeFromMessage(msg.EphemeralMessage.Message)
	case msg.DocumentWithCaptionMessage != nil:
		return getMediaTypeFromMessage(msg.DocumentWithCaptionMessage.Message)
	case msg.ExtendedTextMessage != nil && msg.ExtendedTextMessage.Title != nil:
		return "url"
	case msg.ImageMessage != nil:
		return "image"
	case msg.StickerMessage != nil:
		return "sticker"
	case msg.DocumentMessage != nil:
		return "document"
	case msg.AudioMessage != nil:
		if msg.AudioMessage.GetPTT() {
			return "ptt"
		}
		return "audio"
	case msg.VideoMessage != nil:
		if msg.VideoMessage.GetGifPlayback() {
			return "gif"
		}
		return "video"
	case msg.ContactMessage != nil:
		return "vcard"
	case msg.ContactsArrayMessage != nil:
		return "contact_array"
	case msg.ListMessage != nil:
		return "list"
	case msg.ListResponseMessage != nil:
		return "list_response"
	case msg.ButtonsResponseMessage != nil:
		return "buttons_response"
	case msg.OrderMessage != nil:
		return "order"
	case msg.ProductMessage != nil:
		return "product"
	case msg.InteractiveResponseMessage != nil:
		return "native_flow_response"
	default:
		return ""
	}
}

// returns a user-friendly message type string
// this wraps the whatsmeow-style functions for backward compatibility
func getMessageType(msg *waE2E.Message) string {
	msgType := getTypeFromMessage(msg)

	// If it's media, return the specific media type
	if msgType == "media" {
		mediaType := getMediaTypeFromMessage(msg)
		if mediaType != "" {
			return mediaType
		}
	}

	return msgType
}
