package whatsapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
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

// Media sending constants for file size limits
const (
	maxImageSize    = 5 * 1024 * 1024  // 5 MB
	maxVideoSize    = 100 * 1024 * 1024 // 100 MB
	maxAudioSize    = 16 * 1024 * 1024  // 16 MB
	maxDocumentSize = 2 * 1024 * 1024 * 1024 // 2 GB
)

// loadFileData loads file data from either a local path or base64-encoded data.
// Returns the file bytes, detected filename, and any error encountered.
func (c *Client) loadFileData(filePath, fileData string) ([]byte, string, error) {
	// Validate exactly one input is provided
	if filePath == "" && fileData == "" {
		return nil, "", fmt.Errorf("either file_path or file_data must be provided")
	}
	if filePath != "" && fileData != "" {
		return nil, "", fmt.Errorf("only one of file_path or file_data should be provided, not both")
	}

	// Load from local file path
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read file: %w", err)
		}
		filename := filepath.Base(filePath)
		return data, filename, nil
	}

	// Decode base64 data
	data, err := base64.StdEncoding.DecodeString(fileData)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode base64 data: %w", err)
	}

	// For base64 data, we don't have a filename, return empty string
	return data, "", nil
}

// detectMimeType determines the MIME type of a file from its filename and content.
// Uses file extension first, falls back to content detection for base64 data.
func (c *Client) detectMimeType(filename string, data []byte) string {
	// First try to detect from file extension
	if filename != "" {
		ext := strings.ToLower(filepath.Ext(filename))
		switch ext {
		case ".jpg", ".jpeg":
			return "image/jpeg"
		case ".png":
			return "image/png"
		case ".webp":
			return "image/webp"
		case ".mp4":
			return "video/mp4"
		case ".3gp", ".3gpp":
			return "video/3gpp"
		case ".mp3":
			return "audio/mpeg"
		case ".ogg":
			return "audio/ogg"
		case ".m4a", ".aac":
			return "audio/aac"
		case ".pdf":
			return "application/pdf"
		case ".doc", ".docx":
			return "application/msword"
		case ".zip":
			return "application/zip"
		case ".txt":
			return "text/plain"
		}
	}

	// Fallback: detect from content (http.DetectContentType)
	// This is a basic implementation - for production, consider using a more sophisticated detector
	return http.DetectContentType(data)
}

// validateFileSize checks if a file's size exceeds the maximum allowed for its media type.
// Returns an error if the file is too large, nil otherwise.
func (c *Client) validateFileSize(data []byte, maxSize int, mediaType string) error {
	size := int64(len(data))
	if size > int64(maxSize) {
		return fmt.Errorf("file size %d bytes exceeds maximum %d bytes for %s", size, maxSize, mediaType)
	}
	return nil
}

// SendImage sends an image to a WhatsApp chat.
//
// This method uploads the image to WhatsApp servers and sends it to the specified chat.
// It supports both local file paths and base64-encoded data for flexibility.
//
// Parameters:
//   - ctx: Context for the operation (cancellation, timeouts)
//   - chatJID: Target chat JID (e.g., "5511999999999@s.whatsapp.net")
//   - filePath: Local path to image file (used if provided, takes precedence)
//   - fileData: Base64-encoded image data (used if filePath is empty)
//   - caption: Optional caption text to display with the image
//
// Returns:
//   - error: Error if upload or sending fails, nil on success
//
// File size limits: 5 MB for images (enforced)
// Supported formats: JPEG, PNG, WebP
func (c *Client) SendImage(ctx context.Context, chatJID, filePath, fileData, caption string) error {
	// Parse JID
	targetJID, err := types.ParseJID(chatJID)
	if err != nil {
		return fmt.Errorf("invalid chat JID: %w", err)
	}

	// Load file data
	data, filename, err := c.loadFileData(filePath, fileData)
	if err != nil {
		return fmt.Errorf("failed to load file data: %w", err)
	}

	// Validate file size
	if err := c.validateFileSize(data, maxImageSize, "image"); err != nil {
		return err
	}

	// Detect MIME type
	mimeType := c.detectMimeType(filename, data)

	// Upload to WhatsApp
	uploadResp, err := c.wa.Upload(ctx, data, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("failed to upload image: %w", err)
	}

	// Construct image message
	imageMsg := &waE2E.ImageMessage{
		Caption:       proto.String(caption),
		Mimetype:      proto.String(mimeType),
		URL:           &uploadResp.URL,
		DirectPath:    &uploadResp.DirectPath,
		MediaKey:      uploadResp.MediaKey,
		FileEncSHA256: uploadResp.FileEncSHA256,
		FileSHA256:    uploadResp.FileSHA256,
		FileLength:    &uploadResp.FileLength,
	}

	// Send message
	resp, err := c.wa.SendMessage(ctx, targetJID, &waE2E.Message{
		ImageMessage: imageMsg,
	})
	if err != nil {
		return fmt.Errorf("failed to send image message: %w", err)
	}

	// Save to database
	c.store.SaveMessage(storage.Message{
		ID:          resp.ID,
		ChatJID:     chatJID,
		SenderJID:   resp.Sender.String(),
		Text:        caption,
		Timestamp:   resp.Timestamp,
		IsFromMe:    true,
		MessageType: "image",
	})

	c.log.Infof("Sent image to %s (size: %d bytes)", chatJID, len(data))
	return nil
}

