package whatsapp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"whatsapp-mcp/paths"
	"whatsapp-mcp/storage"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// WebhookManager defines the interface for webhook emission.
type WebhookManager interface {
	EmitMessageEvent(msg storage.MessageWithNames) error
}

// Client wraps the WhatsApp client with additional functionality.
type Client struct {
	wa               *whatsmeow.Client
	store            *storage.MessageStore
	mediaStore       *storage.MediaStore
	webhookManager   WebhookManager // optional webhook manager
	mediaConfig      MediaConfig
	log              waLog.Logger
	logFile          *os.File
	historySyncChans map[string]chan bool // tracks pending sync requests by chat JID
	historySyncMux   sync.Mutex           // protects the map
	ctx              context.Context      // client lifecycle context
	cancel           context.CancelFunc   // cancel function to stop all goroutines
}

// fileLogger wraps a logger to write to both stdout and a file.
type fileLogger struct {
	base waLog.Logger
	file *os.File
}

// Errorf logs an error message to both stdout and file.
func (l *fileLogger) Errorf(msg string, args ...any) {
	l.base.Errorf(msg, args...)
	fmt.Fprintf(l.file, "[ERROR] "+msg+"\n", args...)
}

// Warnf logs a warning message to both stdout and file.
func (l *fileLogger) Warnf(msg string, args ...any) {
	l.base.Warnf(msg, args...)
	fmt.Fprintf(l.file, "[WARN] "+msg+"\n", args...)
}

// Infof logs an info message to both stdout and file.
func (l *fileLogger) Infof(msg string, args ...any) {
	l.base.Infof(msg, args...)
	fmt.Fprintf(l.file, "[INFO] "+msg+"\n", args...)
}

// Debugf logs a debug message to both stdout and file.
func (l *fileLogger) Debugf(msg string, args ...any) {
	l.base.Debugf(msg, args...)
	fmt.Fprintf(l.file, "[DEBUG] "+msg+"\n", args...)
}

// Sub creates a sub-logger for a specific module.
func (l *fileLogger) Sub(module string) waLog.Logger {
	return &fileLogger{
		base: l.base.Sub(module),
		file: l.file,
	}
}

