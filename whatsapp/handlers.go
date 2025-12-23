package whatsapp

import (
	"context"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
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

// computeCanonicalJID returns the first non-nil JID value (PN takes precedence)
func computeCanonicalJID(pn, lid *string) string {
	if pn != nil {
		return *pn
	}
	if lid != nil {
		return *lid
	}
	return ""
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

	msgType := getMessageType(evt.Message)

	// extract sender JID with alternatives using Info.SenderAlt
	senderPN, senderLID := c.extractJIDPair(info.Sender, info.SenderAlt)

	// extract chat JID with alternatives
	// for DMs (not groups), get alternative JID from store
	ctx := context.Background()
	var chatAltJID types.JID
	if info.Chat.Server != "g.us" {
		var err error
		chatAltJID, err = c.wa.Store.GetAltJID(ctx, info.Chat)
		if err != nil {
			c.log.Debugf("No alt JID for chat %s: %v", info.Chat, err)
		}
	}
	chatPN, chatLID := c.extractJIDPair(info.Chat, chatAltJID)

	// save/update chat BEFORE saving message (for foreign key constraint)
	isGroup := info.Chat.Server == "g.us"
	var pushName, contactName string

	if isGroup {
		// for groups, fetch group info to get the name
		ctx := context.Background()
		groupInfo, err := c.wa.GetGroupInfo(ctx, info.Chat)
		if err != nil {
			c.log.Debugf("Failed to get group info for %s: %v", info.Chat, err)
		} else {
			pushName = groupInfo.Name
		}
	} else {
		// for DMs, capture push name from message
		if info.PushName != "" && !info.IsFromMe {
			pushName = info.PushName
		}

		// get ACTUAL saved contact name from contact store
		ctx := context.Background()
		contactInfo, err := c.wa.Store.Contacts.GetContact(ctx, info.Chat)
		if err == nil {
			// Priority: FullName (saved contact) > FirstName > BusinessName
			// NEVER use PushName here - that's the WhatsApp display name!
			if contactInfo.FullName != "" {
				contactName = contactInfo.FullName
			} else if contactInfo.FirstName != "" {
				contactName = contactInfo.FirstName
			} else if contactInfo.BusinessName != "" {
				contactName = contactInfo.BusinessName
			}
		}
	}

	chat := storage.Chat{
		JIDPN:           chatPN,
		JIDLID:          chatLID,
		PushName:        pushName,
		ContactName:     contactName,
		LastMessageTime: info.Timestamp,
		IsGroup:         isGroup,
	}

	if err := c.store.SaveChat(chat); err != nil {
		c.log.Errorf("Failed to save chat %s (PN=%v, LID=%v, IsFromMe=%v): %v",
			info.Chat, chatPN, chatLID, info.IsFromMe, err)
		return
	}

	msg := storage.Message{
		ID:           info.ID,
		ChatJIDPN:    chatPN,
		ChatJIDLID:   chatLID,
		SenderJIDPN:  senderPN,
		SenderJIDLID: senderLID,
		Text:         text,
		Timestamp:    info.Timestamp,
		IsFromMe:     info.IsFromMe,
		MessageType:  msgType,
	}
	// Note: push names are still captured and saved to push_names table below

	if err := c.store.SaveMessage(msg); err != nil {
		computedChatJID := computeCanonicalJID(chatPN, chatLID)
		c.log.Errorf("Failed to save message %s in chat %s: %v (ChatJID computed as: %s)",
			info.ID, info.Chat, err, computedChatJID)
		return
	}

	// save push name to database if available
	if info.PushName != "" && !info.IsFromMe {
		pushNames := map[string]string{info.Sender.String(): info.PushName}
		if err := c.store.SavePushNames(pushNames); err != nil {
			c.log.Debugf("Failed to save push name for %s: %v", info.Sender, err)
		}
	}

	c.log.Infof("Saved message %s from %s (IsFromMe=%v, Type=%s)",
		info.ID, info.Sender, info.IsFromMe, msgType)
}

// handle group info updates (name, topic, settings changes)
func (c *Client) handleGroupInfo(evt *events.GroupInfo) {
	// update group name if changed
	if evt.Name != nil {
		groupPN, groupLID := c.extractJIDPair(evt.JID, types.EmptyJID)

		chat := storage.Chat{
			JIDPN:           groupPN,
			JIDLID:          groupLID,
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

func (c *Client) handleHistorySync(evt *events.HistorySync) {
	c.log.Infof("Starting history sync: %d conversations to process", len(evt.Data.GetConversations()))

	ctx := context.Background()

	// load existing push names from database
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

		// For DMs, get both contact name AND push name for the chat
		var dmContactName, dmPushName string
		if !isGroup {
			contactInfo, err := c.wa.Store.Contacts.GetContact(ctx, chatJIDObject)
			if err == nil {
				// Contact name: FullName (saved contact) > FirstName > BusinessName
				if contactInfo.FullName != "" {
					dmContactName = contactInfo.FullName
				} else if contactInfo.FirstName != "" {
					dmContactName = contactInfo.FirstName
				} else if contactInfo.BusinessName != "" {
					dmContactName = contactInfo.BusinessName
				}
			}

			// Push name: from pushNameMap (WhatsApp display name)
			if pushName := pushNameMap[chatJIDObject.String()]; pushName != "" {
				dmPushName = pushName
			}

			if dmContactName != "" || dmPushName != "" {
				c.log.Infof("Found DM names - contact: %s, push: %s", dmContactName, dmPushName)
			}
		}

		c.log.Infof("Processing chat [%d/%d]: %s (%d messages)",
			idx+1, len(evt.Data.GetConversations()),
			chatJIDObject.String(), len(conv.GetMessages()))

		for _, histMsg := range conv.GetMessages() {
			msg := histMsg.GetMessage()
			if msg == nil {
				continue
			}

			// use ParseWebMessage to properly extract message info including PushName
			parsedMsg, parseErr := c.wa.ParseWebMessage(chatJIDObject, msg)
			if parseErr != nil {
				c.log.Debugf("Failed to parse web message: %v", parseErr)
				// Fallback to manual parsing if ParseWebMessage fails
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
					senderJIDObject, parseErr = types.ParseJID(key.GetParticipant())
					if parseErr != nil {
						c.log.Debugf("Failed to parse participant JID: %v", parseErr)
						continue
					}
				} else {
					// DM
					senderJIDObject, parseErr = types.ParseJID(key.GetRemoteJID())
					if parseErr != nil {
						c.log.Debugf("Failed to parse remote JID: %v", parseErr)
						continue
					}
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
				// This is the sender's WhatsApp display name
				senderName := msg.GetPushName()
				if senderName == "" {
					senderName = pushNameMap[senderJIDObject.String()]
				}

				// collect push names to save to database
				if senderName != "" && !fromMe {
					additionalPushNames[senderJIDObject.String()] = senderName
				}

				// For group messages, look up individual sender contact if needed
				// For DMs, don't contaminate senderName with contact store data
				if senderName == "" && isGroup {
					contactInfo, err := c.wa.Store.Contacts.GetContact(ctx, senderJIDObject)
					if err == nil {
						if contactInfo.PushName != "" {
							senderName = contactInfo.PushName
						} else if contactInfo.BusinessName != "" {
							senderName = contactInfo.BusinessName
						} else if contactInfo.FullName != "" {
							senderName = contactInfo.FullName
						}
					}
				}

				// track ALL chats (for foreign key constraint)
				chatKey := computeCanonicalJID(chatPN, chatLID)

				if chatKey != "" {
					// check if chat already exists in map
					existingChat, exists := chatMap[chatKey]
					if exists {
						// update last message time if this message is newer
						if timestamp.After(existingChat.LastMessageTime) {
							existingChat.LastMessageTime = timestamp
						}
						// update names if we have them and existing doesn't
						if isGroup && groupName != "" && existingChat.PushName == "" {
							existingChat.PushName = groupName
						} else if !isGroup {
							// Update contact name from contact store
							if dmContactName != "" && existingChat.ContactName == "" {
								existingChat.ContactName = dmContactName
								c.log.Infof("Fallback: Updated DM contact name: %s -> %s", chatKey, dmContactName)
							}
							// Update push name from conversation-level lookup
							if dmPushName != "" && existingChat.PushName == "" {
								existingChat.PushName = dmPushName
								c.log.Infof("Fallback: Updated DM push name from conv: %s -> %s", chatKey, dmPushName)
							} else if senderName != "" && !fromMe && existingChat.PushName == "" {
								// Fallback to message-level if conversation-level not available
								existingChat.PushName = senderName
								c.log.Infof("Fallback: Updated DM push name from msg: %s -> %s", chatKey, senderName)
							}
						}
					} else {
						// create new chat entry
						var pushNameVal, contactNameVal string
						if isGroup {
							// use group name fetched from API
							pushNameVal = groupName
						} else {
							// For DMs: use conversation-level names
							contactNameVal = dmContactName
							pushNameVal = dmPushName
							// Fallback: try to get push name from this message if not set
							if pushNameVal == "" && senderName != "" && !fromMe {
								pushNameVal = senderName
							}
							if contactNameVal != "" || pushNameVal != "" {
								c.log.Infof("Fallback: Creating new DM chat - contact: %s, push: %s", contactNameVal, pushNameVal)
							}
						}
						chatMap[chatKey] = &storage.Chat{
							JIDPN:           chatPN,
							JIDLID:          chatLID,
							PushName:        pushNameVal,
							ContactName:     contactNameVal,
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
					Text:         text,
					Timestamp:    timestamp,
					IsFromMe:     fromMe,
					MessageType:  msgType,
				})
				// Note: senderName and msgSenderContactName are still captured
				// and used to update push_names and chats tables
				continue
			}

			// Successfully parsed with ParseWebMessage - use the parsed info
			info := parsedMsg.Info
			messageID := info.ID
			timestamp := info.Timestamp
			fromMe := info.IsFromMe
			senderJIDObject := info.Sender

			// get alternative JID for sender
			var senderAltJID types.JID
			if senderJIDObject.Server != "g.us" {
				senderAltJID, err = c.wa.Store.GetAltJID(ctx, senderJIDObject)
				if err != nil {
					c.log.Debugf("No alt JID for sender %s: %v", senderJIDObject, err)
				}
			}

			senderPN, senderLID := c.extractJIDPair(senderJIDObject, senderAltJID)

			// Use PushName from parsed message
			// This is the sender's WhatsApp display name
			senderName := info.PushName
			if senderName == "" {
				senderName = pushNameMap[senderJIDObject.String()]
			}

			// collect push names to save to database
			if senderName != "" && !fromMe {
				additionalPushNames[senderJIDObject.String()] = senderName
			}

			// For group messages, look up individual sender contact if needed
			// For DMs, don't contaminate senderName with contact store data
			if senderName == "" && isGroup {
				contactInfo, err := c.wa.Store.Contacts.GetContact(ctx, senderJIDObject)
				if err == nil {
					if contactInfo.PushName != "" {
						senderName = contactInfo.PushName
					} else if contactInfo.BusinessName != "" {
						senderName = contactInfo.BusinessName
					} else if contactInfo.FullName != "" {
						senderName = contactInfo.FullName
					}
				}
			}

			// track ALL chats (for foreign key constraint)
			chatKey := computeCanonicalJID(chatPN, chatLID)

			if chatKey != "" {
				// check if chat already exists in map
				existingChat, exists := chatMap[chatKey]
				if exists {
					// update last message time if this message is newer
					if timestamp.After(existingChat.LastMessageTime) {
						existingChat.LastMessageTime = timestamp
					}
					// update names if we have them and existing doesn't
					if isGroup && groupName != "" && existingChat.PushName == "" {
						existingChat.PushName = groupName
						c.log.Infof("Updated group chat name: %s -> %s", chatKey, groupName)
					} else if !isGroup {
						// update contact name from contact store
						if dmContactName != "" && existingChat.ContactName == "" {
							existingChat.ContactName = dmContactName
							c.log.Infof("Updated DM contact name: %s -> %s", chatKey, dmContactName)
						}
						// Update push name from conversation-level lookup
						if dmPushName != "" && existingChat.PushName == "" {
							existingChat.PushName = dmPushName
							c.log.Infof("Updated DM push name from conv: %s -> %s", chatKey, dmPushName)
						} else if senderName != "" && !fromMe && existingChat.PushName == "" {
							// Fallback to message-level if conversation-level not available
							existingChat.PushName = senderName
							c.log.Infof("Updated DM push name from msg: %s -> %s", chatKey, senderName)
						}
					}
				} else {
					// create new chat entry
					var pushNameVal, contactNameVal string
					if isGroup {
						// use group name fetched from API
						pushNameVal = groupName
					} else {
						// For DMs: use conversation-level names
						contactNameVal = dmContactName
						pushNameVal = dmPushName
						// Fallback: try to get push name from this message if not set
						if pushNameVal == "" && senderName != "" && !fromMe {
							pushNameVal = senderName
						}
						if contactNameVal != "" || pushNameVal != "" {
							c.log.Infof("Creating new DM chat - contact: %s, push: %s", contactNameVal, pushNameVal)
						}
					}
					chatMap[chatKey] = &storage.Chat{
						JIDPN:           chatPN,
						JIDLID:          chatLID,
						PushName:        pushNameVal,
						ContactName:     contactNameVal,
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
				Text:         text,
				Timestamp:    timestamp,
				IsFromMe:     fromMe,
				MessageType:  msgType,
			})
			// Note: senderName and msgSenderContactName are still captured
			// and used to update push_names and chats tables
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

func extractText(msg interface{}) string {
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

// getTypeFromMessage returns the high-level message type (text, media, reaction, poll)
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

// getMediaTypeFromMessage returns the specific media type (image, video, audio, etc.)
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

// getMessageType returns a user-friendly message type string
// This wraps the whatsmeow-style functions for backward compatibility
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
