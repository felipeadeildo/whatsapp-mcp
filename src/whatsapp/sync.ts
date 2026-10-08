/**
 * Keeps the archive in step with the socket: every event that carries chats,
 * contacts or messages is translated and written. History sync (at pairing and
 * on demand) and live traffic go through the same path.
 */
import type { Chat } from "@oxidezap/baileyrs/lib/Types/Chat.js"
import type { Contact } from "@oxidezap/baileyrs/lib/Types/Contact.js"
import type { WAMessage } from "@oxidezap/baileyrs/lib/Types/Message.js"
import { jidNormalizedUser } from "@oxidezap/baileyrs/lib/WABinary/jid-utils.js"

import type { Archive, ChatInput, ContactInput } from "../store/archive"
import { interpret, interpretUpdate, type MessageEvent, type StoredMessage } from "./message"
import type { Socket } from "./socket"

type ProtoNumber = number | { toNumber(): number } | null | undefined

function toNumber(value: ProtoNumber): number | null {
  if (value === null || value === undefined) return null
  return typeof value === "number" ? value : value.toNumber()
}

/** The chat a message belongs to, without device suffix; `null` for status broadcasts. */
export function chatOf(remoteJid: string | null | undefined): string | null {
  if (!remoteJid || remoteJid === "status@broadcast") return null
  return jidNormalizedUser(remoteJid)
}

function toChat(chat: Partial<Chat>): ChatInput | null {
  const jid = chatOf(chat.id)
  if (!jid) return null
  const timestamp = toNumber(chat.conversationTimestamp) ?? chat.lastMessageRecvTimestamp ?? null
  return {
    jid,
    name: chat.name ?? null,
    unreadCount: chat.unreadCount ?? null,
    lastMessageAt: timestamp,
    archived: chat.archived ?? null,
    pinned: chat.pinned === undefined || chat.pinned === null ? null : chat.pinned > 0,
    mutedUntil: toNumber(chat.muteEndTime),
  }
}

function toContact(contact: Partial<Contact>): ContactInput | null {
  if (!contact.id) return null
  return {
    jid: jidNormalizedUser(contact.id),
    lid: contact.lid ? jidNormalizedUser(contact.lid) : null,
    phone: contact.phoneNumber ? jidNormalizedUser(contact.phoneNumber) : null,
    name: contact.name ?? null,
    pushName: contact.notify ?? null,
    verifiedName: contact.verifiedName ?? null,
  }
}

function present<T>(value: T | null): value is T {
  return value !== null
}

/** Applies a batch of message events, writing new messages in one transaction. */
function applyMessages(archive: Archive, events: readonly MessageEvent[]): StoredMessage[] {
  const messages: StoredMessage[] = []
  for (const event of events) {
    if (event.type === "message") messages.push(event.message)
  }
  archive.saveMessages(messages)
  for (const event of events) {
    if (event.type === "edit") archive.applyEdit(event.chatJid, event.id, event.text)
    if (event.type === "revoke") archive.applyRevoke(event.chatJid, event.id)
    if (event.type === "reaction") {
      archive.saveReaction(event.chatJid, event.id, event.senderJid, event.emoji, event.at)
    }
  }
  return messages
}

function interpretAll(messages: readonly WAMessage[]): MessageEvent[] {
  const events: MessageEvent[] = []
  for (const message of messages) {
    const chatJid = chatOf(message.key.remoteJid)
    if (chatJid) events.push(interpret(message, chatJid))
  }
  return events
}

export interface SyncHooks {
  /** Called after messages are stored, with how they arrived. */
  readonly onMessages?: (messages: readonly StoredMessage[], source: "live" | "history") => void
}

export function syncArchive(socket: Socket, archive: Archive, hooks: SyncHooks = {}): void {
  const { ev } = socket

  ev.on("messaging-history.set", ({ chats, contacts, messages, lidPnMappings }) => {
    archive.saveContacts(contacts.map(toContact).filter(present))
    archive.saveContacts(
      (lidPnMappings ?? []).map((mapping) => ({
        jid: jidNormalizedUser(mapping.pn),
        lid: jidNormalizedUser(mapping.lid),
        phone: null,
        name: null,
        pushName: null,
        verifiedName: null,
      })),
    )
    archive.saveChats(chats.map(toChat).filter(present))
    hooks.onMessages?.(applyMessages(archive, interpretAll(messages)), "history")
  })

  ev.on("messages.upsert", ({ messages }) => {
    hooks.onMessages?.(applyMessages(archive, interpretAll(messages)), "live")
  })

  ev.on("messages.update", (updates) => {
    const events: MessageEvent[] = []
    for (const update of updates) {
      const chatJid = chatOf(update.key.remoteJid)
      if (chatJid) events.push(interpretUpdate(update, chatJid))
    }
    applyMessages(archive, events)
  })

  ev.on("messages.reaction", (reactions) => {
    for (const { key, reaction } of reactions) {
      const chatJid = chatOf(key.remoteJid)
      if (!chatJid || !key.id) continue
      const sender = reaction.key?.fromMe
        ? null
        : (reaction.key?.participant ?? reaction.key?.remoteJid ?? null)
      archive.saveReaction(
        chatJid,
        key.id,
        sender ? jidNormalizedUser(sender) : null,
        reaction.text ?? "",
        toNumber(reaction.senderTimestampMs) ?? Date.now(),
      )
    }
  })

  ev.on("chats.upsert", (chats) => archive.saveChats(chats.map(toChat).filter(present)))
  ev.on("chats.update", (chats) => archive.saveChats(chats.map(toChat).filter(present)))
  ev.on("chats.delete", (jids) => archive.deleteChats(jids.map(chatOf).filter(present)))
  ev.on("contacts.upsert", (contacts) =>
    archive.saveContacts(contacts.map(toContact).filter(present)),
  )
  ev.on("contacts.update", (contacts) =>
    archive.saveContacts(contacts.map(toContact).filter(present)),
  )
  ev.on("lid-mapping.update", ({ pn, lid }) =>
    archive.saveContacts([
      {
        jid: jidNormalizedUser(pn),
        lid: jidNormalizedUser(lid),
        phone: null,
        name: null,
        pushName: null,
        verifiedName: null,
      },
    ]),
  )
  ev.on("groups.upsert", (groups) =>
    archive.saveChats(
      groups.map((group) => toChat({ id: group.id, name: group.subject })).filter(present),
    ),
  )
  ev.on("groups.update", (groups) =>
    archive.saveChats(
      groups.map((group) => toChat({ id: group.id, name: group.subject })).filter(present),
    ),
  )
}
