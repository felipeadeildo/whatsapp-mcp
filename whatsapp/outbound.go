package whatsapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"whatsapp-mcp/storage"
)

// SendFile uploads a local file and sends it as an image, video, audio or
// document message. The proto field used is determined by the file extension
// (see detectOutboundMediaType). Caption is optional; WhatsApp displays it
// for images and videos and stores it on documents.
//
// On success, the returned string is the WhatsApp message ID. On failure
// before the message is sent, an error is returned and nothing is persisted.
// If the message is sent but the local store write fails, the error is logged
// (consistent with SendTextMessage) and the message ID is still returned --
// the recipient already has the message.
func (c *Client) SendFile(ctx context.Context, chatJID, filePath, caption string) (string, error) {
	targetJID, err := types.ParseJID(chatJID)
	if err != nil {
		return "", fmt.Errorf("invalid chat JID %q: %w", chatJID, err)
	}

	abs, err := validateOutboundMediaPath(filePath)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("file is empty: %s", abs)
	}

	kind, mime := detectOutboundMediaType(abs)

	upload, err := c.wa.Upload(ctx, data, kind.MediaType())
	if err != nil {
		return "", fmt.Errorf("failed to upload %s: %w", kind, err)
	}

	msg := buildMediaMessage(kind, mime, &upload, filepath.Base(abs), caption, false)

	resp, err := c.wa.SendMessage(ctx, targetJID, msg)
	if err != nil {
		return "", fmt.Errorf("failed to send %s: %w", kind, err)
	}

	c.persistOutboundMedia(resp, chatJID, kind.String(), caption)

	c.log.Infof("Sent %s message %s to %s (size: %d bytes)", kind, resp.ID, chatJID, len(data))
	return resp.ID, nil
}

