package whatsapp

import (
	"context"
	"fmt"
	"os"
	"whatsapp-mcp/storage"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

type Client struct {
	wa      *whatsmeow.Client
	store   *storage.MessageStore
	log     waLog.Logger
	logFile *os.File
}

type fileLogger struct {
	base waLog.Logger
	file *os.File
}

func (l *fileLogger) Errorf(msg string, args ...interface{}) {
	l.base.Errorf(msg, args...)
	fmt.Fprintf(l.file, "[ERROR] "+msg+"\n", args...)
}

func (l *fileLogger) Warnf(msg string, args ...interface{}) {
	l.base.Warnf(msg, args...)
	fmt.Fprintf(l.file, "[WARN] "+msg+"\n", args...)
}

func (l *fileLogger) Infof(msg string, args ...interface{}) {
	l.base.Infof(msg, args...)
	fmt.Fprintf(l.file, "[INFO] "+msg+"\n", args...)
}

func (l *fileLogger) Debugf(msg string, args ...interface{}) {
	l.base.Debugf(msg, args...)
	fmt.Fprintf(l.file, "[DEBUG] "+msg+"\n", args...)
}

func (l *fileLogger) Sub(module string) waLog.Logger {
	return &fileLogger{
		base: l.base.Sub(module),
		file: l.file,
	}
}

func NewClient(store *storage.MessageStore, dbPath string, logLevel string) (*Client, error) {
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

	// Create log file in data directory
	logFile, err := os.OpenFile("./data/whatsapp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Create base logger for stdout
	baseLogger := waLog.Stdout("whatsapp", logLevel, true)

	// Wrap with file logger
	logger := &fileLogger{
		base: baseLogger,
		file: logFile,
	}

	logger.Infof("Initializing WhatsApp client with log level: %s (logging to ./data/whatsapp.log)", logLevel)

	ctx := context.Background()

	container, err := sqlstore.New(ctx, "sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create sqlstore: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get  device: %w", err)
	}

	// Configure browser session and device settings for realistic WhatsApp Web appearance
	waStore.BaseClientPayload.UserAgent.Device = proto.String("Whatsapp MCP")
	waStore.BaseClientPayload.UserAgent.Manufacturer = proto.String("Google Inc.")
	waStore.BaseClientPayload.UserAgent.OsVersion = proto.String("Linux x86_64")
	waStore.DeviceProps.Os = proto.String("Linux")

	waClient := whatsmeow.NewClient(deviceStore, logger)

	// Set display name
	waClient.Store.PushName = "Whatsapp MCP"

	client := &Client{
		wa:      waClient,
		store:   store,
		log:     logger,
		logFile: logFile,
	}

	waClient.AddEventHandler(client.eventHandler)

	return client, nil
}

func (c *Client) IsLoggedIn() bool {
	return c.wa.Store.ID != nil
}

func (c *Client) Connect() error {
	return c.wa.Connect()
}

func (c *Client) Disconnect() {
	c.wa.Disconnect()
	if c.logFile != nil {
		c.logFile.Close()
	}
}

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

	// Extract JID pairs for storage
	chatPN, chatLID := c.extractJIDPair(targetJID, types.EmptyJID)
	senderPN, senderLID := c.extractJIDPair(resp.Sender, types.EmptyJID)

	c.store.SaveMessage(storage.Message{
		ID:           resp.ID,
		ChatJIDPN:    chatPN,
		ChatJIDLID:   chatLID,
		SenderJIDPN:  senderPN,
		SenderJIDLID: senderLID,
		Text:         text,
		Timestamp:    resp.Timestamp,
		IsFromMe:     true,
		MessageType:  "text",
	})

	return nil
}
