# 1. Transcribe voice messages on read, not on arrival

Date: 2026-11-03. Status: accepted.

## Context

WhatsApp transcribes voice messages on the phone and never sends the text to linked devices, so the server has to transcribe them itself. Workers AI bills speech to text per audio minute, and an assistant reads only a small part of the archive. Media links on WhatsApp's servers expire, but the phone can upload a message's media again when it is online.

## Decision

`get_messages` and `get_message_context` transcribe the voice and audio messages they return. Each read transcribes at most 10, newest first, and waits at most 8 seconds. Whatever is still running then finishes in the background. `messages.transcript` keeps the text, and the full-text index covers it, so later reads and `search_messages` find it without another call. An empty string means nothing was said.

The server downloads the audio only to transcribe it and does not keep the file. It skips view-once audio, and revoking a message clears its transcript.

The model is `@cf/openai/whisper-large-v3-turbo` by default, at $0.0005 a minute. `TRANSCRIBE_MODEL` switches to `@cf/deepgram/nova-3` or turns transcription off. A filter drops transcripts that are only a subtitle credit ("Legendas pela comunidade Amara.org"), which Whisper writes over silence.

## Alternatives

- **Transcribe every voice message on arrival.** Pays for audio nobody reads.
- **Download every voice message on arrival and keep it in R2.** Would make old audio independent of the phone, but stores files that are mostly never used.
- **Transcribe only through a separate tool.** Costs nothing until asked, but the assistant has to know to ask, and search never sees voice messages it did not open.

## Consequences

- The first read of a chat with many new voice messages can take up to 8 seconds longer.
- An old voice message whose link expired transcribes only while the phone is online.
- If assistants end up reading most voice messages anyway, transcribing or downloading on arrival becomes worth revisiting.
