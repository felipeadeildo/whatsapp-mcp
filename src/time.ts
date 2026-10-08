/**
 * Converts between the archive's Unix seconds and wall-clock time in the
 * account's configured time zone, which is how people (and the models acting
 * for them) read and write dates.
 */

/** Offset of `timeZone` from UTC at `instantMs`, in milliseconds. */
function offsetAt(instantMs: number, timeZone: string): number {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone,
    hourCycle: "h23",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).formatToParts(new Date(instantMs))
  const part = (type: Intl.DateTimeFormatPartTypes): number =>
    Number(parts.find((entry) => entry.type === type)?.value ?? 0)
  const wallAsUtc = Date.UTC(
    part("year"),
    part("month") - 1,
    part("day"),
    part("hour"),
    part("minute"),
    part("second"),
  )
  return wallAsUtc - Math.floor(instantMs / 1000) * 1000
}

/** `YYYY-MM-DD HH:mm` in `timeZone`. */
export function formatLocal(unixSeconds: number, timeZone: string): string {
  const ms = unixSeconds * 1000
  return new Date(ms + offsetAt(ms, timeZone)).toISOString().slice(0, 16).replace("T", " ")
}

const DATE_ONLY = /^\d{4}-\d{2}-\d{2}$/
const HAS_OFFSET = /(Z|[+-]\d{2}:?\d{2})$/i

/**
 * Parses an ISO date or date-time to Unix seconds. Without an explicit offset
 * the value is read as wall-clock time in `timeZone`; a bare date means its
 * midnight there.
 */
export function parseLocal(input: string, timeZone: string): number {
  const text = input.trim()
  if (HAS_OFFSET.test(text)) return Math.floor(Date.parse(text) / 1000)
  const wall = Date.parse(DATE_ONLY.test(text) ? `${text}T00:00:00Z` : `${text.replace(" ", "T")}Z`)
  if (Number.isNaN(wall)) throw new Error(`not an ISO date or date-time: ${input}`)
  // Two passes settle the offset across a DST transition.
  const first = wall - offsetAt(wall, timeZone)
  return Math.floor((wall - offsetAt(first, timeZone)) / 1000)
}
