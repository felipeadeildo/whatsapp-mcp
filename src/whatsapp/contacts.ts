// WhatsApp saves contacts by bare phone-number JID and rejects LIDs, while the
// archive keeps people under their LID once the pair is known.
import { jidNormalizedUser } from "@oxidezap/baileyrs/lib/WABinary/jid-utils.js"

import type { Archive } from "../store/archive"
import type { Identity } from "../store/names"
import type { Socket } from "./socket"

const PN_SUFFIX = "@s.whatsapp.net"
const LID_SUFFIX = "@lid"

export type AddressBook = Pick<Socket, "addOrEditContact" | "removeContact">

export interface ContactName {
  readonly fullName: string
  readonly firstName?: string | undefined
}

export class NotAPersonError extends Error {
  override readonly name = "NotAPersonError"

  constructor(readonly jid: string) {
    super(`${jid} is not a person; contacts can only be saved for people`)
  }
}

export class PhoneUnknownError extends Error {
  override readonly name = "PhoneUnknownError"

  constructor(readonly jid: string) {
    super(
      `no phone number is known for ${jid}; WhatsApp saves contacts by phone number, so pass the phone with country code instead`,
    )
  }
}

// Takes a JID or a phone number with country code.
export function resolveAddress(archive: Pick<Archive, "canonical">, to: string): string {
  if (to.includes("@")) return archive.canonical(jidNormalizedUser(to))
  const digits = to.replace(/\D/g, "")
  if (!digits) throw new Error(`not a JID or phone number: ${to}`)
  return archive.canonical(`${digits}${PN_SUFFIX}`)
}

export interface ContactTarget {
  readonly jid: string
  readonly pn: string
}

export function contactTarget(
  archive: Pick<Archive, "canonical" | "phoneJidOf">,
  to: string,
): ContactTarget {
  const jid = resolveAddress(archive, to)
  if (!jid.endsWith(LID_SUFFIX) && !jid.endsWith(PN_SUFFIX)) throw new NotAPersonError(jid)
  const pn = archive.phoneJidOf(jid)
  if (!pn) throw new PhoneUnknownError(jid)
  return { jid, pn }
}

// WhatsApp Web always sends a first name, which the phone shows where space is short.
export function firstNameOf(fullName: string): string {
  return fullName.trim().split(/\s+/)[0] ?? fullName
}

export async function saveContact(
  book: AddressBook,
  archive: Archive,
  to: string,
  name: ContactName,
): Promise<Identity> {
  const { jid, pn } = contactTarget(archive, to)
  const fullName = name.fullName.trim()
  const firstName = name.firstName?.trim() || firstNameOf(fullName)
  await book.addOrEditContact(pn, { fullName, firstName, saveOnPrimaryAddressbook: true })
  // Nothing guarantees the sync echoes our own mutation back as `contacts.update`.
  archive.learnNames(jid, [{ source: "contact", name: fullName }])
  return archive.getPerson(jid)
}

export async function removeContact(
  book: AddressBook,
  archive: Archive,
  to: string,
): Promise<Identity> {
  const { jid, pn } = contactTarget(archive, to)
  await book.removeContact(pn)
  // Only the address book name goes; names from other sources take over.
  archive.forgetNames(jid, "contact")
  return archive.getPerson(jid)
}