// NewClient creates a new WhatsApp client with the given configuration.
func NewClient(store *storage.MessageStore, mediaStore *storage.MediaStore, webhookManager WebhookManager, logLevel string) (*Client, error) {
	// validate log level, default to INFO if invalid
	validLevels := map[string]bool{
		"DEBUG": true,
		"INFO":  true,
		"WARN":  true,
		"ERROR": true,
	}
	if !validLevels[logLevel] {
		logLevel = "INFO"
	}

	// create log file in data directory
	logFile, err := os.OpenFile(paths.WhatsAppLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// create base logger for stdout
	baseLogger := waLog.Stdout("whatsapp", logLevel, true)

	// Wrap with file logger
	logger := &fileLogger{
		base: baseLogger,
		file: logFile,
	}

	logger.Infof("Initializing WhatsApp client with log level: %s (logging to %s)", logLevel, paths.WhatsAppLogPath)

	// Load media configuration
	mediaConfig := LoadMediaConfig()
	logger.Infof("Media auto-download: enabled=%v, max_size=%d MB, types=%v",
		mediaConfig.AutoDownloadEnabled,
		mediaConfig.AutoDownloadMaxSize/(1024*1024),
		getEnabledTypes(mediaConfig.AutoDownloadTypes))

	ctx := context.Background()

	container, err := sqlstore.New(ctx, "sqlite", "file:"+paths.WhatsAppAuthDBPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create sqlstore: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get  device: %w", err)
	}

	waClient := whatsmeow.NewClient(deviceStore, logger)

	// create client lifecycle context
	clientCtx, cancel := context.WithCancel(context.Background())

	client := &Client{
		wa:               waClient,
		store:            store,
		mediaStore:       mediaStore,
		webhookManager:   webhookManager,
		mediaConfig:      mediaConfig,
		log:              logger,
		logFile:          logFile,
		historySyncChans: make(map[string]chan bool),
		ctx:              clientCtx,
		cancel:           cancel,
	}

	waClient.AddEventHandler(client.eventHandler)

	return client, nil
}

// IsLoggedIn reports whether the client is logged in.
func (c *Client) IsLoggedIn() bool {
	return c.wa.Store.ID != nil
}

// Connect establishes a connection to WhatsApp.
func (c *Client) Connect() error {
	return c.wa.Connect()
}

// Disconnect closes the WhatsApp connection and cleans up resources.
func (c *Client) Disconnect() {
	// cancel context to stop all running goroutines
	if c.cancel != nil {
		c.cancel()
	}
	c.wa.Disconnect()
	if c.logFile != nil {
		if err := c.logFile.Close(); err != nil {
			c.log.Errorf("failed to close log file: %v", err)
		}
	}
}

// GetQRChannel returns a channel for receiving QR codes for authentication.
func (c *Client) GetQRChannel(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	if c.IsLoggedIn() {
		return nil, fmt.Errorf("already logged in")
	}

	qrChan, err := c.wa.GetQRChannel(ctx)
	if err != nil {
		return nil, err
	}

	go func() {
		err := c.Connect()
		if err != nil {
			c.log.Errorf("failed to connect: %v", err)
		}
	}()

	return qrChan, nil
}

// SendTextMessage sends a text message to a chat.
func (c *Client) SendTextMessage(ctx context.Context, chatJID string, text string) error {
	targetJID, err := types.ParseJID(chatJID)
	if err != nil {
		return err
	}

	resp, err := c.wa.SendMessage(ctx, targetJID, &waE2E.Message{
		Conversation: proto.String(text),
	})

	if err != nil {
		return err
	}

	c.store.SaveMessage(storage.Message{
		ID:          resp.ID,
		ChatJID:     chatJID,
		SenderJID:   resp.Sender.String(),
		Text:        text,
		Timestamp:   resp.Timestamp,
		IsFromMe:    true,
		MessageType: "text",
	})

	return nil
}

// readMediaSource reads media data from a local file path or URL.
// Returns the data bytes and MIME type.
func (c *Client) readMediaSource(source string) ([]byte, string, error) {
	const maxSize = 16 * 1024 * 1024

	// Expand ~ to home directory
	if strings.HasPrefix(source, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, "", fmt.Errorf("failed to expand home directory: %w", err)
		}
		source = home + source[1:]
	}

	if strings.HasPrefix(source, "/") {
		// Local file
		c.log.Infof("Reading local file: %s", source)
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read local file: %w", err)
		}
		if len(data) > maxSize {
			return nil, "", fmt.Errorf("file too large (max 16MB)")
		}
		mimeType := http.DetectContentType(data)
		return data, mimeType, nil
	}

	// URL
	c.log.Infof("Downloading from URL: %s", source)
	resp, err := http.Get(source)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("failed to download: HTTP %d", resp.StatusCode)
	}

	limitedReader := io.LimitReader(resp.Body, int64(maxSize)+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read data: %w", err)
	}
	if len(data) > maxSize {
		return nil, "", fmt.Errorf("file too large (max 16MB)")
	}

	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	return data, mimeType, nil
}

// convertGifToMp4 converts a GIF file to MP4 using ffmpeg.
// Returns the MP4 data or an error if ffmpeg is not available or conversion fails.
func convertGifToMp4(gifData []byte) ([]byte, error) {
	tmpDir, err := os.MkdirTemp("", "wa-gif-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	gifPath := filepath.Join(tmpDir, "input.gif")
	mp4Path := filepath.Join(tmpDir, "output.mp4")

	if err := os.WriteFile(gifPath, gifData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write temp gif: %w", err)
	}

	// Try common ffmpeg paths since LaunchAgents may have limited PATH
	ffmpegPath := "ffmpeg"
	for _, p := range []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg"} {
		if _, err := os.Stat(p); err == nil {
			ffmpegPath = p
			break
		}
	}

	cmd := exec.Command(ffmpegPath, "-y", "-i", gifPath,
		"-movflags", "faststart",
		"-pix_fmt", "yuv420p",
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-an", mp4Path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w\n%s", err, string(output))
	}

	return os.ReadFile(mp4Path)
}