// SendAudioMessage sends an audio file as a WhatsApp voice note (PTT). When
// the source file is not already in ogg-opus format, ffmpeg is invoked to
// transcode it into the format WhatsApp expects (mono, 48 kHz, libopus,
// 16 kbps). Without ffmpeg installed, ErrFFmpegNotAvailable is returned for
// non-ogg inputs; pre-converted .ogg/.opus inputs always succeed.
func (c *Client) SendAudioMessage(ctx context.Context, chatJID, audioPath string) (string, error) {
	targetJID, err := types.ParseJID(chatJID)
	if err != nil {
		return "", fmt.Errorf("invalid chat JID %q: %w", chatJID, err)
	}

	abs, err := validateOutboundMediaPath(audioPath)
	if err != nil {
		return "", err
	}

	// decide whether we need to convert. .ogg and .opus go through unchanged;
	// everything else is routed through ffmpeg.
	srcPath := abs
	cleanup := func() {}
	ext := strings.ToLower(filepath.Ext(abs))
	if ext != ".ogg" && ext != ".opus" {
		converted, cleanFn, convErr := convertToOpusOgg(ctx, abs)
		if convErr != nil {
			return "", convErr
		}
		srcPath = converted
		cleanup = cleanFn
	}
	defer cleanup()

	data, err := os.ReadFile(srcPath)
	if err != nil {
		return "", fmt.Errorf("failed to read audio file: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("audio file is empty after conversion")
	}

	upload, err := c.wa.Upload(ctx, data, whatsmeow.MediaAudio)
	if err != nil {
		return "", fmt.Errorf("failed to upload audio: %w", err)
	}

	// Probe duration. Errors here are non-fatal -- the message can still be
	// sent without a Seconds field on the proto.
	duration, durErr := probeAudioDurationSeconds(ctx, srcPath)
	if durErr != nil {
		c.log.Debugf("ffprobe failed for %s: %v (continuing without duration)", srcPath, durErr)
	}

	audioMsg := &waE2E.AudioMessage{
		URL:           proto.String(upload.URL),
		DirectPath:    proto.String(upload.DirectPath),
		MediaKey:      upload.MediaKey,
		Mimetype:      proto.String("audio/ogg; codecs=opus"),
		FileEncSHA256: upload.FileEncSHA256,
		FileSHA256:    upload.FileSHA256,
		FileLength:    proto.Uint64(upload.FileLength),
		PTT:           proto.Bool(true),
	}
	if duration > 0 {
		audioMsg.Seconds = proto.Uint32(duration)
	}

	resp, err := c.wa.SendMessage(ctx, targetJID, &waE2E.Message{
		AudioMessage: audioMsg,
	})
	if err != nil {
		return "", fmt.Errorf("failed to send voice note: %w", err)
	}

	c.persistOutboundMedia(resp, chatJID, "ptt", "")

	c.log.Infof("Sent voice note %s to %s (duration: %ds, size: %d bytes)",
		resp.ID, chatJID, duration, len(data))
	return resp.ID, nil
}

// buildMediaMessage constructs the proper *waE2E.Message variant for the
// given media kind. For images and videos the caption is included; documents
// get a FileName so receivers display them with the original filename.
func buildMediaMessage(
	kind outboundMediaKind,
	mime string,
	upload *whatsmeow.UploadResponse,
	fileName, caption string,
	asPTT bool,
) *waE2E.Message {
	common := struct {
		URL           *string
		DirectPath    *string
		MediaKey      []byte
		FileEncSHA256 []byte
		FileSHA256    []byte
		FileLength    *uint64
		Mimetype      *string
	}{
		URL:           proto.String(upload.URL),
		DirectPath:    proto.String(upload.DirectPath),
		MediaKey:      upload.MediaKey,
		FileEncSHA256: upload.FileEncSHA256,
		FileSHA256:    upload.FileSHA256,
		FileLength:    proto.Uint64(upload.FileLength),
		Mimetype:      proto.String(mime),
	}

	switch kind {
	case outboundMediaImage:
		img := &waE2E.ImageMessage{
			URL:           common.URL,
			DirectPath:    common.DirectPath,
			MediaKey:      common.MediaKey,
			Mimetype:      common.Mimetype,
			FileEncSHA256: common.FileEncSHA256,
			FileSHA256:    common.FileSHA256,
			FileLength:    common.FileLength,
		}
		if caption != "" {
			img.Caption = proto.String(caption)
		}
		return &waE2E.Message{ImageMessage: img}

	case outboundMediaVideo:
		vid := &waE2E.VideoMessage{
			URL:           common.URL,
			DirectPath:    common.DirectPath,
			MediaKey:      common.MediaKey,
			Mimetype:      common.Mimetype,
			FileEncSHA256: common.FileEncSHA256,
			FileSHA256:    common.FileSHA256,
			FileLength:    common.FileLength,
		}
		if caption != "" {
			vid.Caption = proto.String(caption)
		}
		return &waE2E.Message{VideoMessage: vid}

	case outboundMediaAudio:
		aud := &waE2E.AudioMessage{
			URL:           common.URL,
			DirectPath:    common.DirectPath,
			MediaKey:      common.MediaKey,
			Mimetype:      common.Mimetype,
			FileEncSHA256: common.FileEncSHA256,
			FileSHA256:    common.FileSHA256,
			FileLength:    common.FileLength,
			PTT:           proto.Bool(asPTT),
		}
		return &waE2E.Message{AudioMessage: aud}

	default: // document
		doc := &waE2E.DocumentMessage{
			URL:           common.URL,
			DirectPath:    common.DirectPath,
			MediaKey:      common.MediaKey,
			Mimetype:      common.Mimetype,
			FileEncSHA256: common.FileEncSHA256,
			FileSHA256:    common.FileSHA256,
			FileLength:    common.FileLength,
			FileName:      proto.String(fileName),
		}
		if caption != "" {
			doc.Caption = proto.String(caption)
		}
		return &waE2E.Message{DocumentMessage: doc}
	}
}

// persistOutboundMedia mirrors the SendTextMessage save pattern: log on
// failure but never return an error to the caller, since the message has
// already been delivered to WhatsApp servers.
func (c *Client) persistOutboundMedia(resp whatsmeow.SendResponse, chatJID, messageType, caption string) {
	if err := c.store.SaveMessage(storage.Message{
		ID:          resp.ID,
		ChatJID:     chatJID,
		SenderJID:   resp.Sender.String(),
		Text:        caption,
		Timestamp:   resp.Timestamp,
		IsFromMe:    true,
		MessageType: messageType,
	}); err != nil {
		c.log.Errorf("failed to save outbound %s message %s: %v", messageType, resp.ID, err)
	}
}

// SendReaction sends an emoji reaction against a previously sent or received
// message. Passing an empty emoji string removes the reaction (this is how
// WhatsApp itself models removal on the wire).
//
// senderJID is the JID of the original message's sender. For your own
// messages pass your JID or an empty string; for messages sent by others
// pass their JID. The reaction is also persisted in the local store so
// reads of message_reactions reflect what was sent.
func (c *Client) SendReaction(ctx context.Context, chatJID, messageID, senderJID, emoji string) error {
	chat, err := types.ParseJID(chatJID)
	if err != nil {
		return fmt.Errorf("invalid chat JID %q: %w", chatJID, err)
	}

	var sender types.JID
	if strings.TrimSpace(senderJID) == "" {
		sender = types.EmptyJID
	} else {
		sender, err = types.ParseJID(senderJID)
		if err != nil {
			return fmt.Errorf("invalid sender JID %q: %w", senderJID, err)
		}
	}

	msg := c.wa.BuildReaction(chat, sender, types.MessageID(messageID), emoji)
	resp, err := c.wa.SendMessage(ctx, chat, msg)
	if err != nil {
		return fmt.Errorf("failed to send reaction: %w", err)
	}

	// Mirror the reaction locally so readers see the same state. The reactor
	// here is "us" -- use our own JID rather than the original sender's, since
	// (message_id, sender_jid) is the natural primary key on this table.
	myJID := ""
	if c.wa.Store.ID != nil {
		myJID = c.wa.Store.ID.ToNonAD().String()
	}
	if err := c.store.SaveReaction(storage.Reaction{
		MessageID: messageID,
		ChatJID:   chatJID,
		SenderJID: myJID,
		Emoji:     emoji,
		Timestamp: resp.Timestamp,
	}); err != nil {
		c.log.Errorf("failed to persist reaction for %s: %v", messageID, err)
	}

	if emoji == "" {
		c.log.Infof("Removed reaction on message %s in chat %s", messageID, chatJID)
	} else {
		c.log.Infof("Reacted %q on message %s in chat %s", emoji, messageID, chatJID)
	}
	return nil
}

// EditMessage replaces the text of an existing message. The WhatsApp server
// rejects edits older than whatsmeow.EditWindow (~20 minutes); the resulting
// IQ error is surfaced to the caller so they can show a useful message.
//
// Only text messages can be edited (this matches WhatsApp's official client
// -- captions on media require a separate flow not exposed here).
func (c *Client) EditMessage(ctx context.Context, chatJID, messageID, newText string) error {
	chat, err := types.ParseJID(chatJID)
	if err != nil {
		return fmt.Errorf("invalid chat JID %q: %w", chatJID, err)
	}

	if strings.TrimSpace(newText) == "" {
		return fmt.Errorf("new text is empty -- to delete a message use delete_message instead")
	}

	// Pre-validate the edit against the local message store. whatsmeow itself
	// does NOT enforce the 20-minute edit window: it will happily build and
	// send the edit envelope, the WhatsApp server will ack the IQ, but the
	// edit is silently dropped on the recipient side if the original message
	// is older than whatsmeow.EditWindow (or if we never sent it / it's not
	// from us). The previous version of this function returned success in all
	// those cases, leading the LLM to claim "edited" when nothing actually
	// changed in the client. Catch those cases here so the caller sees a
	// clear error.
	if c.store != nil {
		original, lookupErr := c.store.GetMessageByID(messageID)
		if lookupErr != nil {
			c.log.Warnf("could not look up message %s before edit: %v", messageID, lookupErr)
		} else if original == nil {
			return fmt.Errorf("message %s not found in local store -- cannot edit a message we never observed", messageID)
		} else {
			if !original.IsFromMe {
				return fmt.Errorf("message %s is not from us (is_from_me=false) -- WhatsApp only allows editing your own messages", messageID)
			}
			if original.ChatJID != chat.String() {
				return fmt.Errorf("message %s belongs to chat %s, not %s -- refusing to edit", messageID, original.ChatJID, chat.String())
			}
			age := time.Since(original.Timestamp)
			if age > whatsmeow.EditWindow {
				return fmt.Errorf("edit window expired: message %s was sent %s ago, WhatsApp only allows edits within %s", messageID, age.Round(time.Second), whatsmeow.EditWindow)
			}
		}
	}

	editMsg := c.wa.BuildEdit(chat, types.MessageID(messageID), &waE2E.Message{
		Conversation: proto.String(newText),
	})

	resp, err := c.wa.SendMessage(ctx, chat, editMsg)
	if err != nil {
		return fmt.Errorf("failed to edit message: %w", err)
	}

	if err := c.store.MarkMessageEdited(messageID, newText, resp.Timestamp); err != nil {
		// not finding the row locally is benign -- the edit was accepted by the
		// server, we just don't have the original cached. Anything else is a
		// real failure worth logging.
		c.log.Debugf("could not update local row for edited message %s: %v", messageID, err)
	}

	c.log.Infof("Edited message %s in chat %s", messageID, chatJID)
	return nil
}

// DeleteMessage revokes a message for everyone in the chat. WhatsApp enforces
// a time window for revoking your own messages (~15 minutes for self,
// indefinitely for group admins); the corresponding server error is returned
// verbatim so callers can surface it.
//
// senderJID identifies the original message's sender. Pass an empty string
// (or your own JID) to revoke your own message; pass another user's JID to
// admin-revoke their message in a group.
func (c *Client) DeleteMessage(ctx context.Context, chatJID, messageID, senderJID string) error {
	chat, err := types.ParseJID(chatJID)
	if err != nil {
		return fmt.Errorf("invalid chat JID %q: %w", chatJID, err)
	}

	var sender types.JID
	if strings.TrimSpace(senderJID) == "" {
		sender = types.EmptyJID
	} else {
		sender, err = types.ParseJID(senderJID)
		if err != nil {
			return fmt.Errorf("invalid sender JID %q: %w", senderJID, err)
		}
	}

	resp, err := c.wa.SendMessage(ctx, chat, c.wa.BuildRevoke(chat, sender, types.MessageID(messageID)))
	if err != nil {
		return fmt.Errorf("failed to revoke message: %w", err)
	}

	if err := c.store.MarkMessageDeleted(messageID, resp.Timestamp); err != nil {
		c.log.Debugf("could not update local row for revoked message %s: %v", messageID, err)
	}

	c.log.Infof("Revoked message %s in chat %s", messageID, chatJID)
	return nil
}
