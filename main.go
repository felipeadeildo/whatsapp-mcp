// WhatsApp MCP Server provides AI assistants access to WhatsApp conversations.
//
// This server implements the Model Context Protocol (MCP) specification to expose
// WhatsApp messages and chats through a standardized interface. It connects to
// WhatsApp Web using your existing account and persists all messages locally in
// SQLite for fast searching and retrieval.
//
// The server supports:
//   - Real-time message syncing from WhatsApp
//   - Full-text search across all conversations
//   - Pattern matching with glob support
//   - Media download and storage
//   - On-demand history loading
//   - Timezone-aware message formatting
//
// Configuration is done via environment variables (see .env.example).
// Authentication uses QR code scanning on first launch.
//
// Usage:
//
//	whatsapp-mcp
//
// The server runs as an MCP stdio server and communicates via JSON-RPC.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"whatsapp-mcp/mcp"
	"whatsapp-mcp/paths"
	"whatsapp-mcp/storage"
	"whatsapp-mcp/webhook"
	"whatsapp-mcp/whatsapp"

	"github.com/joho/godotenv"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mdp/qrterminal/v3"
	"github.com/skip2/go-qrcode"
)

// TODO: cleanup main entry
// TODO: move dotenv loading to a separate package
// TODO: move services initialization to a separate package
// TODO: move and improve api/mcp endpoints registration

func main() {
	// load .env file
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found, using environment variables only")
	}

	// get API key from environment
	apiKey := os.Getenv("MCP_API_KEY")
	if apiKey == "" {
		log.Println("Warning: MCP_API_KEY not set, using default (insecure!)")
		apiKey = "change-me-in-production"
	}

	// get HTTP port from environment
	httpPort := os.Getenv("MCP_PORT")
	if httpPort == "" {
		httpPort = "8080"
	}

	// get log level from environment
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "INFO"
	}
	log.Printf("Log level: %s", logLevel)

	// get timezone from environment
	timezoneName := os.Getenv("TIMEZONE")
	if timezoneName == "" {
		timezoneName = "UTC"
	}
	timezone, err := time.LoadLocation(timezoneName)
	if err != nil {
		log.Printf("Warning: Invalid timezone '%s', using UTC: %v", timezoneName, err)
		timezone = time.UTC
	}
	log.Printf("Timezone: %s", timezone.String())

	// ensure data directories exist
	if err := paths.EnsureDataDirectories(); err != nil {
		log.Fatal("Failed to create data directories:", err)
	}

	// initialize database
	db, err := storage.InitDB()
	if err != nil {
		log.Fatal("Failed to init DB:", err)
	}
	defer db.Close()

	store := storage.NewMessageStore(db)
	log.Println("Message storage initialized")

	mediaStore := storage.NewMediaStore(db)
	log.Println("Media storage initialized")

	// initialize webhook system
	webhookConfig := webhook.LoadConfig()
	webhookStore := storage.NewWebhookStore(db)
	webhookLogger := log.New(os.Stdout, "[WEBHOOK] ", log.LstdFlags)
	webhookManager := webhook.NewWebhookManager(webhookStore, webhookConfig, webhookLogger)

	// Register primary webhook from env var if configured
	if webhookConfig.PrimaryURL != "" {
		primaryWebhook := storage.WebhookRegistration{
			ID:         "system:primary",
			URL:        webhookConfig.PrimaryURL,
			EventTypes: []string{"message"},
			Active:     true,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		// Use upsert to create or update the primary webhook
		if err := webhookStore.UpsertWebhook(primaryWebhook); err != nil {
			log.Printf("Warning: Failed to register primary webhook: %v", err)
		} else {
			log.Println("Primary webhook registered from WEBHOOK_URL")
		}
	}

	webhookManager.Start()
	log.Println("Webhook manager started")

	// initialize WhatsApp client
	waClient, err := whatsapp.NewClient(store, mediaStore, webhookManager, logLevel)
	if err != nil {
		log.Fatal("Failed to create WhatsApp client:", err)
	}
	log.Println("WhatsApp client created")

	// check authentication and connect
	if !waClient.IsLoggedIn() {
		log.Println("Not logged in. Please scan QR code:")

		ctx := context.Background()
		qrChan, err := waClient.GetQRChannel(ctx)
		if err != nil {
			log.Fatal("Failed to get QR channel:", err)
		}

		for evt := range qrChan {
			if evt.Event == "code" {
				fmt.Println("\nScan the QR code below:")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				fmt.Println("\nQR Code also saved to qr.png")
				qrcode.WriteFile(evt.Code, qrcode.Low, 256, paths.QRCodePath)
			} else {
				log.Println("QR event:", evt.Event)
			}
		}
	} else {
		log.Println("Already logged in")

		if err := waClient.Connect(); err != nil {
			log.Fatal("Failed to connect:", err)
		}
		log.Println("Connected to WhatsApp")
	}

	// initialize MCP server
	mcpServer := mcp.NewMCPServer(waClient, store, mediaStore, timezone)
	log.Println("MCP server initialized")

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if waClient.IsLoggedIn() {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("WhatsApp not connected"))
		}
	})

	streamableServer := server.NewStreamableHTTPServer(
		mcpServer.GetServer(),
		server.WithEndpointPath("/mcp"),
	)

	// MCP endpoint with API key in path
	mux.HandleFunc("/mcp/", func(w http.ResponseWriter, r *http.Request) {
		// Extract API key from URL path: /mcp/{apiKey}
		path := strings.TrimPrefix(r.URL.Path, "/mcp/")
		providedKey := strings.Split(path, "/")[0] // Get first segment after /mcp/

		if providedKey != apiKey {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Unauthorized: Invalid API key"))
			return
		}

		// Create a new request with the remaining path
		remainingPath := strings.TrimPrefix(path, providedKey)
		if !strings.HasPrefix(remainingPath, "/") {
			remainingPath = "/" + remainingPath
		}
		r.URL.Path = "/mcp" + remainingPath

		// Serve the MCP request
		streamableServer.ServeHTTP(w, r)
	})

	// Webhook management API
	webhookHandler := webhook.NewHandler(webhookManager, webhookStore, apiKey)

	mux.HandleFunc("/api/webhooks", func(w http.ResponseWriter, r *http.Request) {
		if !webhookHandler.ValidateAuth(r) {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		switch r.Method {
		case http.MethodPost:
			webhookHandler.CreateWebhook(w, r)
		case http.MethodGet:
			webhookHandler.ListWebhooks(w, r)
		default:
			http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/webhooks/", func(w http.ResponseWriter, r *http.Request) {
		if !webhookHandler.ValidateAuth(r) {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		webhookHandler.HandleWebhookByID(w, r)
	})

	httpServer := &http.Server{
		Addr:    ":" + httpPort,
		Handler: mux,
	}

	// start server in background
	go func() {
		log.Printf("Starting server on http://0.0.0.0:%s", httpPort)
		log.Printf("- Health check: http://0.0.0.0:%s/health", httpPort)
		log.Printf("- MCP endpoint: http://0.0.0.0:%s/mcp/{API_KEY}", httpPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	log.Println("WhatsApp MCP running. Press Ctrl+C to stop.")

	// wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("\nShutting down...")

	// graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// shutdown HTTP server
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	// stop webhook manager
	webhookManager.Stop()
	log.Println("Webhook manager stopped")

	// disconnect WhatsApp
	waClient.Disconnect()
	log.Println("Shutdown complete")

}