// SendImageMessage sends an image or GIF to a WhatsApp chat.
// imageSource can be a URL (http/https) or a local file path (absolute or ~/...).
// For GIF sources, it sends as a video with GifPlayback=true (WhatsApp requirement).
func (c *Client) SendImageMessage(ctx context.Context, chatJID string, imageSource string, caption string, replyToID string) error {
	c.log.Infof("SendImageMessage called: chatJID=%s, imageSource=%s, caption=%s", chatJID, imageSource, caption)

	targetJID, err := types.ParseJID(chatJID)
	if err != nil {
		return fmt.Errorf("invalid chat JID: %w", err)
	}

	data, mimeType, err := c.readMediaSource(imageSource)
	if err != nil {
		return fmt.Errorf("failed to read image: %w", err)
	}

	isGif := strings.Contains(mimeType, "gif") || strings.HasSuffix(strings.ToLower(imageSource), ".gif")

	// For GIFs: WhatsApp requires MP4 data with GifPlayback flag.
	const maxMediaSize = 16 * 1024 * 1024
	if isGif && strings.Contains(mimeType, "gif") && !strings.HasPrefix(imageSource, "/") {
		mp4URL := strings.Replace(imageSource, "/giphy.gif", "/giphy.mp4", 1)
		mp4URL = strings.Replace(mp4URL, "rid=giphy.gif", "rid=giphy.mp4", 1)
		if mp4URL != imageSource {
			c.log.Infof("GIF detected, fetching MP4 version: %s", mp4URL)
			mp4Resp, err := http.Get(mp4URL)
			if err == nil && mp4Resp.StatusCode == http.StatusOK {
				mp4Data, err := io.ReadAll(io.LimitReader(mp4Resp.Body, maxMediaSize+1))
				mp4Resp.Body.Close()
				if err == nil && len(mp4Data) <= maxMediaSize && len(mp4Data) > 0 {
					data = mp4Data
					mimeType = "video/mp4"
				}
			} else if mp4Resp != nil {
				mp4Resp.Body.Close()
			}
		}
	}

	// For local GIFs (or URL GIFs where MP4 fetch failed), convert with ffmpeg
	if isGif && strings.Contains(mimeType, "gif") {
		mp4Data, err := convertGifToMp4(data)
		if err != nil {
			c.log.Warnf("ffmpeg GIF conversion failed: %v (sending raw GIF)", err)
		} else {
			data = mp4Data
			mimeType = "video/mp4"
		}
	}

	var msg *waE2E.Message

	if isGif {
		uploaded, err := c.wa.Upload(ctx, data, whatsmeow.MediaVideo)
		if err != nil {
			return fmt.Errorf("failed to upload GIF: %w", err)
		}

		videoMsg := &waE2E.VideoMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(data))),
			Mimetype:      proto.String(mimeType),
			GifPlayback:   proto.Bool(true),
		}
		if caption != "" {
			videoMsg.Caption = proto.String(caption)
		}
		msg = &waE2E.Message{VideoMessage: videoMsg}
	} else {
		uploaded, err := c.wa.Upload(ctx, data, whatsmeow.MediaImage)
		if err != nil {
			return fmt.Errorf("failed to upload image: %w", err)
		}

		imageMsg := &waE2E.ImageMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(data))),
			Mimetype:      proto.String(mimeType),
		}
		if caption != "" {
			imageMsg.Caption = proto.String(caption)
		}
		msg = &waE2E.Message{ImageMessage: imageMsg}
	}

	// Add reply context if specified
	if replyToID != "" {
		quotedMsg, err := c.store.GetMessageByID(replyToID)
		if err != nil {
			return fmt.Errorf("failed to look up quoted message: %w", err)
		}
		if quotedMsg == nil {
			return fmt.Errorf("quoted message %s not found in database", replyToID)
		}

		contextInfo := &waE2E.ContextInfo{
			StanzaID:      proto.String(replyToID),
			Participant:   proto.String(quotedMsg.SenderJID),
			QuotedMessage: &waE2E.Message{Conversation: proto.String(quotedMsg.Text)},
		}

		if isGif {
			msg.VideoMessage.ContextInfo = contextInfo
		} else {
			msg.ImageMessage.ContextInfo = contextInfo
		}
	}

	sendResp, err := c.wa.SendMessage(ctx, targetJID, msg)
	if err != nil {
		return fmt.Errorf("failed to send image: %w", err)
	}

	c.log.Infof("Image sent successfully! ID=%s", sendResp.ID)
	msgType := "image"
	if isGif {
		msgType = "gif"
	}
	text := caption
	if text == "" {
		text = "[" + msgType + "]"
	}

	c.store.SaveMessage(storage.Message{
		ID:          sendResp.ID,
		ChatJID:     chatJID,
		SenderJID:   sendResp.Sender.String(),
		Text:        text,
		Timestamp:   sendResp.Timestamp,
		IsFromMe:    true,
		MessageType: msgType,
	})

	return nil
}

