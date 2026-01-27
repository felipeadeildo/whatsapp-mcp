# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## ⚠️ CRITICAL: Always Follow CONTRIBUTING.md

**Before making ANY changes to this repository:**
1. Read `CONTRIBUTING.md` completely
2. Follow the development workflow exactly as specified
3. Adhere to code style guidelines strictly
4. Test locally before committing
5. Create migrations if database schema changes

**No exceptions. No shortcuts.**

Key requirements from CONTRIBUTING.md:
- Create feature branches: `git checkout -b feat/your-feature`
- Follow Go conventions (gofmt, golint)
- Add comments for exported functions
- Keep functions small and focused
- Handle errors explicitly
- Test with `go test ./...`
- Use `go run cmd/migrate/main.go create description` for schema changes

## Project Overview

WhatsApp MCP Server is a Model Context Protocol server that bridges WhatsApp Web and AI assistants. It uses the `whatsmeow` library to connect to WhatsApp, stores messages in SQLite, and exposes them through MCP tools, prompts, and resources.

**Key Architecture:**
- **MCP Layer** (`mcp/`) - MCP protocol implementation with tools, prompts, resources
- **WhatsApp Client** (`whatsapp/`) - whatsmeow wrapper with event handlers
- **Storage Layer** (`storage/`) - SQLite persistence with migrations
- **Webhook System** (`webhook/`) - Async HTTP event delivery
- **Entry Point** (`main.go`) - Initialization, HTTP server, graceful shutdown

## Common Commands

### Build & Run
```bash
# Run the server (local)
go run main.go

# Build binary
go build -o whatsapp-mcp main.go

# Build migration CLI tool
go build -o migrate cmd/migrate/main.go
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run specific test
go test -run TestFunctionName ./...
```

### Docker
```bash
# Build and run
docker compose up --build

# View logs
docker compose logs -f

# Rebuild after changes
docker compose down && docker compose up --build
```

### Database Migrations
```bash
# Create a new migration
go run cmd/migrate/main.go create feature_description

# Apply migrations (runs automatically on server start)
go run cmd/migrate/main.go upgrade latest

# Check migration status
go run cmd/migrate/main.go status
```

## Architecture Notes

### Two Main Programs

1. **`main.go`** - WhatsApp MCP server
   - Initializes storage, WhatsApp client, MCP server
   - HTTP server on `:8080` (configurable via `MCP_PORT`)
   - MCP endpoint: `/mcp/{API_KEY}` with streamable HTTP transport
   - Health check: `/health`

2. **`cmd/migrate/main.go`** - Database migration CLI
   - Standalone tool for managing migrations
   - Commands: `create`, `upgrade`, `status`
   - Follows Go convention of separate commands in `cmd/`

### MCP Tool Pattern

Tools are registered in `mcp/tools.go` and implemented in `mcp/handlers.go`:

```go
// Registration (mcp/tools.go)
m.server.AddTool(
    mcp.NewTool("tool_name",
        mcp.WithDescription("..."),
        mcp.WithString("param", mcp.Required(), mcp.Description("...")),
    ),
    m.handleToolName,
)

// Handler (mcp/handlers.go)
func (m *MCPServer) handleToolName(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    // Extract params with request.RequireString(), GetString(), etc.
    // Call WhatsApp client methods
    // Return formatted response with mcp.NewToolResultText()
}
```

### WhatsApp Client Wrapper

The `whatsapp.Client` wraps `whatsmeow.Client`:
- **Authentication:** QR code flow via `GetQRChannel()` on first run
- **Sending:** `SendTextMessage()` - uploads to WhatsApp, sends to JID, saves to DB
- **History:** `RequestHistorySync()` - on-demand loading from servers with wait/sync modes
- **Profile:** `GetMyInfo()` - fetch user profile data
- **Events:** `eventHandler()` - real-time message sync to database

### Database Migration System

Custom migration system in `storage/`:
- Migration files: `storage/migrations/XXX_description.sql` (numbered, sequential)
- Tracking table: `schema_migrations` with version, checksum, applied_at
- Auto-applied on server startup via `storage.InitDB()`
- **Never modify applied migrations** - create new ones to fix issues
- Use `IF NOT EXISTS` clauses for idempotency

### Media Handling (Incoming Only - Currently)

Media receiving is fully implemented:
- Auto-download with configurable filters (type, size, source)
- Storage in `data/media/{images,videos,audio,documents}/`
- Metadata in `media` table with download status tracking
- MCP resource: `whatsapp://media/{message_id}`

**Media sending is NOT yet implemented** - this is a planned feature.

### Webhook System

Asynchronous HTTP delivery with retry logic:
- Events: `message` (new messages received)
- Stored in `webhooks` table with active/inactive status
- Management API: `/api/webhooks` (authenticated with API key)
- Primary webhook configurable via `WEBHOOK_URL` env var (registered as `system:primary`)

### Storage Layer

Three main stores in `storage/`:
- **MessageStore** - chats and messages with views for enriched data
- **MediaStore** - media metadata and download status
- **WebhookStore** - webhook registrations

Uses `modernc.org/sqlite` (pure Go, no CGo) with WAL mode for performance.

## Configuration

Environment variables (see `.env.example`):
- `MCP_API_KEY` - API key for MCP endpoint authentication
- `MCP_PORT` - HTTP server port (default: 8080)
- `LOG_LEVEL` - WhatsApp client logging (DEBUG, INFO, WARN, ERROR)
- `TIMEZONE` - Local timezone for message timestamps (default: UTC)
- `WEBHOOK_URL` - Primary webhook URL for message events

Media auto-download configuration in `data/media_config.json`:
- `auto_download_enabled` - Enable/disable auto-download
- `auto_download_max_size` - Size limit in bytes
- `auto_download_types` - Per-type flags (image, video, audio, document, sticker)
- `auto_download_from_history` - Download media from history sync

## Development Workflow

1. **Create feature branch:** `git checkout -b feat/your-feature`
2. **Make changes** following the code patterns above
3. **If schema changes:** Create migration with `go run cmd/migrate/main.go create description`
4. **Test locally:** `go run main.go`
5. **Commit and push**
6. **Open PR** with clear description

## Important Gotchas

- **JID Format:** WhatsApp JIDs are `phone_number@s.whatsapp.net` for individuals, `group_id@g.us` for groups. Always use `find_chat` to get JIDs before operations.
- **WhatsApp Connection:** Must be authenticated (QR code on first run) and connected before any operations. Check `waClient.IsLoggedIn()`.
- **Timezone Handling:** All timestamps stored in UTC, displayed in configured timezone via `toLocalTime()` and `formatDateTime()` helpers.
- **MCP Instructions:** The server has built-in instructions for AI assistants - use prompts and resources for common workflows.
- **Graceful Shutdown:** 5-second timeout for HTTP server shutdown, webhook manager stop, WhatsApp disconnect.

## whatsmeow Integration Notes

- Uses `go.mau.fi/whatsmeow` library for WhatsApp Web protocol
- Client store in SQLite (`data/db/whatsapp_auth.db`) for session persistence
- Dual logging: stdout + `data/whatsapp.log` file for debugging
- Message types: text, image, video, audio, document, sticker (receiving only for media)
- Event-driven architecture via `AddEventHandler()` for real-time sync
