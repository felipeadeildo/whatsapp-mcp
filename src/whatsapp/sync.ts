/**
 * Keeps the archive in step with the socket. History sync (at pairing and on
 * demand) and live traffic go through the same path: learn who people are
 * first (contacts, phone/LID pairs, the alternate JIDs every message key
 * carries), then store chats and messages under their canonical JIDs.
 */
import type { Chat } from "@oxidezap/baileyrs/lib/Types/Chat.js"
import type { Contact } from "@oxidezap/baileyrs/lib/Types/Contact.js"
import type { WAMessage, WAMessageKey } from "@oxidezap/baileyrs/lib/Types/Message.js"
import { jidNormalizedUser } from "@oxidezap/baileyrs/lib/WABinary/jid-utils.js"

import {
  type Archive,
  type ChatInput,
  isRedactedPhone,
  type MessageInput,
  type NameInput,
} from "../store/archive"
import type { NameSource } from "../store/names"
import {
  attachedReactions,
  interpret,
  interpretUpdate,
  type MessageEvent,
  reactionEvent,
  type StoredMessage,
  toNumber,
} from "./message"
import { encodeRaw } from "./raw"
import type { Socket } from "./socket"

const PERSON_SERVERS = ["@s.whatsapp.net", "@lid", "@hosted", "@hosted.lid", "@bot"]

function isPerson(jid: string): boolean {
  return PERSON_SERVERS.some((server) => jid.endsWith(server))
}

/** A JID without device suffix; `null` for empty values and the status feed. */
function normalized(jid: string | null | undefined): string | null {
  if (!jid || jid === "status@broadcast") return null
  return jidNormalizedUser(jid)
}

/** Translates socket events into archive writes. */
class Ingest {
  constructor(private readonly archive: Archive) {}

  /** The canonical JID for a chat or person, or `null` for what is not archived. */
  canonical(jid: string | null | undefined): string | null {
    const id = normalized(jid)
    return id ? this.archive.canonical(id) : null
  }

  /** A JID and its alternate (one phone, one LID) name the same person. */
  pair(jid: string | null | undefined, alt: string | null | undefined): void {
    const a = normalized(jid)
    const b = normalized(alt)
    if (!a || !b || a === b) return
    const lid = [a, b].find((value) => value.endsWith("@lid"))
    const pn = [a, b].find((value) => value.endsWith("@s.whatsapp.net"))
    if (lid && pn) this.archive.learnPair(pn, lid)
  }

  /**
   * Records a contact event. `origin` decides what its `name` means: live
   * contact events carry the address book name, while history fills `name`
   * with whatever the conversation was called (`displayName || name ||
   * username` in baileyrs), which can be a masked phone or the username.
   */
  contact(contact: Partial<Contact>, origin: "live" | "history"): void {
    const jid = normalized(contact.id)
    if (!jid) return
    if (!isPerson(jid)) {
      if (contact.name) this.archive.learnNames(jid, [{ source: "subject", name: contact.name }])
      return
    }
    this.pair(jid, contact.lid)
    this.pair(jid, contact.phoneNumber)
    const names: NameInput[] = []
    const { name, username } = contact
    if (name && name !== username) {
      let source: NameSource = origin === "live" ? "contact" : "history"
      if (isRedactedPhone(name)) source = "redacted"
      names.push({ source, name })
    }
    if (username) names.push({ source: "username", name: username })
    if (contact.notify) names.push({ source: "push", name: contact.notify })
    if (contact.verifiedName) names.push({ source: "business", name: contact.verifiedName })
    this.archive.learnNames(jid, names)
  }

  /** What a chat event says about the chat; group and channel names are kept as subjects. */
  chat(chat: Partial<Chat>): ChatInput | null {
    const jid = this.canonical(chat.id)
    if (!jid) return null
    if (chat.name && !isPerson(jid))
      this.archive.learnNames(jid, [{ source: "subject", name: chat.name }])
    return {
      jid,
      unreadCount: chat.unreadCount ?? null,
      lastMessageAt: toNumber(chat.conversationTimestamp) ?? chat.lastMessageRecvTimestamp ?? null,
      archived: chat.archived ?? null,
      pinned: chat.pinned === undefined || chat.pinned === null ? null : chat.pinned > 0,
      mutedUntil: toNumber(chat.muteEndTime),
    }
  }

  /** Learns the identities a message key reveals before anything is stored under them. */
  learnFromKey(
    key: WAMessageKey,
    pushName: string | null | undefined,
    participant: string | null | undefined = key.participant,
    seenAt?: number,
  ): void {
    this.pair(key.remoteJid, key.remoteJidAlt)
    this.pair(participant, key.participantAlt)
    const sender = key.fromMe ? null : normalized(participant ?? key.remoteJid)
    if (sender && pushName && isPerson(sender)) {
      this.archive.learnNames(sender, [{ source: "push", name: pushName, seenAt }])
    }
  }