// SendVideoMessage sends a video to a WhatsApp chat.
// videoSource can be a URL (http/https) or a local file path (absolute or ~/...).
func (c *Client) SendVideoMessage(ctx context.Context, chatJID string, videoSource string, caption string, replyToID string) error {
	targetJID, err := types.ParseJID(chatJID)
	if err != nil {
		return fmt.Errorf("invalid chat JID: %w", err)
	}

	data, mimeType, err := c.readMediaSource(videoSource)
	if err != nil {
		return fmt.Errorf("failed to read video: %w", err)
	}

	uploaded, err := c.wa.Upload(ctx, data, whatsmeow.MediaVideo)
	if err != nil {
		return fmt.Errorf("failed to upload video: %w", err)
	}

	videoMsg := &waE2E.VideoMessage{
		URL:           proto.String(uploaded.URL),
		DirectPath:    proto.String(uploaded.DirectPath),
		MediaKey:      uploaded.MediaKey,
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(data))),
		Mimetype:      proto.String(mimeType),
	}
	if caption != "" {
		videoMsg.Caption = proto.String(caption)
	}

	msg := &waE2E.Message{VideoMessage: videoMsg}

	// Add reply context if specified
	if replyToID != "" {
		quotedMsg, err := c.store.GetMessageByID(replyToID)
		if err != nil {
			return fmt.Errorf("failed to look up quoted message: %w", err)
		}
		if quotedMsg == nil {
			return fmt.Errorf("quoted message %s not found in database", replyToID)
		}
		msg.VideoMessage.ContextInfo = &waE2E.ContextInfo{
			StanzaID:      proto.String(replyToID),
			Participant:   proto.String(quotedMsg.SenderJID),
			QuotedMessage: &waE2E.Message{Conversation: proto.String(quotedMsg.Text)},
		}
	}

	sendResp, err := c.wa.SendMessage(ctx, targetJID, msg)
	if err != nil {
		return fmt.Errorf("failed to send video: %w", err)
	}

	text := caption
	if text == "" {
		text = "[video]"
	}

	c.store.SaveMessage(storage.Message{
		ID:          sendResp.ID,
		ChatJID:     chatJID,
		SenderJID:   sendResp.Sender.String(),
		Text:        text,
		Timestamp:   sendResp.Timestamp,
		IsFromMe:    true,
		MessageType: "video",
	})

	return nil
}

