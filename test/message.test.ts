import { describe, expect, test } from "bun:test"
import type { WAMessage } from "@oxidezap/baileyrs/lib/Types/Message.js"

import { attachedReactions, interpret, interpretUpdate } from "../src/whatsapp/message"

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

function stored(message: WAMessage["message"], key: Partial<WAMessage["key"]> = {}) {
  const event = interpret(raw(message, key))
  if (event.type !== "message") throw new Error(`expected a message, got ${event.type}`)
  return event.message
}

describe("interpret", () => {
  test("a plain text message", () => {
    expect(stored({ conversation: "oi" })).toEqual({
      remoteJid: CHAT,
      participant: null,
      id: "ID1",
      fromMe: false,
      pushName: "Maria",
      sentAt: 1_791_000_000,
      kind: "text",
      text: "oi",
      quotedId: null,
      mentions: [],
      forwarded: false,
      media: null,
    })
  })

  test("a group reply keeps the participant, the quoted id, mentions and the forwarded flag", () => {
    const message = stored(
      {
        extendedTextMessage: {
          text: "concordo @5511777777777",
          contextInfo: {
            stanzaId: "Q1",
            mentionedJid: ["5511777777777@s.whatsapp.net"],
            isForwarded: true,
          },
        },
      },
      { remoteJid: GROUP, participant: "5511888888888@s.whatsapp.net" },
    )
    expect(message).toMatchObject({
      remoteJid: GROUP,
      participant: "5511888888888@s.whatsapp.net",
      text: "concordo @5511777777777",
      quotedId: "Q1",
      mentions: ["5511777777777@s.whatsapp.net"],
      forwarded: true,
    })
  })

  test("a group message from history takes its author from the message, not the key", () => {
    const event = interpret({
      ...raw({ conversation: "oi" }, { remoteJid: GROUP }),
      participant: "999@lid",
    })
    expect(event.type === "message" && event.message.participant).toBe("999@lid")
  })

  test("a comment on a channel post reads as the message inside it", () => {
    expect(stored({ commentMessage: { message: { conversation: "boa!" } } })).toMatchObject({
      text: "boa!",
    })
  })

  test("content with nothing anyone wrote is ignored", () => {
    expect(interpret(raw({ messageContextInfo: {} }))).toEqual({ type: "ignored" })
  })

  test("an image inside a disappearing-message wrapper keeps its caption and media", () => {
    const message = stored({
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
    })
    expect(message).toMatchObject({
      kind: "image",
      text: "olha",
      media: { mimetype: "image/jpeg", bytes: 2048, width: 10, height: 20 },
    })
  })

  test("a voice note is told apart from an audio file, and a video note from a video", () => {
    expect(stored({ audioMessage: { ptt: true, seconds: 7 } })).toMatchObject({
      kind: "voice",
      media: { seconds: 7 },
    })
    expect(stored({ ptvMessage: { seconds: 3 } })).toMatchObject({ kind: "video_note" })
  })

  test("a business template reads as its title, body, footer and buttons", () => {
    const message = stored({
      templateMessage: {
        hydratedTemplate: {
          hydratedTitleText: "Pedido 42",
          hydratedContentText: "Seu pedido saiu para entrega",
          hydratedFooterText: "Loja X",
          hydratedButtons: [
            { quickReplyButton: { displayText: "Rastrear" } },
            { urlButton: { displayText: "Site", url: "https://x.example" } },
          ],
        },
      },
    })
    expect(message.kind).toBe("text")
    expect(message.text).toBe(
      "Pedido 42\n\nSeu pedido saiu para entrega\n\nLoja X\n\n[Rastrear] [Site (https://x.example)]",
    )
  })

  test("a list and the reply to it read as the options and the choice", () => {
    expect(
      stored({
        listMessage: {
          title: "Cardápio",
          buttonText: "Ver opções",
          sections: [{ rows: [{ title: "Pizza", description: "grande" }, { title: "Suco" }] }],
        },
      }).text,
    ).toBe("Cardápio\n\nVer opções:\n- Pizza: grande\n- Suco")
    expect(stored({ buttonsResponseMessage: { selectedDisplayText: "Sim" } }).text).toBe("Sim")
  })

  test("a poll lists its options", () => {
    expect(
      stored({
        pollCreationMessageV3: {
          name: "Almoço?",
          options: [{ optionName: "sim" }, { optionName: "não" }],
        },
      }),
    ).toMatchObject({ kind: "poll", text: "Almoço?\n\n- sim\n- não" })
  })

  test("unknown content keeps its type name", () => {
    expect(stored({ requestPaymentMessage: {} })).toMatchObject({
      kind: "other",
      text: "requestPaymentMessage",
    })
  })

  test("a reaction points at its target", () => {
    expect(
      interpret(
        raw({ reactionMessage: { key: { id: "TARGET" }, text: "❤️", senderTimestampMs: 5_000 } }),
      ),
    ).toEqual({
      type: "reaction",
      remoteJid: CHAT,
      id: "TARGET",
      senderJid: CHAT,
      emoji: "❤️",
      at: 5_000,
    })
  })

  test("a revoke and an edit are changes to another message", () => {
    expect(interpret(raw({ protocolMessage: { type: 0, key: { id: "OLD" } } }))).toEqual({
      type: "revoke",
      remoteJid: CHAT,
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
      ),
    ).toEqual({ type: "edit", remoteJid: CHAT, id: "OLD", text: "novo" })
  })

  test("poll votes and messages without content are ignored", () => {
    expect(interpret(raw({ pollUpdateMessage: {} }))).toEqual({ type: "ignored" })
    expect(interpret(raw(null))).toEqual({ type: "ignored" })
  })
})

describe("attachedReactions", () => {
  test("history messages carry their reactions along", () => {
    const message = {
      ...raw({ conversation: "foto linda" }),
      reactions: [
        { key: { remoteJid: CHAT, fromMe: false }, text: "😍", senderTimestampMs: 9_000 },
      ],
    }
    expect(attachedReactions(message)).toEqual([
      { type: "reaction", remoteJid: CHAT, id: "ID1", senderJid: CHAT, emoji: "😍", at: 9_000 },
    ])
  })
})

describe("interpretUpdate", () => {
  test("a stub revoke becomes a revoke", () => {
    expect(
      interpretUpdate({ key: { remoteJid: CHAT, id: "OLD" }, update: { messageStubType: 1 } }),
    ).toEqual({
      type: "revoke",
      remoteJid: CHAT,
      id: "OLD",
    })
  })

  test("a status update is ignored", () => {
    expect(interpretUpdate({ key: { remoteJid: CHAT, id: "OLD" }, update: { status: 3 } })).toEqual(
      {
        type: "ignored",
      },
    )
  })
})
