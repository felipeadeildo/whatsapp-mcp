package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"whatsapp-mcp/storage"
	"whatsapp-mcp/whatsapp"

	"github.com/mdp/qrterminal/v3"
	"github.com/skip2/go-qrcode"
)

func main() {
	db, err := storage.InitDB("./data/messages.db")
	if err != nil {
		log.Fatal("Failed to init DB:", err)
	}
	defer db.Close()

	store := storage.NewMessageStore(db)
	log.Println("Message storage initialized")

	waClient, err := whatsapp.NewClient(store, "./data/whatsapp_auth.db")
	if err != nil {
		log.Fatal("Failed to create WhatsApp client:", err)
	}
	log.Println("WhatsApp client created")

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

	log.Println("WhatsApp MCP running. Press Ctrl+C to stop.")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("\nShutting down...")
	waClient.Disconnect()
}