// SendVideo sends a video to a WhatsApp chat.
//
// This method uploads the video to WhatsApp servers and sends it to the specified chat.
// It supports both local file paths and base64-encoded data for flexibility.
//
// Parameters:
//   - ctx: Context for the operation
//   - chatJID: Target chat JID
//   - filePath: Local path to video file (used if provided)
//   - fileData: Base64-encoded video data (used if filePath is empty)
//   - caption: Optional caption text
//
// File size limits: 100 MB for videos (enforced)
// Supported formats: MP4, 3GP
func (c *Client) SendVideo(ctx context.Context, chatJID, filePath, fileData, caption string) error {
	// Parse JID
	targetJID, err := types.ParseJID(chatJID)
	if err != nil {
		return fmt.Errorf("invalid chat JID: %w", err)
	}

	// Load file data
	data, filename, err := c.loadFileData(filePath, fileData)
	if err != nil {
		return fmt.Errorf("failed to load file data: %w", err)
	}

	// Validate file size
	if err := c.validateFileSize(data, maxVideoSize, "video"); err != nil {
		return err
	}

	// Detect MIME type
	mimeType := c.detectMimeType(filename, data)

	// Upload to WhatsApp
	uploadResp, err := c.wa.Upload(ctx, data, whatsmeow.MediaVideo)
	if err != nil {
		return fmt.Errorf("failed to upload video: %w", err)
	}

	// Construct video message
	videoMsg := &waE2E.VideoMessage{
		Caption:       proto.String(caption),
		Mimetype:      proto.String(mimeType),
		URL:           &uploadResp.URL,
		DirectPath:    &uploadResp.DirectPath,
		MediaKey:      uploadResp.MediaKey,
		FileEncSHA256: uploadResp.FileEncSHA256,
		FileSHA256:    uploadResp.FileSHA256,
		FileLength:    &uploadResp.FileLength,
	}

	// Send message
	resp, err := c.wa.SendMessage(ctx, targetJID, &waE2E.Message{
		VideoMessage: videoMsg,
	})
	if err != nil {
		return fmt.Errorf("failed to send video message: %w", err)
	}

	// Save to database
	c.store.SaveMessage(storage.Message{
		ID:          resp.ID,
		ChatJID:     chatJID,
		SenderJID:   resp.Sender.String(),
		Text:        caption,
		Timestamp:   resp.Timestamp,
		IsFromMe:    true,
		MessageType: "video",
	})

	c.log.Infof("Sent video to %s (size: %d bytes)", chatJID, len(data))
	return nil
}

