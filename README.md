<div align="center">

# WhatsApp MCP Server

**Give AI assistants access to your WhatsApp conversations**

[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![MCP Protocol](https://img.shields.io/badge/MCP-Compatible-7C3AED?style=flat)](https://modelcontextprotocol.io)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker&logoColor=white)](https://www.docker.com/)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg?style=flat)](LICENSE)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/felipeadeildo/whatsapp-mcp)

*Built with [whatsmeow](https://github.com/tulir/whatsmeow) and [mcp-go](https://github.com/mark3labs/mcp-go)*

[Features](#-features) • [Quick Start](#-quick-start) • [Architecture](#-architecture) • [MCP Integration](#-mcp-integration)

</div>

## 🎯 What is This?

A **Model Context Protocol (MCP) server** that bridges WhatsApp and AI assistants like Claude. It exposes your WhatsApp messages through standardized MCP tools, prompts, and resources - allowing AI to read, search, and send messages on your behalf.

**The Vision:** Let AI handle your WhatsApp conversations intelligently, with full context and natural language understanding.

```
You: "Summarize what João said about the budget meeting"
AI:  *searches all your chats* → "João mentioned in the Tech Team group..."

You: "Reply to Maria's last message and schedule lunch"
AI:  *reads context, sends reply* → "Sent! I've proposed Thursday at noon"
```

## ✨ Features

### Core Capabilities

- **📱 Full WhatsApp Integration** - Connect to WhatsApp Web using your existing account
- **💾 Local-First Storage** - All messages stored in SQLite, synced in real-time
- **🔍 Powerful Search** - Pattern matching, cross-chat queries, sender filtering
- **⏱️ Timezone Support** - Messages displayed in your local timezone
- **📥 On-Demand Loading** - Fetch older messages from WhatsApp servers as needed
- **🔐 Secure by Design** - API key authentication, local data storage, HTTPS ready

### MCP Features

This server implements the full MCP specification with:

- **7 Tools** for WhatsApp operations
- **4 Prompts** for common workflows
- **4 Resources** for interactive guides
- **Server Instructions** for optimal AI interactions

#### Tools

| Tool | Purpose | Highlights |
|------|---------|-----------|
| `list_chats` | Browse conversations | Ordered by recent activity |
| `get_chat_messages` | Read specific chat | Pagination, sender filtering |
| `search_messages` | Search across all chats | Pattern matching, wildcards |
| `find_chat` | Locate chat by name | Fuzzy search support |
| `send_message` | Send WhatsApp messages | To any chat or group |
| `load_more_messages` | Fetch older history | On-demand from servers |
| `get_my_info` | Get your profile info | JID, name, status, picture |

#### Prompts

Pre-built workflows that guide AI assistants:

- **`search_person_messages`** - Find ALL messages from someone across all chats
- **`get_context_about_person`** - Comprehensive analysis of someone's messages
- **`analyze_conversation`** - Summarize recent chat activity
- **`search_keyword`** - Find specific topics across conversations

#### Resources

Interactive documentation embedded in the MCP server:

- **Cross-Chat Search Guide** - Master advanced search workflows
- **Workflow Guide** - Common operations and best practices
- **JID Format Guide** - Understanding WhatsApp identifiers
- **Search Patterns Guide** - Wildcards and pattern matching

## 🏗️ Architecture

```mermaid
graph TB
    subgraph "AI Client"
        A[AI Assistant <br/> e.g., Claude Web]
    end

    subgraph "WhatsApp MCP Server"
        B[MCP HTTP Server :8080]
        C[MCP Layer]
        D[WhatsApp Client]
        E[(SQLite Database)]

        B -->|/mcp endpoint| C
        B -->|/health| B

        C -->|Tools| C1[list_chats<br/>get_chat_messages<br/>search_messages<br/>find_chat<br/>send_message<br/>load_more_messages<br/>get_my_info]
        C -->|Prompts| C2[search_person_messages<br/>get_context_about_person<br/>analyze_conversation<br/>search_keyword]
        C -->|Resources| C3[Workflow Guides<br/>Search Patterns<br/>JID Format]

        C1 -.->|read/write| E
        C1 -.->|send| D

        D -->|sync messages| E
        D <-->|WhatsApp Protocol| F
    end

    subgraph "WhatsApp"
        F[WhatsApp Servers]
    end

    A <-->|Streamable HTTP<br/>API Key Auth| B

    style A fill:#4A90E2,stroke:#2E5C8A,stroke-width:2px,color:#000
    style B fill:#F5A623,stroke:#C67E1B,stroke-width:2px,color:#000
    style C fill:#9013FE,stroke:#6B0FC7,stroke-width:2px,color:#fff
    style C1 fill:#50E3C2,stroke:#3AAA94,stroke-width:2px,color:#000
    style C2 fill:#BD10E0,stroke:#9012FE,stroke-width:2px,color:#fff
    style C3 fill:#F5A623,stroke:#C67E1B,stroke-width:2px,color:#000
    style D fill:#50E3C2,stroke:#3AAA94,stroke-width:2px,color:#000
    style E fill:#E85D75,stroke:#B5475C,stroke-width:2px,color:#fff
    style F fill:#25D366,stroke:#1DA851,stroke-width:2px,color:#000
```

### How It Works

1. **Initial Sync** - WhatsApp sends message history on first connection
2. **Real-Time Updates** - All new messages automatically stored in SQLite
3. **MCP Exposure** - Tools, prompts, and resources expose functionality to AI
4. **On-Demand Loading** - Fetch older messages from WhatsApp when needed
5. **AI Integration** - Claude (or any MCP client) accesses WhatsApp through standardized protocol

## 🚀 Quick Start

### Prerequisites

- **Go 1.25.5+** (for local setup) or **Docker** (recommended)
- **WhatsApp account** (will be linked via QR code)
- **MCP-compatible AI client** (Claude, Cursor, etc.)

### Option 1: Docker Setup (Recommended)

1. **Clone and configure**
   ```bash
   git clone https://github.com/felipeadeildo/whatsapp-mcp
   cd whatsapp-mcp
   cp .env.example .env
   # Edit .env with your settings (API key, timezone, etc.)
   ```

2. **Start the server**
   ```bash
   docker compose up -d
   ```

3. **Link WhatsApp**
   ```bash
   # View logs to see QR code
   docker compose logs -f whatsapp-mcp

   # Scan QR code with WhatsApp mobile app:
   # Settings → Linked Devices → Link a Device
   ```

4. **Verify it's running**
   ```bash
   curl http://localhost:8080/health
   # Expected: "OK"
   ```

### Option 2: Local Setup

1. **Install dependencies**
   ```bash
   git clone https://github.com/felipeadeildo/whatsapp-mcp
   cd whatsapp-mcp
   go mod download
   ```

2. **Configure environment**
   ```bash
   cp .env.example .env
   # Edit .env with your settings
   ```

3. **Run the server**
   ```bash
   go run main.go
   ```

4. **Link WhatsApp** (scan QR code shown in terminal)

## 🔌 MCP Integration

### Connect to Claude Desktop

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "whatsapp": {
      "type": "http",
      "url": "http://localhost:8080/mcp",
      "headers": {
        "Authorization": "Bearer your-secret-api-key"
      }
    }
  }
}
```

### Connect to Other MCP Clients

The server exposes a Streamable HTTP endpoint compatible with any MCP client:

- **URL:** `http://localhost:8080/mcp`
- **Transport:** Streamable HTTP
- **Authentication:** Two methods supported (Bearer header preferred)

**Option A — Authorization header (recommended):**

Keeps the key out of URLs, proxy logs, and shell history.

```json
{
  "mcpServers": {
    "whatsapp": {
      "type": "http",
      "url": "http://localhost:8080/mcp",
      "headers": {
        "Authorization": "Bearer your-secret-api-key"
      }
    }
  }
}
```

**Option B — Key in URL path (backward compatible):**

Existing clients using `/mcp/{key}` continue to work unchanged.

```json
{
  "mcpServers": {
    "whatsapp": {
      "url": "http://localhost:8080/mcp/your-secret-api-key",
      "type": "http"
    }
  }
}
```

## 🎨 Usage Examples

Once connected, your AI assistant can:

### Search for People
```
You: "Find all messages from Arthur across all my chats"
AI: [Uses search_person_messages prompt]
    → Finds messages in DMs, groups, everywhere
    → Analyzes communication patterns
    → Provides context about Arthur
```

### Analyze Conversations
```
You: "What did we discuss in the Tech Team group this week?"
AI: [Uses analyze_conversation prompt]
    → Reads recent messages
    → Summarizes key topics
    → Lists action items and deadlines
```

### Smart Messaging
```
You: "Tell Maria I'll be 10 minutes late"
AI: [Uses find_chat + send_message]
    → Finds Maria's chat
    → Sends contextual message
    → Confirms delivery
```

### Deep Search
```
You: "Find all mentions of 'budget meeting' in any chat"
AI: [Uses search_keyword prompt]
    → Searches across all conversations
    → Shows context around each mention
    → Orders by relevance/date
```

## 📊 Data & Privacy

### Local Storage

All data is stored in `./data/`:
- **`db/`** - Database files
  - `messages.db` - SQLite database with messages and chats
  - `whatsapp_auth.db` - WhatsApp session credentials
- **`media/`** - Downloaded media files
- **`whatsapp.log`** - WhatsApp client logs

**⚠️ Important:** Database files contain sensitive data. Keep them secure (file permissions `600`) and backed up.

## 🛣️ Roadmap

### ✅ Implemented

- [x] WhatsApp Web integration via whatsmeow
- [x] Real-time message sync to SQLite
- [x] MCP server with Streamable HTTP transport
- [x] Pattern matching and wildcards
- [x] Sender filtering and cross-chat search
- [x] Timestamp-based pagination
- [x] Timezone support
- [x] On-demand message loading from servers
- [x] Docker deployment (with healthcheck!)

### 🚧 Planned

- [ ] **Media Support**
  - Voice message transcription
  - Image OCR and analysis
  - Video metadata extraction
  - Document parsing
  - Contact card handling

- [ ] **GraphRAG Integration**
  - Entity extraction from conversations
  - Relationship mapping between contacts
  - Semantic search capabilities
  - Context-aware recommendations

- [ ] **Enhanced Tools**
  - Mark messages as read
  - React to messages (emoji reactions)
  - Send media files
  - Group management (create, members)
  - Status updates
  - Account management (profile picture, name)

- [ ] **Analytics** (maybe)
  - Message statistics
  - Conversation insights
  - Response time tracking

## 📚 Documentation

### MCP Resources (Built-In)

The server includes interactive guides accessible through MCP:
- **Workflow Guide** - Common operations and patterns
- **Cross-Chat Search** - Master advanced search techniques
- **JID Format Guide** - Understanding WhatsApp identifiers
- **Search Patterns** - Wildcards and pattern matching

AI assistants can access these guides through the MCP Resources API.

### Environment Variables

See `.env.example` and be happy!

## 🔔 Webhook Events

When `WEBHOOK_URL` is set, the server POSTs a JSON payload to that URL for every incoming and outgoing message.

### Payload Structure

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "event_type": "message.received",
  "timestamp": "2026-06-14T10:00:00Z",
  "data": {
    "message_id": "3EB0...",
    "chat_jid": "6281234567890@s.whatsapp.net",
    "sender_jid": "6281234567890@s.whatsapp.net",
    "text": "Hello!",
    "timestamp": "2026-06-14T10:00:00Z",
    "is_from_me": false,
    "message_type": "text",
    "chat_name": "John Doe",
    "sender_push_name": "John",
    "sender_contact_name": "John Doe",
    "is_group": false,
    "media_metadata": null,
    "referral": null
  }
}
```

### Fields

| Field | Type | Description |
|---|---|---|
| `id` | string (UUID) | Unique event identifier |
| `event_type` | string | `message.received` or `message.sent` |
| `timestamp` | string (RFC3339) | When the event was generated |
| `data.message_id` | string | WhatsApp message ID |
| `data.chat_jid` | string | JID of the chat (DM or group) |
| `data.sender_jid` | string | JID of the sender |
| `data.text` | string | Message text content |
| `data.timestamp` | string (RFC3339) | When the message was sent |
| `data.is_from_me` | bool | `true` if sent from your account |
| `data.message_type` | string | `text`, `image`, `video`, `audio`, `document`, `sticker`, `ptt`, `gif` |
| `data.chat_name` | string | Display name of the chat (omitted if empty) |
| `data.sender_push_name` | string | WhatsApp display name of the sender (omitted if empty) |
| `data.sender_contact_name` | string | Local contact name for the sender (omitted if empty) |
| `data.is_group` | bool | `true` if the message is in a group chat |
| `data.media_metadata` | object \| null | Present when message has a media attachment (see below) |
| `data.referral` | object \| null | Present when message originated from a Meta Click-to-WhatsApp ad (see below) |

### Media Metadata

Present when `message_type` is `image`, `video`, `audio`, `document`, `sticker`, `ptt`, or `gif`.

```json
"media_metadata": {
  "message_id": "3EB0...",
  "file_name": "photo.jpg",
  "file_size": 204800,
  "mime_type": "image/jpeg",
  "has_media": true
}
```

### Referral (Click-to-WhatsApp Ads)

When a user taps a Meta ad with a "Message on WhatsApp" button, their first message carries ad attribution metadata (`ExternalAdReply` in the WhatsApp protocol). The server extracts this and populates `referral`:

```json
"referral": {
  "ctwa_clid": "ARAkLkA8...",
  "source_id": "120208468219880053",
  "source_type": "AD",
  "source_url": "https://fb.com/ads/...",
  "headline": "Order Now"
}
```

| Field | Description |
|---|---|
| `ctwa_clid` | Meta's click ID — use this for offline conversion attribution via Meta Conversions API |
| `source_id` | The ad ID that originated the conversation |
| `source_type` | Ad placement type (e.g. `AD`) |
| `source_url` | Destination URL of the ad |
| `headline` | Ad creative headline text |

`referral` is `null` for all non-ad messages. It is supported on text, image, and video messages (the message types where WhatsApp carries `ExternalAdReply`).

### Delivery & Retries

Failed deliveries are retried up to `WEBHOOK_MAX_RETRIES` times with exponential backoff. Delivery attempts are logged in the database and visible via the webhook management API (`GET /webhooks/deliveries`).

## 🤝 Contributing

This is a personal project I maintain for daily use. Contributions are welcome!

See [CONTRIBUTING.md](CONTRIBUTING.md) for:
- Development setup and workflow
- Project structure (main server vs migration CLI)
- Database migration system
- Code style guidelines

Quick start:
1. Fork the repository
2. Create your feature branch
3. Follow the guidelines in CONTRIBUTING.md
4. Submit a pull request

## ⚠️ Disclaimer

This project is **not affiliated with WhatsApp or Meta**. It uses the unofficial WhatsApp Web API through the whatsmeow library. Use at your own risk.

**Important Notes:**
- WhatsApp may change their API at any time
- Using unofficial APIs may violate WhatsApp's Terms of Service
- This is provided as-is with no warranties
- Keep your session data secure

---

<div align="center">

**Built with ❤️ for the MCP community**

[Report Bug](https://github.com/felipeadeildo/whatsapp-mcp/issues) • [Request Feature](https://github.com/felipeadeildo/whatsapp-mcp/issues)

</div>
