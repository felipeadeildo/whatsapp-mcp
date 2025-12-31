package mcp

import (
	"log"
	"time"

	"whatsapp-mcp/storage"
	"whatsapp-mcp/whatsapp"

	"github.com/mark3labs/mcp-go/server"
)

type MCPServer struct {
	server   *server.MCPServer
	wa       *whatsapp.Client
	store    *storage.MessageStore
	log      *log.Logger
	timezone *time.Location
}

func NewMCPServer(wa *whatsapp.Client, store *storage.MessageStore, timezone *time.Location) *MCPServer {
	s := server.NewMCPServer(
		"WhatsApp MCP",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithRecovery(),
	)

	m := &MCPServer{
		server:   s,
		wa:       wa,
		store:    store,
		log:      log.Default(),
		timezone: timezone,
	}

	// register all tools
	m.registerTools()

	return m
}

func (m *MCPServer) GetServer() *server.MCPServer {
	return m.server
}