// RequestHistorySync requests additional message history from WhatsApp.
// If waitForSync is true, it blocks until the sync completes and returns the new messages.
func (c *Client) RequestHistorySync(ctx context.Context, chatJID string, count int, waitForSync bool) ([]storage.MessageWithNames, error) {
	// parse the chatJID string to types.JID
	parsedJID, err := types.ParseJID(chatJID)
	if err != nil {
		return nil, fmt.Errorf("invalid chat JID: %w", err)
	}

	normalizedJID := c.normalizeJID(parsedJID)

	oldestMessage, err := c.store.GetOldestMessage(normalizedJID)
	if err != nil {
		return nil, fmt.Errorf("failed to get oldest message: %w", err)
	}

	if oldestMessage == nil {
		return nil, fmt.Errorf("no messages in database for this chat. Please wait for initial history sync")
	}

	lastKnownMessageInfo := &types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     parsedJID,
			IsFromMe: oldestMessage.IsFromMe,
		},
		ID:        oldestMessage.ID,
		Timestamp: oldestMessage.Timestamp,
	}

	reqMsg := c.wa.BuildHistorySyncRequest(lastKnownMessageInfo, count)

	if waitForSync {
		oldestTimestamp := oldestMessage.Timestamp

		syncChan := make(chan bool, 1)

		c.historySyncMux.Lock()
		c.historySyncChans[normalizedJID] = syncChan
		c.historySyncMux.Unlock()

		_, err = c.wa.SendMessage(ctx, c.wa.Store.ID.ToNonAD(), reqMsg, whatsmeow.SendRequestExtra{Peer: true})
		if err != nil {
			// clean up the channel on error
			c.historySyncMux.Lock()
			delete(c.historySyncChans, normalizedJID)
			c.historySyncMux.Unlock()
			return nil, fmt.Errorf("failed to send history sync request: %w", err)
		}

		c.log.Infof("Sent ON_DEMAND history sync request for chat %s (count: %d)", normalizedJID, count)

		// wait for signal with timeout (30 seconds)
		select {
		case <-syncChan:
			c.log.Debugf("History sync completed for chat %s", normalizedJID)
		case <-time.After(30 * time.Second):
			// clean up on timeout
			c.historySyncMux.Lock()
			delete(c.historySyncChans, normalizedJID)
			c.historySyncMux.Unlock()
			return nil, fmt.Errorf("timeout waiting for history sync. Try using wait_for_sync=false for async mode")
		}

		// retrieve newly loaded messages from database
		messages, err := c.store.GetChatMessagesOlderThan(normalizedJID, oldestTimestamp, count)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve newly loaded messages: %w", err)
		}

		c.log.Infof("Retrieved %d newly loaded messages for chat %s", len(messages), normalizedJID)
		return messages, nil
	} else {
		// asynchronous mode - send request and return immediately
		_, err = c.wa.SendMessage(ctx, c.wa.Store.ID.ToNonAD(), reqMsg, whatsmeow.SendRequestExtra{Peer: true})
		if err != nil {
			return nil, fmt.Errorf("failed to send history sync request: %w", err)
		}

		c.log.Infof("Sent ON_DEMAND history sync request for chat %s (count: %d, async mode)", normalizedJID, count)
		return []storage.MessageWithNames{}, nil
	}
}

// MyInfo contains the user's own WhatsApp profile information
type MyInfo struct {
	JID          string // User's WhatsApp JID
	PushName     string // User's display name (from store)
	Status       string // User's bio/status message
	PictureID    string // Profile picture ID
	PictureURL   string // Profile picture download URL (empty if not set)
	BusinessName string // Verified business name (if applicable)
}

// GetMyInfo retrieves the current user's WhatsApp profile information
func (c *Client) GetMyInfo(ctx context.Context) (*MyInfo, error) {
	if !c.IsLoggedIn() {
		return nil, fmt.Errorf("not logged in")
	}

	myJID := c.wa.Store.ID.ToNonAD()

	// Get basic user info (status, picture ID, verified business name)
	userInfoMap, err := c.wa.GetUserInfo(ctx, []types.JID{myJID})
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	userInfo, ok := userInfoMap[myJID]
	if !ok {
		return nil, fmt.Errorf("user info not found for own JID")
	}

	// Get push name from store
	pushName := c.wa.Store.PushName

	// Get contact info for business name (if available)
	var businessName string
	if c.wa.Store.Contacts != nil {
		contactInfo, err := c.wa.Store.Contacts.GetContact(ctx, myJID)
		if err == nil && contactInfo.Found {
			businessName = contactInfo.BusinessName
		}
	}

	// Try to get profile picture URL
	var pictureURL string
	picInfo, err := c.wa.GetProfilePictureInfo(ctx, myJID, &whatsmeow.GetProfilePictureParams{
		Preview: false,
	})
	if err == nil && picInfo != nil {
		pictureURL = picInfo.URL
	}
	// Ignore ErrProfilePictureNotSet and ErrProfilePictureUnauthorized - just leave URL empty

	return &MyInfo{
		JID:          myJID.String(),
		PushName:     pushName,
		Status:       userInfo.Status,
		PictureID:    userInfo.PictureID,
		PictureURL:   pictureURL,
		BusinessName: businessName,
	}, nil
}

// getEnabledTypes returns a list of enabled media types for logging.
func getEnabledTypes(types map[string]bool) []string {
	var enabled []string
	for t, v := range types {
		if v {
			enabled = append(enabled, t)
		}
	}
	return enabled
}
