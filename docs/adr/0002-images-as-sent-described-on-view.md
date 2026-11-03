# 0002. Show images as WhatsApp sent them, and describe them on the first view

## Context

A read showed an image only by its caption and size. When someone forwarded a Pix receipt, the assistant saw `{ "kind": "image", "media": { "bytes": 110626 } }` and nothing about the amount or who was paid.

Sending the picture to the assistant covers most clients. Claude Desktop, claude.ai and Claude Code pass image results to the model. Some clients don't, like the Anthropic API's MCP connector, which replaces the image with a line of text. Those clients need words about the image, and so does search.

Size turned out not to be a problem. The images in the chats we checked were 40 to 320 KB, well under Claude's limit of 5 MB. In our tests, Workers AI vision models also read files up to 11 MB and 5184 px without resizing.

## Decision

We added a `view_media` tool. When the assistant calls it, the server downloads that one message's media, as transcription does (ADR 0001), and returns the image as WhatsApp sent it. Nothing resizes or converts it.

The first view of an image, or of a document that is an image, also asks a Workers AI vision model to describe it and copy its text. For the receipt, that means the bank, the amount, both names and the transaction ID. The model is Mistral Small 3.1 unless `DESCRIBE_MODEL` names another. The server saves the description in `messages.description`, which full-text search covers, so searching for "pix 150" finds the receipt.

Other media gets what it needs and no more. A video shows the JPEG preview inside the message, so the server downloads nothing. A voice message shows its transcript. A sticker shows without a description. The server never opens view-once media.

## Alternatives we rejected

We didn't resize images with the Images binding. Almost no WhatsApp image needs it, and it would add a binding and a billed transformation. An image over 3.75 MB shows the preview inside the message instead.

We didn't return images from `get_messages`. Each image costs the model 1,000 to 1,600 tokens of context, on every read of the chat.

We didn't describe images when a read returns them, because that pays for images nobody opened. Never describing them was the other extreme. Search wouldn't find them, and clients that drop images would show nothing.

## Consequences

The first view of an image took 2 to 8 seconds longer in production. If the description takes more than 15 seconds, the view returns with `description_status: "pending"` and the server saves it in the background. A description costs about $0.0004.

The language took a second try. We asked Mistral Small 3.1 to write in the language of the text in the image, and it still described Portuguese screenshots in English. It follows the prompt when the prompt names the language, so `DESCRIBE_LANGUAGE` names it, and descriptions are in English without it. That variable is separate from `TRANSCRIBE_LANGUAGE`. Whisper detected the language of every voice message in production, and forcing Portuguese on it would break the voice messages in English.

If a client turns out to need smaller images, the Images binding is the place to start.
