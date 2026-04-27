package whatsapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
	// everything else is shoved through ffmpeg.
	srcPath := abs
	cleanup := func() {}
	switch filepath.Ext(filepath.ToSlash(abs)) {
	case ".ogg", ".OGG", ".opus", ".OPUS":
		// no conversion needed
	default:
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
