import type { ArchivedMessage } from "../src/store/archive"

// A voice message from 1@lid unless overridden.
export function archivedMessage(
  id: string,
  overrides: Partial<ArchivedMessage> = {},
): ArchivedMessage {
  return {
    chatJid: "1@lid",
    chatName: null,
    id,
    fromMe: false,
    senderJid: "1@lid",
    senderName: null,
    sentAt: 1_791_000_000,
    kind: "voice",
    text: null,
    quotedId: null,
    mentions: [],
    forwarded: false,
    media: null,
    transcript: null,
    description: null,
    edited: false,
    deleted: false,
    reactions: [],
    ...overrides,
  }
}
