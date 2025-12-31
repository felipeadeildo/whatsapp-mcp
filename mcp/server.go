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
		server.WithInstructions(`WhatsApp integration for messaging operations.

Key workflow: find_chat → get_chat_messages or send_message
Always get chat_jid from find_chat before other operations.
JIDs are WhatsApp identifiers (e.g., 5511999999999@s.whatsapp.net).

Use prompts for common workflows or resources for detailed guides.`),
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
		server.WithResourceCapabilities(true, false),
		server.WithRecovery(),
	)

	m := &MCPServer{
		server:   s,
		wa:       wa,
		store:    store,
		log:      log.Default(),
		timezone: timezone,
	}

	// register all capabilities
	m.registerTools()
	m.registerPrompts()
	m.registerResources()

	return m
}

func (m *MCPServer) GetServer() *server.MCPServer {
	return m.server
}
