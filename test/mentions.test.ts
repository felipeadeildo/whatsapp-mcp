import { describe, expect, test } from "bun:test"

import { mentionJid, mentionText } from "../src/whatsapp/mentions"

const PN = "5511999999999@s.whatsapp.net"
const LID = "123456789@lid"

describe("mentions", () => {
  test("a group addressed by LID gets the LID, a group by phone gets the phone", () => {
    expect(mentionJid({ jid: LID, lid: LID, pn: PN }, "lid")).toBe(LID)
    expect(mentionJid({ jid: LID, lid: LID, pn: PN }, "pn")).toBe(PN)
    // Without the LID, the phone is all there is.
    expect(mentionJid({ jid: PN, lid: null, pn: PN }, "lid")).toBe(PN)
  })

  test("@<phone>, @+<phone> and @<lid> in the text become the user of the mentioned JID", () => {
    const targets = [{ jid: LID, known: [PN, LID] }]
    expect(mentionText("oi @5511999999999, tudo bem?", targets)).toBe("oi @123456789, tudo bem?")
    expect(mentionText("oi @+5511999999999", targets)).toBe("oi @123456789")
    expect(mentionText("oi @123456789", targets)).toBe("oi @123456789")
  })

  test("a longer number that starts with the phone is left alone", () => {
    const targets = [{ jid: LID, known: [PN] }]
    expect(mentionText("@55119999999990", targets)).toBe("@55119999999990")
  })
})
