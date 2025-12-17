# WhatsApp MCP

This is a proof of concept for a WhatsApp MCP with a database for storing messages exposed to the outside world by MCP tools.

# Architecture

The client-whatsapp interface is built on top of [whatsmeow](https://github.com/whatsmeow/whatsmeow). The server is built on top of [go-mcp](https://github.com/whatsapp/go-mcp).

When connected to WhatsApp, an event of HistorySync is sent to the client. This event is where we load a bunch of messages. The next sent Message events is stored as well.

When the client sends a message, this message is stored in the database as well.

So, we exposes the database to the outside world by MCP tools to send / fetch messages based on this message database copy in real-time.

# TODO

- [x] Store to message database
- [x] WhatsApp interface with whatsmeow
- [ ] MCP interface
- [ ] docker setup for fast deployment
- [ ] audio, image, video and contact messages transcription
- [ ] knowledge graph for GraphRAG


# Note

This is just a side project that i wanna mantain for myself using daily. I don't have time answering every DMs.