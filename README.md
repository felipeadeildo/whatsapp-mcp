<div align="center">

# whatsapp-mcp

**Give your AI agents your WhatsApp.**

[![MCP server](https://img.shields.io/badge/MCP-server-3cc488?style=flat-square)](https://modelcontextprotocol.io) [![Runs on Cloudflare Workers](https://img.shields.io/badge/runs_on-Cloudflare_Workers-f38020?style=flat-square&logo=cloudflare&logoColor=white)](https://developers.cloudflare.com/durable-objects/) [![License: GPL-3.0](https://img.shields.io/badge/license-GPL--3.0-8ba69d?style=flat-square)](LICENSE)

[![Deploy to Cloudflare](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https://github.com/felipeadeildo/whatsapp-mcp)

<br>

<img src="docs/pairing.png" alt="Pairing page with a QR code inside a camera viewfinder and the three steps to link a device" width="820">

</div>

whatsapp-mcp lets an AI assistant like Claude read your WhatsApp chats, search them and send messages for you. It connects to your account the same way [WhatsApp Web](https://faq.whatsapp.com/1317564962315842) does, as a linked device, and keeps a searchable copy of your chats.

It speaks [MCP](https://modelcontextprotocol.io/introduction), the standard way AI apps connect to tools, so it works with Claude Code, Cursor and other MCP clients. It runs on your own [Cloudflare](https://www.cloudflare.com) account, so your messages stay with you.

Once it is set up, you can ask things like:

- "What did Marina say about the trip?"
- "Which chats have unread messages since yesterday?"
- "Reply to the family group saying I'll be late."

## Get started

You need a [Cloudflare account](https://dash.cloudflare.com/sign-up), a [GitHub account](https://github.com/signup) and your phone with WhatsApp.

### 1. Deploy

Click **Deploy to Cloudflare** above. Cloudflare copies this project to your GitHub, builds it and puts it online at an address like `https://whatsapp-mcp.<your-name>.workers.dev`.

During the setup it asks for an `API_KEY`. That is a password you make up for your server. Generate a strong one in a terminal with `openssl rand -hex 32`, or use a password manager, and keep it safe. Anyone who has it can read and send your messages.

### 2. Link your WhatsApp

Open this address in your browser, with your API key after the `#`:

```
https://whatsapp-mcp.<your-name>.workers.dev/accounts/personal#<API_KEY>
```

Select **Link WhatsApp**. On your phone, open WhatsApp, go to **Settings → Linked devices → Link a device** and point the camera at the code. The page then counts your chats as they arrive.

`personal` is a name you choose. Each name is a separate WhatsApp account, so one server can hold several.

<div align="center">
<img src="docs/linked.png" alt="Pairing page after linking, with the archive size and the MCP endpoint ready to copy" width="820">
</div>

### 3. Connect your AI assistant

The pairing page shows the command for [Claude Code](https://docs.anthropic.com/en/docs/claude-code/mcp) with a copy button:

```sh
claude mcp add --transport http whatsapp \
  https://whatsapp-mcp.<your-name>.workers.dev/accounts/personal/mcp \
  --header "Authorization: Bearer <API_KEY>"
```

Other MCP clients need the same address and an `Authorization: Bearer <API_KEY>` header. See your client's documentation for where to put them.

## What your assistant can do

| Tool                  | What it does                                                    |
| --------------------- | --------------------------------------------------------------- |
| `get_status`          | Connection state and how much of your history is saved          |
| `list_chats`          | Chats in the order your phone shows them, with unread counts    |
| `find_chats`          | Chats by any name or phone number                               |
| `find_people`         | People by any name or phone, including group members            |
| `get_messages`        | A chat's messages, with paging and date or sender filters       |
| `get_message_context` | A message and the conversation around it                        |
| `search_messages`     | Search all chats by words, ignoring accents and capitals        |
| `send_message`        | Sends text, as a reply or with mentions, never twice on retries |
| `react_to_message`    | Adds or removes a reaction                                      |
| `edit_message`        | Edits a message you sent                                        |
| `delete_message`      | Deletes a message for everyone                                  |
| `mark_as_read`        | Marks a chat as read                                            |
| `save_contact`        | Saves or renames someone in your address book, on every device  |
| `remove_contact`      | Removes someone from your address book, on every device         |
| `load_older_messages` | Asks your phone for older history                               |
| `check_numbers`       | Which phone numbers have WhatsApp                               |
| `get_group_info`      | A group's details and members                                   |
| `get_profile`         | Someone's name, About text and picture                          |

Tools that send or change something tell your assistant so, and most assistants ask you before using them.

## How it works

```mermaid
flowchart LR
  agent["AI assistant"] -- "MCP + API key" --> worker["Worker"]
  worker -- "RPC" --> account
  subgraph account["Durable Object, one per WhatsApp account"]
    socket["WhatsApp session<br/>whatsapp-rust in WASM"]
    archive[("SQLite archive<br/>full-text search")]
    socket --> archive
  end
  socket <-- "WebSocket" --> whatsapp["WhatsApp"]
```

Each WhatsApp account lives in its own [Durable Object](https://developers.cloudflare.com/durable-objects/), a small server that Cloudflare keeps running with its own SQLite database. The WhatsApp session inside it is [baileyrs](https://github.com/oxidezap/baileyrs), whose protocol core is [whatsapp-rust](https://github.com/oxidezap/whatsapp-rust) compiled to WebAssembly. An alarm every 30 seconds keeps the object in memory and reconnects it after deploys.

WhatsApp sends your recent history once, when you link, and the archive keeps every message after that. Search finds people by any name they have used, and treats a person's phone number and their anonymous WhatsApp ID (LID) as the same person.

## Configuration

| Name       | Kind     | Purpose                                                       |
| ---------- | -------- | ------------------------------------------------------------- |
| `API_KEY`  | Secret   | The password every request needs                              |
| `TIMEZONE` | Variable | Time zone for dates, like `America/Sao_Paulo`. `UTC` if unset |

Change `TIMEZONE` in `wrangler.jsonc`, or pass `--var TIMEZONE:<zone>` to `wrangler deploy`. The pairing page also uses a small REST API under `/accounts/:id`: `status`, `start`, `send` and `logout`.

## Costs

**One personal account running 24/7 costs $5 per month**, the price of Workers Paid. Its included usage covers everything one account does:

|                | One account, per month                              | Workers Paid includes |
| -------------- | --------------------------------------------------- | --------------------- |
| Time in memory | 324,000 GB-s                                        | 400,000 GB-s          |
| Requests       | 90,000, plus your assistant's calls                 | 1 million             |
| Written rows   | 260,000 once to link, then 90,000 plus new messages | 50 million            |

The object stays in memory all day, and Cloudflare bills that time as if it used the full 128 MB, however busy the account is. It also wakes every 30 seconds, which explains the 90,000 requests and rows. Linking saves the whole history at once: an account with about 30,000 messages wrote 258,000 rows in its first hour. Going past the included usage would take about 30,000 assistant calls or 100,000 new messages a day.

More accounts share the same plan. Cloudflare bills extra time in blocks of 1 million GB-s at $12.50, so two to four accounts cost $17.50 per month.

Workers Free is not enough, even for one account.

> [!WARNING]
> Workers Free allows 100,000 written rows per day, fewer than the first sync writes. When an account crosses that limit, Cloudflare blocks every write until 00:00 UTC, so the server can't save new messages or schedule its heartbeat. Upgrading to Workers Paid lifts the block.

See [Workers pricing](https://developers.cloudflare.com/workers/platform/pricing/), [Durable Objects pricing](https://developers.cloudflare.com/durable-objects/platform/pricing/) and [limits](https://developers.cloudflare.com/durable-objects/platform/limits/) for current numbers.

## Is it safe?

Your session and your messages stay in your Cloudflare account. The server talks only to WhatsApp, and only someone with your API key can use it.

This is an unofficial WhatsApp client, not affiliated with WhatsApp or Meta, and WhatsApp can ban accounts that use one. Link a number you can afford to lose, and keep your assistant asking before it sends messages.

## Development

You need [Bun](https://bun.sh). [Wrangler](https://developers.cloudflare.com/workers/wrangler/) comes with the project.

```sh
bun install
printf 'API_KEY=%s\nTIMEZONE=UTC\n' "$(openssl rand -hex 32)" > .dev.vars
bun run dev     # http://localhost:8787/accounts/dev#<API_KEY>
bun run check   # types, lint, format and tests
```

To deploy from your machine, run `bunx wrangler secret put API_KEY` once and then `bun run deploy`.

Built on [baileyrs](https://github.com/oxidezap/baileyrs) and [whatsapp-rust](https://github.com/oxidezap/whatsapp-rust). Message rendering and identity rules follow [mautrix-whatsapp](https://github.com/mautrix/whatsapp). Licensed under [GPL-3.0](LICENSE).

<br>

<div align="center">
<a href="https://adeildo.dev"><img src="docs/cat.svg" width="44" alt="A cat asleep on a laptop, the adeildo.dev logo"></a>
<br>
<sub>Brought to you by <a href="https://adeildo.dev">adeildo.dev</a></sub>
</div>
