// WhatsApp turns `@<user>` into a link only when `<user>` is the user part of a
// mentioned JID. In groups addressed by LID that is the LID, which nobody knows, so
// callers write the phone number and it is rewritten here.

export interface MentionTarget {
  /** The JID WhatsApp receives in `mentionedJid`. */
  readonly jid: string
  /** Every JID the person goes by, so `@<phone>` and `@<lid>` in the text both match. */
  readonly known: readonly string[]
}

export function userOf(jid: string): string {
  return jid.slice(0, jid.indexOf("@")).split(":")[0] ?? jid
}

function escape(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
}

/** Rewrites `@<number>`, `@+<number>` and `@<lid>` to `@<user of the target JID>`. */
export function mentionText(text: string, targets: readonly MentionTarget[]): string {
  let out = text
  for (const target of targets) {
    const users = [...new Set(target.known.map(userOf))]
      .filter((user) => user.length > 0)
      .toSorted((a, b) => b.length - a.length)
    if (users.length === 0) continue
    const pattern = new RegExp(`@\\+?(?:${users.map(escape).join("|")})(?!\\d)`, "g")
    out = out.replace(pattern, `@${userOf(target.jid)}`)
  }
  return out
}

/** The JID to mention someone by: their LID in a group addressed by LID, else their phone JID. */
export function mentionJid(
  person: { readonly lid: string | null; readonly pn: string | null; readonly jid: string },
  addressing: "lid" | "pn" | null,
): string {
  if (addressing === "lid") return person.lid ?? person.pn ?? person.jid
  if (addressing === "pn") return person.pn ?? person.jid
  return person.jid
}
