# 0005. A REST API for bots, served by the account

## Context

The REST API had `status`, `start`, `send`, `logout`, `erase` and webhooks. A bot that answers in a group needs more: edit the "thinking…" message it sent, list the groups to listen to, map members to people by phone, and know when someone mentioned it or replied to it. The MCP tools do all of this, but their output is text written for a model, and their shape changes when the model's needs do. A program needs stable JSON and errors it can branch on.

WhatsApp names one person by phone JID in some chats and by LID in others. In a group addressed by LID, a mention arrives as `<lid>@lid`, and a mention only turns into a link when the text says `@<lid>`. Nobody outside WhatsApp knows a LID by heart.

## Decision

New resources live under `/accounts/:id`: `me`, `messages`, `chats` and `groups`. The Worker checks the credentials, as for the rest of the API, and passes the request to the account's Durable Object, whose `fetch` routes it (`src/api/rest.ts`). Inside the object, a refusal is a `RequestError` with an HTTP status and a `code`, such as `edit_window_closed`. Across RPC an error keeps only its message, so routing inside the object is what keeps the code.

JSON follows the webhooks: snake_case, nulls kept, ISO 8601 UTC, and a message has the same shape in `GET /chats/:chat/messages` as in a webhook event. Errors are `{ "error", "code" }`, the `error` string the old routes already return plus a code.

Sending resolves mentions. The caller writes `@<number>` and lists the number in `mentions`; the server asks the group for its addressing mode (cached for 10 minutes), mentions the LID in a LID group and the phone JID in a phone group, and rewrites `@<number>` to match. Asking the group also teaches the archive each member's LID and phone pair.

Messages carry `mentions_me` and `reply_to_me`. The server knows the account by its phone JID and its LID, and compares. A reply is to the account when the quoted message is in the archive as sent by it, or else when the quoted author named in the reply is the account. That author is now saved in `messages.quoted_sender`.

Edits are refused after 15 minutes, with `409 edit_window_closed`. WhatsApp accepts a late edit and delivers it to nobody, so without this check the archive and the `message.edited` event would say an edit happened that no one sees.

The old `POST /send` stays, with its camelCase body, for scripts that use it.

## Alternatives we rejected

We didn't keep the router in the Worker and call the object over RPC. Errors would cross RPC as plain messages, and the Worker would match strings to find the status.

We didn't make callers send LIDs. The bot would have to fetch the group and learn the addressing rules, and get mentions that read as a plain number when it guessed wrong.

We didn't change the shape of `reply_to`. It stays the quoted message's ID, and `reply_to_me` sits next to it, so existing receivers keep working.

## Consequences

A message with mentions in a group costs one extra request to WhatsApp every 10 minutes per group.

Replies archived before this change have no `quoted_sender`; `reply_to_me` still works for them when the quoted message is in the archive.

The edit window follows WhatsApp's 15 minutes. If WhatsApp changes it, `EDIT_WINDOW_SECONDS` changes with it.
