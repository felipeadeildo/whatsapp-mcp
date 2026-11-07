# 0003. The archive outlives the link

## Context

WhatsApp unlinked every device of one account after an internal error on its side. The server then deleted the archive along with the credentials. Linking again brought back only the recent history that the phone sends, so older messages, transcripts and image descriptions were gone.

Unlinking doesn't mean the owner is done with the server. WhatsApp unlinks devices on its own, and the archive is the one thing a new link can't rebuild.

## Decision

Unlinking now deletes only the credentials. When the same number links again, its history merges into the archive. Saving a message is an upsert on chat and message ID that keeps edits, deletions, transcripts and descriptions, so a second sync fills gaps and changes nothing else.

The first number to link becomes the owner of the archive, saved as `owner` in the object's storage. If another number scans the code, the server unlinks that device again and drops its events before they reach the archive. The pairing page shows both numbers and offers two ways out: link the owner, or erase the archive.

Erasing is the only way to delete the archive. It's a button on the pairing page, behind a confirmation, and `POST /accounts/:id/erase`. The "Unlink" button keeps the archive.

## Alternatives we rejected

We didn't erase the archive when another number links. Scanning the code with the wrong phone would delete everything without asking.

We didn't keep one archive per number in the same account. A second number can use another account ID, which gives it its own Durable Object.

## Consequences

An account linked before this change has no owner yet. The number connected at its next reconnect becomes the owner.