  /** Stores messages (new or redelivered) and the changes some of them carry. */
  messages(messages: readonly WAMessage[]): MessageInput[] {
    const inputs: MessageInput[] = []
    const changes: MessageEvent[] = []
    for (const raw of messages) {
      // History puts a group message's author on the message, not on its key.
      // The message's own time dates the name, so an old history message does
      // not outrank the name someone uses today.
      this.learnFromKey(
        raw.key,
        raw.pushName,
        raw.key.participant ?? raw.participant,
        toNumber(raw.messageTimestamp) ?? undefined,
      )
      const event = interpret(raw)
      if (event.type === "message") {
        const input = this.toInput(event.message, raw)
        if (input) inputs.push(input)
      } else {
        changes.push(event)
      }
      changes.push(...attachedReactions(raw))
    }
    this.archive.saveMessages(inputs)
    this.applyChanges(changes)
    return inputs
  }

  private toInput(message: StoredMessage, raw: WAMessage): MessageInput | null {
    const chatJid = this.canonical(message.remoteJid)
    if (!chatJid) return null
    return {
      chatJid,
      id: message.id,
      fromMe: message.fromMe,
      senderJid: message.fromMe ? null : this.canonical(message.participant ?? message.remoteJid),
      sentAt: message.sentAt,
      kind: message.kind,
      text: message.text,
      quotedId: message.quotedId,
      mentions: message.mentions,
      forwarded: message.forwarded,
      media: message.media,
      raw: encodeRaw(raw),
    }
  }

  /** Applies edits, revokes and reactions to messages already stored. */
  applyChanges(events: readonly MessageEvent[]): void {
    for (const event of events) {
      if (event.type === "message" || event.type === "ignored") continue
      const chatJid = this.canonical(event.remoteJid)
      if (!chatJid) continue
      if (event.type === "edit") this.archive.applyEdit(chatJid, event.id, event.text)
      if (event.type === "revoke") this.archive.applyRevoke(chatJid, event.id)
      if (event.type === "reaction") {
        const sender = event.senderJid === null ? null : this.canonical(event.senderJid)
        this.archive.saveReaction(chatJid, event.id, sender, event.emoji, event.at)
      }
    }
  }
}

export interface SyncHooks {
  /** Called after messages are stored, with how they arrived. */
  readonly onMessages?: (messages: readonly MessageInput[], source: "live" | "history") => void
}

export function syncArchive(socket: Socket, archive: Archive, hooks: SyncHooks = {}): void {
  const { ev } = socket
  const ingest = new Ingest(archive)
  const chats = (list: readonly Partial<Chat>[]) =>
    archive.saveChats(list.map((chat) => ingest.chat(chat)).filter((chat) => chat !== null))

  ev.on("messaging-history.set", ({ chats: historyChats, contacts, messages, lidPnMappings }) => {
    for (const { lid, pn } of lidPnMappings ?? []) ingest.pair(lid, pn)
    for (const contact of contacts) ingest.contact(contact, "history")
    chats(historyChats)
    hooks.onMessages?.(ingest.messages(messages), "history")
  })

  ev.on("messages.upsert", ({ messages }) => {
    hooks.onMessages?.(ingest.messages(messages), "live")
  })

  ev.on("messages.update", (updates) => {
    for (const update of updates) ingest.learnFromKey(update.key, null)
    ingest.applyChanges(updates.map(interpretUpdate))
  })

  ev.on("messages.reaction", (reactions) => {
    ingest.applyChanges(
      reactions.map(({ key, reaction }) => reactionEvent(key, reaction, Date.now())),
    )
  })

  ev.on("chats.upsert", chats)
  ev.on("chats.update", chats)
  ev.on("chats.delete", (jids) =>
    archive.deleteChats(jids.map((jid) => ingest.canonical(jid)).filter((jid) => jid !== null)),
  )
  ev.on("contacts.upsert", (contacts) =>
    contacts.forEach((contact) => ingest.contact(contact, "live")),
  )
  ev.on("contacts.update", (contacts) =>
    contacts.forEach((contact) => ingest.contact(contact, "live")),
  )
  ev.on("lid-mapping.update", ({ lid, pn }) => ingest.pair(lid, pn))

  const groups = (list: readonly { id?: string | null; subject?: string | null }[]) =>
    chats(list.map((group) => ({ id: group.id ?? undefined, name: group.subject ?? undefined })))
  ev.on("groups.upsert", (list) => {
    for (const group of list)
      group.participants.forEach((participant) => ingest.contact(participant, "live"))
    groups(list)
  })
  ev.on("groups.update", groups)

  ev.on("connection.update", ({ connection }) => {
    const me = socket.user
    if (connection !== "open" || !me) return
    const jid = jidNormalizedUser(me.id)
    ingest.pair(jid, me.lid)
    if (me.name) archive.learnNames(jid, [{ source: "push", name: me.name }])
  })
}

/**
 * History carries no group subjects. The bridge lists every group with its
 * subject in a single request; Baileys' `groupFetchAllParticipating` instead
 * fetches full metadata group by group, which WhatsApp rate-limits (429) for
 * accounts in many groups, so the light listing is used here.
 */
export async function refreshGroupNames(socket: Socket, archive: Archive): Promise<number> {
  const client = socket.waClient
  if (!client) return 0
  const groups = Object.values(await client.groupFetchAllParticipating())
  for (const group of groups) {
    const jid = jidNormalizedUser(group.id)
    archive.saveChats([{ jid }])
    if (group.subject) archive.learnNames(jid, [{ source: "subject", name: group.subject }])
  }
  return groups.length
}