// SendDocument sends a document to a WhatsApp chat.
//
// This method uploads the document to WhatsApp servers and sends it to the specified chat.
// It supports both local file paths and base64-encoded data for flexibility.
//
// Parameters:
//   - ctx: Context for the operation
//   - chatJID: Target chat JID
//   - filePath: Local path to document file (used if provided)
//   - fileData: Base64-encoded document data (used if filePath is empty)
//   - filename: Filename for the document (required if using fileData)
//
// File size limits: 2 GB for documents (enforced)
// Supported formats: PDF, DOCX, ZIP, TXT, etc.
func (c *Client) SendDocument(ctx context.Context, chatJID, filePath, fileData, filename string) error {
	// Parse JID
	targetJID, err := types.ParseJID(chatJID)
	if err != nil {
		return fmt.Errorf("invalid chat JID: %w", err)
	}

	// Load file data
	data, detectedFilename, err := c.loadFileData(filePath, fileData)
	if err != nil {
		return fmt.Errorf("failed to load file data: %w", err)
	}

	// Determine filename (use provided filename if given, otherwise use detected filename)
	finalFilename := filename
	if finalFilename == "" {
		finalFilename = detectedFilename
		if finalFilename == "" {
			return fmt.Errorf("filename must be provided when using file_data")
		}
	}

	// Validate file size
	if err := c.validateFileSize(data, maxDocumentSize, "document"); err != nil {
		return err
	}

	// Detect MIME type
	mimeType := c.detectMimeType(finalFilename, data)

	// Upload to WhatsApp
	uploadResp, err := c.wa.Upload(ctx, data, whatsmeow.MediaDocument)
	if err != nil {
		return fmt.Errorf("failed to upload document: %w", err)
	}

	// Construct document message
	docMsg := &waE2E.DocumentMessage{
		FileName:      proto.String(finalFilename),
		Mimetype:      proto.String(mimeType),
		URL:           &uploadResp.URL,
		DirectPath:    &uploadResp.DirectPath,
		MediaKey:      uploadResp.MediaKey,
		FileEncSHA256: uploadResp.FileEncSHA256,
		FileSHA256:    uploadResp.FileSHA256,
		FileLength:    &uploadResp.FileLength,
	}

	// Send message
	resp, err := c.wa.SendMessage(ctx, targetJID, &waE2E.Message{
		DocumentMessage: docMsg,
	})
	if err != nil {
		return fmt.Errorf("failed to send document message: %w", err)
	}

	// Save to database
	c.store.SaveMessage(storage.Message{
		ID:          resp.ID,
		ChatJID:     chatJID,
		SenderJID:   resp.Sender.String(),
		Text:        finalFilename,
		Timestamp:   resp.Timestamp,
		IsFromMe:    true,
		MessageType: "document",
	})

	c.log.Infof("Sent document to %s (filename: %s, size: %d bytes)", chatJID, finalFilename, len(data))
	return nil
}

// SendAudio sends an audio file to a WhatsApp chat.
//
// This method uploads the audio file to WhatsApp servers and sends it to the specified chat.
// It supports both local file paths and base64-encoded data for flexibility.
//
// Parameters:
//   - ctx: Context for the operation
//   - chatJID: Target chat JID
//   - filePath: Local path to audio file (used if provided)
//   - fileData: Base64-encoded audio data (used if filePath is empty)
//
// File size limits: 16 MB for audio files (enforced)
// Supported formats: MP3, OGG, AAC, M4A
func (c *Client) SendAudio(ctx context.Context, chatJID, filePath, fileData string) error {
	// Parse JID
	targetJID, err := types.ParseJID(chatJID)
	if err != nil {
		return fmt.Errorf("invalid chat JID: %w", err)
	}

	// Load file data
	data, filename, err := c.loadFileData(filePath, fileData)
	if err != nil {
		return fmt.Errorf("failed to load file data: %w", err)
	}

	// Validate file size
	if err := c.validateFileSize(data, maxAudioSize, "audio"); err != nil {
		return err
	}

	// Detect MIME type
	mimeType := c.detectMimeType(filename, data)

	// Upload to WhatsApp
	uploadResp, err := c.wa.Upload(ctx, data, whatsmeow.MediaAudio)
	if err != nil {
		return fmt.Errorf("failed to upload audio: %w", err)
	}

	// Construct audio message
	audioMsg := &waE2E.AudioMessage{
		Mimetype:      proto.String(mimeType),
		URL:           &uploadResp.URL,
		DirectPath:    &uploadResp.DirectPath,
		MediaKey:      uploadResp.MediaKey,
		FileEncSHA256: uploadResp.FileEncSHA256,
		FileSHA256:    uploadResp.FileSHA256,
		FileLength:    &uploadResp.FileLength,
	}

	// Send message
	resp, err := c.wa.SendMessage(ctx, targetJID, &waE2E.Message{
		AudioMessage: audioMsg,
	})
	if err != nil {
		return fmt.Errorf("failed to send audio message: %w", err)
	}

	// Save to database
	c.store.SaveMessage(storage.Message{
		ID:          resp.ID,
		ChatJID:     chatJID,
		SenderJID:   resp.Sender.String(),
		Timestamp:   resp.Timestamp,
		IsFromMe:    true,
		MessageType: "audio",
	})

	c.log.Infof("Sent audio to %s (size: %d bytes)", chatJID, len(data))
	return nil
}
