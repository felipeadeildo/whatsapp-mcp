# WhatsApp MCP

WhatsApp MCP is a self-hosted service that allows you to connect your WhatsApp account and expose it as an Model Context Protocol (MCP) server to be consumed by AI services as a tool.

> WARNING: This project uses a unofficial WhatsApp API and is not endorsed by WhatsApp.

### Project Workflow

The main idea is to use the [unofficial WhatsApp API](https://github.com/asternic/wuzapi) to give us the capabilities to interact with Whatsapp methods as send message, download content and much more.

Unfortunately, this API don't give us retrieve endpoints to get message history, so the Python layer will connect to the internal database and get the messages to expose an API compatible with MCP to give as context to the AI.
