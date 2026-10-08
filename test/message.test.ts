import { describe, expect, test } from "bun:test"
import type { WAMessage } from "@oxidezap/baileyrs/lib/Types/Message.js"

import { interpret, interpretUpdate } from "../src/whatsapp/message"

const CHAT = "5511999999999@s.whatsapp.net"
const GROUP = "123456789@g.us"

function raw(message: WAMessage["message"], key: Partial<WAMessage["key"]> = {}): WAMessage {
  return {
    key: { remoteJid: CHAT, id: "ID1", fromMe: false, ...key },
    message,
    messageTimestamp: 1_791_000_000,
    pushName: "Maria",
  }
}

describe("interpret", () => {
  test("a plain text message", () => {
    expect(interpret(raw({ conversation: "oi" }), CHAT)).toEqual({
      type: "message",
      message: {
        chatJid: CHAT,
        id: "ID1",
        fromMe: false,
        senderJid: CHAT,
        senderName: "Maria",
        sentAt: 1_791_000_000,
        kind: "text",
        text: "oi",
        quotedId: null,
        media: null,
      },
    })
  })

  test("a group reply keeps the participant as sender and the quoted id", () => {
    const event = interpret(
      raw(
        { extendedTextMessage: { text: "concordo", contextInfo: { stanzaId: "Q1" } } },
        { remoteJid: GROUP, participant: "5511888888888@s.whatsapp.net" },
      ),
      GROUP,
    )
    expect(event.type === "message" && event.message).toMatchObject({
      senderJid: "5511888888888@s.whatsapp.net",
      text: "concordo",
      quotedId: "Q1",
    })
  })

  test("our own message has no sender", () => {
    const event = interpret(raw({ conversation: "eu" }, { fromMe: true }), CHAT)
    expect(event.type === "message" && event.message.senderJid).toBeNull()
  })

  test("an image inside a disappearing-message wrapper keeps its caption and media", () => {
    const event = interpret(
      raw({
        ephemeralMessage: {
          message: {
            imageMessage: {
              caption: "olha",
              mimetype: "image/jpeg",
              fileLength: 2048,
              width: 10,
              height: 20,
            },
          },
        },
      }),
      CHAT,
    )
    expect(event.type === "message" && event.message).toMatchObject({
      kind: "image",
      text: "olha",
      media: { mimetype: "image/jpeg", bytes: 2048, width: 10, height: 20 },
    })
  })

  test("a voice note is told apart from an audio file", () => {
    const event = interpret(raw({ audioMessage: { ptt: true, seconds: 7 } }), CHAT)
    expect(event.type === "message" && event.message).toMatchObject({
      kind: "voice",
      media: { seconds: 7 },
    })
  })

  test("a reaction points at its target", () => {
    const event = interpret(
      raw({ reactionMessage: { key: { id: "TARGET" }, text: "❤️", senderTimestampMs: 5_000 } }),
      CHAT,
    )
    expect(event).toEqual({
      type: "reaction",
      chatJid: CHAT,
      id: "TARGET",
      senderJid: CHAT,
      emoji: "❤️",
      at: 5_000,
    })
  })

  test("a revoke and an edit are changes to another message", () => {
    expect(interpret(raw({ protocolMessage: { type: 0, key: { id: "OLD" } } }), CHAT)).toEqual({
      type: "revoke",
      chatJid: CHAT,
      id: "OLD",
    })
    expect(
      interpret(
        raw({
          protocolMessage: {
            type: 14,
            key: { id: "OLD" },
            editedMessage: { conversation: "novo" },
          },
        }),
        CHAT,
      ),
    ).toEqual({ type: "edit", chatJid: CHAT, id: "OLD", text: "novo" })
  })

  test("a message without content is ignored", () => {
    expect(interpret(raw(null), CHAT)).toEqual({ type: "ignored" })
  })
})

describe("interpretUpdate", () => {
  test("a stub revoke becomes a revoke", () => {
    expect(interpretUpdate({ key: { id: "OLD" }, update: { messageStubType: 1 } }, CHAT)).toEqual({
      type: "revoke",
      chatJid: CHAT,
      id: "OLD",
    })
  })

  test("a status update is ignored", () => {
    expect(interpretUpdate({ key: { id: "OLD" }, update: { status: 3 } }, CHAT)).toEqual({
      type: "ignored",
    })
  })
})
