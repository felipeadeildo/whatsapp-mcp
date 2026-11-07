# Costs

One personal account running all day costs $5 per month, the price of Workers Paid. The usage included in that plan covers everything one account does:

|                | One account, per month                              | Workers Paid includes |
| -------------- | --------------------------------------------------- | --------------------- |
| Time in memory | 324,000 GB-s                                        | 400,000 GB-s          |
| Requests       | 90,000, plus your assistant's calls                 | 1 million             |
| Written rows   | 260,000 once to link, then 90,000 plus new messages | 50 million            |

The object stays in memory all day, and Cloudflare bills that time as if it used the full 128 MB, however quiet the account is. It also wakes every 30 seconds, which is where the 90,000 requests and rows come from. Linking saves the whole history at once: an account with about 30,000 messages wrote 258,000 rows in its first hour. To go past the included usage, you'd need about 30,000 assistant calls or 100,000 new messages a day.

More accounts share the same plan. Cloudflare bills extra time in blocks of 1 million GB-s at $12.50, so two to four accounts cost $17.50 per month.

Workers AI bills transcription per audio minute: $0.0005 with Whisper and $0.0052 with Nova-3. A thousand 30-second voice messages cost about $0.26 with Whisper, and the 10,000 free neurons a day cover about 200 minutes of it. An image description costs about $0.0004 with Mistral Small 3.1.

Workers Free isn't enough, even for one account. It allows 100,000 written rows a day, fewer than the first sync writes. When an account crosses that limit, Cloudflare blocks every write until 00:00 UTC, so the server can't save messages or schedule its heartbeat.

For current prices, see [Workers pricing](https://developers.cloudflare.com/workers/platform/pricing/), [Durable Objects pricing](https://developers.cloudflare.com/durable-objects/platform/pricing/) and [limits](https://developers.cloudflare.com/durable-objects/platform/limits/).
