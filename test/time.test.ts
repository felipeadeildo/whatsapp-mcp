import { describe, expect, test } from "bun:test"

import { toFtsQuery } from "../src/store/archive"
import { formatLocal, parseLocal } from "../src/time"

const SAO_PAULO = "America/Sao_Paulo"
const NEW_YORK = "America/New_York"
// 2026-10-08 15:32:00 UTC
const INSTANT = 1_791_473_520

describe("formatLocal", () => {
  test("renders wall-clock time in the zone", () => {
    expect(formatLocal(INSTANT, "UTC")).toBe("2026-10-08 15:32")
    expect(formatLocal(INSTANT, SAO_PAULO)).toBe("2026-10-08 12:32")
  })
})

describe("parseLocal", () => {
  test("reads a time without offset as wall-clock time in the zone", () => {
    expect(parseLocal("2026-10-08 12:32", SAO_PAULO)).toBe(INSTANT)
    expect(parseLocal("2026-10-08T12:32", SAO_PAULO)).toBe(INSTANT)
  })

  test("an explicit offset wins over the zone", () => {
    expect(parseLocal("2026-10-08T15:32:00Z", SAO_PAULO)).toBe(INSTANT)
  })

  test("a bare date is midnight in the zone", () => {
    expect(formatLocal(parseLocal("2026-10-08", SAO_PAULO), SAO_PAULO)).toBe("2026-10-08 00:00")
  })

  test("round-trips across a DST change", () => {
    expect(formatLocal(parseLocal("2026-11-01 12:00", NEW_YORK), NEW_YORK)).toBe("2026-11-01 12:00")
    expect(formatLocal(parseLocal("2026-03-09 12:00", NEW_YORK), NEW_YORK)).toBe("2026-03-09 12:00")
  })

  test("rejects what is not a date", () => {
    expect(() => parseLocal("ontem", SAO_PAULO)).toThrow()
  })
})

describe("toFtsQuery", () => {
  test("every word becomes a quoted prefix term", () => {
    expect(toFtsQuery("reunião  orçamento")).toBe('"reunião"* "orçamento"*')
  })

  test("FTS syntax in the input cannot break the query", () => {
    expect(toFtsQuery('foo" OR "bar')).toBe('"foo"* "OR"* "bar"*')
  })

  test("blank input is no query", () => {
    expect(toFtsQuery("   ")).toBeNull()
  })
})
