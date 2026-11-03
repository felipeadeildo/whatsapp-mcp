# 0002. Show images as WhatsApp sent them, and describe them on the first view

## Context

Reads show an image only by its caption and size, so the assistant cannot see it. MCP clients pass image results to the model, except some that drop them, like the Anthropic API's MCP connector. WhatsApp images are usually 40 to 200 KB, well under Claude's 5 MB limit, and Workers AI vision models took files up to 11 MB and 5184 px in our tests without resizing.

## Decision

A `view_media` tool downloads the media of one message when the assistant asks for it, the same way transcription does (ADR 0001), and returns the image as WhatsApp sent it. Nothing resizes or converts it. The first view of an image or image document also asks a Workers AI vision model (Mistral Small 3.1 by default, `DESCRIBE_MODEL`) for a description and the text in the image, and saves it in `messages.description`, which full-text search covers. Videos show the JPEG preview inside the message. Voice messages show their transcript. Stickers are shown but not described, and view-once media is never opened.

## Alternatives rejected

- Resizing with the Images binding: almost no WhatsApp image needs it, and it adds a binding and a billed transformation. An image over 3.75 MB shows the preview inside the message instead.
- Returning images from `get_messages`: each image costs the model 1,000 to 1,600 tokens of context on every read.
- Describing images when a read returns them: it would pay for images nobody opened.
- Never describing: images would not be searchable, and clients that drop image results would show nothing.

## Consequences

- The first view of an image takes 2 to 7 seconds more. After 15 seconds the view returns with `description_status: "pending"`, and the description saves in the background.
- A description costs about $0.0004 per image.
- Asked to follow the language of the text in the image, Mistral Small 3.1 still wrote the description in English. Naming the language works, so `DESCRIBE_LANGUAGE` sets it and descriptions are English without it. It is separate from `TRANSCRIBE_LANGUAGE` because forcing one language on Whisper breaks voice messages in another, while detection got every one right in production.
- If a client needs smaller images, revisit the Images binding.
