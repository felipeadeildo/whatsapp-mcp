package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"whatsapp-mcp/mcp"
	"whatsapp-mcp/storage"
	"whatsapp-mcp/whatsapp"

	"github.com/mark3labs/mcp-go/server"
	"github.com/mdp/qrterminal/v3"
	"github.com/skip2/go-qrcode"
)

func main() {
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

	// initialize database
	db, err := storage.InitDB("./data/messages.db")
	if err != nil {
		log.Fatal("Failed to init DB:", err)
	}
	defer db.Close()

	store := storage.NewMessageStore(db)
	log.Println("Message storage initialized")

	// initialize WhatsApp client
	waClient, err := whatsapp.NewClient(store, "./data/whatsapp_auth.db")
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
				qrcode.WriteFile(evt.Code, qrcode.Low, 256, "./qr.png")
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
	mcpServer := mcp.NewMCPServer(waClient, store)
	log.Println("MCP server initialized")

	// create streamable HTTP server with authentication
	httpServer := server.NewStreamableHTTPServer(
		mcpServer.GetServer(),
		server.WithEndpointPath("/mcp"),
		server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			// validate API key from Authorization header
			auth := r.Header.Get("Authorization")
			expectedAuth := "Bearer " + apiKey

			if auth != expectedAuth {
				// mark as unauthorized in context
				return context.WithValue(ctx, "unauthorized", true)
			}

			return ctx
		}),
	)

	// add health check endpoint on separate port or handle separately
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if waClient.IsLoggedIn() {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("WhatsApp not connected"))
		}
	})

	// start health check server
	healthServer := &http.Server{
		Addr:    ":8081",
		Handler: healthMux,
	}
	go func() {
		log.Println("Health check available at http://0.0.0.0:8081/health")
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Health server error: %v", err)
		}
	}()

	// start MCP server in background
	go func() {
		log.Printf("Starting MCP server on http://0.0.0.0:%s/mcp", httpPort)
		if err := httpServer.Start(":" + httpPort); err != nil {
			log.Fatalf("MCP server error: %v", err)
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

	// shutdown health server
	if err := healthServer.Shutdown(ctx); err != nil {
		log.Printf("Health server shutdown error: %v", err)
	}

	// disconnect WhatsApp
	waClient.Disconnect()
	log.Println("Shutdown complete")
}
