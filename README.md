# WhatsApp MCP

WhatsApp MCP is a self-hosted service that allows you to connect your WhatsApp account and expose it as an Model Context Protocol (MCP) server to be consumed by AI services as a tool.

> WARNING: This project uses a unofficial WhatsApp API and is not endorsed by WhatsApp.

### Project Workflow

The main idea is to use the [unofficial WhatsApp API](https://github.com/asternic/wuzapi) to give us the capabilities to interact with Whatsapp methods as send message, download content and much more.

Unfortunately, this API don't give us retrieve endpoints to get message history, so the Python layer will connect to the internal database and get the messages to expose an API compatible with MCP to give as context to the AI.


### Status

Actually there's a problem with retrieving the initial message history, the API don't give us a way to retrieve the message history and dont saves it internally.

There's three options:
1. Just give up on retrieving the message history before the point the device was linked and just consume the webhooks to save with python.
2. Change the wuzapi codebase to retrieve the message history on start (that is so much work and i would have to maintain continuously)
3. Remove the wuzapi and use the whatsmeow library directly (actually that is the best option, but i would have to implement an interface do interact to the session using the library and we lose the multi-session feature)