package whatsapp

import (
	"context"
	"fmt"
	"os"
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

	// Configure retry receipt handler: when a recipient can't decrypt a message,
	// WhatsApp sends a retry receipt. This callback loads the original protobuf
	// from the database so the message can be re-encrypted and resent.
	waClient.GetMessageForRetry = func(requester, to types.JID, id types.MessageID) *waE2E.Message {
		protoBytes, err := store.GetMessageProto(string(id))
		if err != nil {
			logger.Warnf("Error loading message proto for retry %s: %v", id, err)
			return nil
		}
		if protoBytes == nil {
			logger.Debugf("No stored proto for retry %s", id)
			return nil
		}
		var msg waE2E.Message
		if err := proto.Unmarshal(protoBytes, &msg); err != nil {
			logger.Warnf("Error unmarshalling message proto for retry %s: %v", id, err)
			return nil
		}
		logger.Infof("Loaded message proto for retry %s", id)
		return &msg
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

	// Add to recent messages cache for retry receipt handling
	c.wa.DangerousInternals().AddRecentMessage(targetJID, resp.ID, &waE2E.Message{Conversation: proto.String(text)}, nil)

	c.store.SaveMessage(storage.Message{
		ID:          resp.ID,
		ChatJID:     chatJID,
		SenderJID:   resp.Sender.String(),
		Text:        text,
		Timestamp:   resp.Timestamp,
		IsFromMe:    true,
		MessageType: "text",
	})

	// Persist protobuf for retry receipt handling across restarts
	if protoBytes, err := proto.Marshal(&waE2E.Message{Conversation: proto.String(text)}); err == nil {
		c.store.SaveMessageProto(resp.ID, protoBytes)
	} else {
		c.log.Warnf("Failed to marshal message proto for %s: %v", resp.ID, err)
	}

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
