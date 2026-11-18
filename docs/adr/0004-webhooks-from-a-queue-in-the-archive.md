# 0004. Webhooks deliver from a queue in the account's own SQLite

## Context

An assistant only sees WhatsApp when someone asks it something. To answer a message as it arrives, or to copy messages into another system, a program has to hear about them first. MCP has no standard way to push an event to a client that is not connected, so the server POSTs each event to a URL the owner chooses.

WhatsApp sends the same message more than once: on reconnects, in the history sync and when the phone uploads media again. The Durable Object can be evicted or redeployed at any moment, and a receiver can be down for hours.

## Decision

The archive tells new from repeated. `saveMessages` returns only the messages it did not have, and edits, deletions and reactions count only when they change a row. Messages from the history sync never become events, so linking does not send tens of thousands of requests. The events are `message.received`, `message.sent`, `message.edited`, `message.deleted`, `message.reaction` and `account.connection`, which reports links, unlinks and refused numbers but not the reconnects in between.

Each event becomes one row per interested webhook in `webhook_deliveries`, in the same SQLite as the archive and in the same synchronous turn as the archive write. The Durable Object commits both together, so a message is never archived without its deliveries, and an eviction loses nothing: the next alarm picks the queue up.

Delivery is a signed POST in the [Standard Webhooks](https://www.standardwebhooks.com) format, so receivers verify it with an existing library. It is at least once: `webhook-id` stays the same across attempts. Order is not kept, so one failing event cannot hold the rest back. After the first attempt the server retries after 5 s, 5 min, 30 min, 2 h, 5 h, 10 h and 10 h, as Svix does. A `410 Gone` turns the webhook off at once, and so do three days of nothing but failures. The payload is saved with the delivery, so a retry sends what the first attempt sent.

The object has one alarm, until now the 30-second heartbeat. It is now set for whichever comes first, the heartbeat or the next retry, and never pushed later, so steady traffic cannot starve the heartbeat.

The REST API, the pairing page and the MCP tools manage webhooks through the same Durable Object methods and the same zod schema. `WEBHOOK_TOOLS` limits what MCP clients can do: `manage` by default, `read` or `off`.

A webhook can ask for voice messages to be transcribed before delivery. The delivery then waits up to 8 seconds for the transcript, and saves it in the archive, as a read would (ADR 0001). It is off by default, because it pays for audio that the receiver may never use.

Erasing the archive deletes the webhooks too. Any number can link after an erase, and its messages should not go to the previous owner's receivers.

## Alternatives we rejected

We didn't use Cloudflare Queues. They retry and keep dead letters, but they add a binding to every deploy and bill per operation, and the consumer would still have to ask the Durable Object for each webhook's URL and secret. The SQLite queue commits with the archive and adds nothing to the deploy. If an account ever sends thousands of events a minute, Queues are worth another look.

We didn't fetch without a queue. A deploy, an eviction or a receiver that is down would lose the event.

We didn't keep the MCP tools read-only, though a webhook is a standing channel out of the account, and an assistant could be talked into creating one by a message it reads. The owner wanted to manage webhooks from an assistant. The tools carry `destructiveHint` and `openWorldHint` so clients ask first, the server's instructions say to create a webhook only when the user asks in the conversation, each webhook records whether it was created through `api`, `page` or `mcp`, and `WEBHOOK_TOOLS` turns them down to `read` or `off`.

## Consequences

Delivering adds no cost of its own. The heartbeat already keeps the object awake, so the time spent sending is not billed again, and outgoing requests are free on Workers. Each event writes about three rows per webhook.

A receiver that was down for more than about 28 hours misses events. It can read them from the archive with the MCP tools.

A webhook keeps at most about 5,000 pending deliveries. The hourly cleanup drops the oldest beyond that, so a dead endpoint cannot grow the database without limit.

The heartbeat and the retries share one alarm. Any new use of the alarm has to go through `scheduleAlarm`.
