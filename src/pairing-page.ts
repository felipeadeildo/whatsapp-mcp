// The script avoids backticks and `${` so it can live inside a template.
import type { AuthMode } from "./auth/config"
import { page } from "./page"
import { type Lang, TEXT } from "./text"

const SCRIPT = String.raw`
const GLYPHS = {
  done: '<svg class="glyph check" viewBox="0 0 48 48" fill="none" aria-hidden="true"><path d="M10 25l9 9 19-20" stroke="currentColor" stroke-width="5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  alert: '<svg class="glyph" viewBox="0 0 48 48" fill="none" aria-hidden="true"><path d="M24 12v15" stroke="currentColor" stroke-width="5" stroke-linecap="round"/><circle cx="24" cy="36" r="3.2" fill="currentColor"/></svg>',
  idle: '<svg class="glyph" viewBox="0 0 48 48" fill="none" aria-hidden="true"><rect x="14" y="6" width="20" height="36" rx="4" stroke="currentColor" stroke-width="3.5"/><path d="M21 35h6" stroke="currentColor" stroke-width="3.5" stroke-linecap="round"/></svg>',
  spin: '<div class="spinner" role="presentation"></div>',
  ring: '<div class="ring"><svg viewBox="0 0 48 48" aria-hidden="true"><circle class="track" cx="24" cy="24" r="20"/><circle class="bar" cx="24" cy="24" r="20"/></svg><div><div class="ring-value"></div><div class="ring-label"></div></div></div>',
}

const lang = document.documentElement.lang
const t = JSON.parse(document.getElementById("text").textContent)
const account = document.body.dataset.account
const mode = document.body.dataset.mode
const base = location.pathname.endsWith("/") ? location.pathname.slice(0, -1) : location.pathname
const number = new Intl.NumberFormat(lang)
const day = new Intl.DateTimeFormat(lang, { dateStyle: "long" })
const relative = new Intl.RelativeTimeFormat(lang, { numeric: "auto" })
const calm = matchMedia("(prefers-reduced-motion: reduce)").matches
const shownCounts = {}
let lastTotal = null
const $ = (id) => document.getElementById(id)
let timer = null
let lastQr = null
let busy = false


function fill(template, values) {
  return template.replace(/\{(\w+)\}/g, (_, name) => values[name])
}

async function call(path, method) {
  const response = await fetch(base + "/" + path, { method: method || "GET" })
  // The session ran out: reloading shows the sign-in form.
  if (response.status === 401) {
    location.reload()
    throw new Error("unauthorized")
  }
  const body = await response.json()
  if (!response.ok) throw new Error(body.error || response.statusText)
  return body
}

// Numbers climb from the value shown last time, so new batches are visible.
function countUp(root) {
  root.querySelectorAll("[data-count]").forEach((element) => {
    const key = element.dataset.count
    const to = Number(element.dataset.value)
    const from = shownCounts[key] ?? to
    shownCounts[key] = to
    element.textContent = number.format(from)
    if (calm || from === to) return
    const start = performance.now()
    const step = (now) => {
      const k = Math.min(1, (now - start) / 900)
      element.textContent = number.format(Math.round(from + (to - from) * (1 - (1 - k) ** 3)))
      if (k < 1) requestAnimationFrame(step)
    }
    requestAnimationFrame(step)
  })
}

function counter(key, value) {
  return '<span data-count="' + key + '" data-value="' + value + '"></span>'
}

function ago(seconds) {
  const diff = Math.min(0, Math.round(seconds - Date.now() / 1000))
  for (const [unit, size] of [["day", 86400], ["hour", 3600], ["minute", 60]]) {
    if (-diff >= size) return relative.format(Math.round(diff / size), unit)
  }
  return relative.format(diff, "second")
}

function bump(total) {
  if (lastTotal !== null && total > lastTotal) {
    const element = $("bump")
    element.textContent = fill(t.fresh, { count: number.format(total - lastTotal) })
    // Reading the layout in between restarts the animation.
    element.classList.remove("pop")
    void element.offsetWidth
    element.classList.add("pop")
  }
  lastTotal = total
}

function showRing(progress) {
  const ring = $("visual").querySelector(".ring")
  const known = typeof progress === "number"
  ring.toggleAttribute("data-indeterminate", !known)
  ring.style.setProperty("--progress", known ? progress / 100 : 0)
  ring.querySelector(".ring-value").textContent = known ? progress + "%" : ""
  ring.querySelector(".ring-label").textContent = known ? t.history : ""
}

function show(view) {
  $("title").textContent = view.title
  $("lead").innerHTML = view.lead
  countUp($("lead"))
  $("live").hidden = !view.live
  if (view.live) $("live-text").textContent = t.live + " · " + fill(t.lastMessage, { ago: ago(view.live) })
  $("who").hidden = !view.who
  $("who").textContent = view.who || ""
  $("steps").hidden = !view.steps
  $("note").hidden = !view.steps
  $("detail").hidden = !view.detail
  $("detail").textContent = view.detail ? t.lastError + view.detail : ""
  $("connect").hidden = !view.connect
  $("unlink").hidden = !view.connect
  $("link").hidden = !view.link

  const finder = $("viewfinder")
  finder.dataset.mode = view.mode
  const visual = $("visual")
  if (view.qr) {
    if (view.qr !== lastQr) {
      visual.innerHTML = '<div class="qr fresh">' + view.qr + "</div>"
      lastQr = view.qr
    }
  } else {
    lastQr = null
    const glyph = GLYPHS[view.glyph]
    if (visual.dataset.glyph !== view.glyph) {
      visual.innerHTML = glyph
      visual.dataset.glyph = view.glyph
    }
  }
  if (view.qr) visual.dataset.glyph = ""
  if (view.glyph === "ring") showRing(view.progress)
}

function escapeHtml(text) {
  const span = document.createElement("span")
  span.textContent = text
  return span.innerHTML
}

function viewFor(status) {
  switch (status.state) {
    case "open": {
      const archive = status.archive
      const syncing = status.history.active || archive.messages === 0
      const counts = { messages: counter("messages", archive.messages), chats: counter("chats", archive.chats) }
      let lead
      if (archive.messages === 0) lead = escapeHtml(t.syncStart)
      else if (syncing) lead = fill(t.syncing, counts)
      else lead = fill(t.archived, { ...counts, date: day.format(new Date(archive.oldestMessageAt * 1000)) })
      const phone = status.me ? "+" + status.me.split("@")[0] : null
      return {
        title: fill(t.linked, { name: status.myName || phone || account }),
        who: status.myName ? phone : null,
        lead,
        mode: syncing ? "sync" : "done",
        glyph: syncing ? "ring" : "done",
        progress: status.history.progress,
        live: syncing ? null : archive.newestMessageAt,
        connect: true,
      }
    }
    case "pairing":
      return { title: t.pairing[0], lead: escapeHtml(t.pairing[1]), mode: "qr", qr: status.qrSvg, steps: true }
    case "connecting":
      return { title: t.connecting[0], lead: escapeHtml(t.connecting[1]), mode: "wait", glyph: "spin" }
    case "closed":
      return {
        title: t.reconnecting[0],
        lead: escapeHtml(t.reconnecting[1]),
        mode: "wait",
        glyph: "spin",
        detail: status.lastError,
      }
    case "logged_out":
      if (status.lastError) {
        return { title: t.unlinked[0], lead: escapeHtml(t.unlinked[1]), mode: "alert", glyph: "alert", link: true }
      }
      return { title: t.notLinked[0], lead: escapeHtml(t.notLinked[1]), mode: "idle", glyph: "idle", link: true }
    default:
      return { title: t.notLinked[0], lead: escapeHtml(t.notLinked[1]), mode: "idle", glyph: "idle", link: true }
  }
}

function schedule(ms) {
  clearTimeout(timer)
  if (!document.hidden) timer = setTimeout(refresh, ms)
}

async function refresh() {
  try {
    const status = await call("status")
    const view = viewFor(status)
    if (!busy) show(view)
    if (status.state === "open" && view.live) bump(status.archive.messages)
    else lastTotal = null
    schedule(status.state === "open" && view.live ? 5000 : 2000)
  } catch (error) {
    if (error.message === "unauthorized") return
    show({ title: t.offline[0], lead: escapeHtml(t.offline[1]), mode: "alert", glyph: "alert", detail: error.message })
    schedule(5000)
  }
}

async function act(button, path, pendingLabel) {
  const label = button.textContent
  busy = true
  button.disabled = true
  button.textContent = pendingLabel
  try {
    await call(path, "POST")
  } catch (error) {
    $("detail").hidden = false
    $("detail").textContent = t.lastError + error.message
  } finally {
    busy = false
    button.disabled = false
    button.textContent = label
    refresh()
  }
}

const ICONS = {
  copy: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="12" height="12" rx="2.5"/><path d="M5 15H4.5A1.5 1.5 0 0 1 3 13.5v-9A1.5 1.5 0 0 1 4.5 3h9A1.5 1.5 0 0 1 15 4.5V5"/></svg>',
  check: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7.5"/></svg>',
}

// Shell syntax colors, as in docs/use-cases.svg: program, flags, strings, the rest.
function highlight(command) {
  return (command.match(/"[^"]*"|\s+|[^\s"]+/g) || []).map((token, index) => {
    if (/^\s+$/.test(token)) return document.createTextNode(token)
    const span = document.createElement("span")
    if (index === 0) span.className = "tok-bin"
    else if (token.startsWith("--")) span.className = "tok-flag"
    else if (token.startsWith('"')) span.className = "tok-str"
    span.textContent = token
    return span
  })
}

// Fills one labelled, copyable field: a value, an icon button and an optional note.
function field(id, label, value, note) {
  $(id + "-label").textContent = label
  $(id).replaceChildren(...(id === "endpoint" ? [value] : highlight(value)))
  const button = $(id + "-copy")
  const status = $(id + "-status")
  button.innerHTML = ICONS.copy
  button.setAttribute("aria-label", t.copy + ": " + label)
  button.addEventListener("click", async () => {
    await navigator.clipboard.writeText(value)
    button.innerHTML = ICONS.check
    button.dataset.copied = ""
    status.textContent = t.copied
    setTimeout(() => {
      button.innerHTML = ICONS.copy
      delete button.dataset.copied
      status.textContent = ""
    }, 1600)
  })
  if (note) {
    $(id + "-note").innerHTML = note
    $(id + "-note").hidden = false
  }
}

const endpoint = location.origin + base + "/mcp"
const bearer = mode === "api_key"
field("endpoint", t.endpoint, endpoint)
field("claude", t.claude, "claude mcp add --transport http whatsapp " + endpoint +
  (bearer ? ' --header "Authorization: Bearer <SECRET>"' : ""))
field("pi", t.pi, "pi mcp add whatsapp --url " + endpoint +
  (bearer ? " --bearer-token-env-var WHATSAPP_MCP_SECRET" : ""), t.piNote[mode])
$("connect-title").textContent = t.connect[mode][0]
$("connect-lead").textContent = t.connect[mode][1]
$("note").textContent = t.phoneNote
t.steps.forEach((step) => {
  const item = document.createElement("li")
  item.textContent = step
  $("steps").append(item)
})

$("link").textContent = t.link
$("link").addEventListener("click", () => act($("link"), "start", t.linking))
$("unlink").textContent = t.unlink
$("unlink").addEventListener("click", () => {
  if (confirm(t.confirmUnlink)) act($("unlink"), "logout", t.unlinking)
})
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) refresh()
})

show({ title: t.loading[0], lead: escapeHtml(t.loading[1]), mode: "wait", glyph: "spin" })
refresh()
`

function copyableField(id: string): string {
  return `
    <div class="field">
      <p id="${id}-label" class="field-label"></p>
      <div class="copyable">
        <code id="${id}" translate="no"></code>
        <button id="${id}-copy" class="copy" type="button"></button>
        <span id="${id}-status" class="sr-only" role="status"></span>
      </div>
      <p id="${id}-note" class="field-note" hidden></p>
    </div>`
}

export function pairingPage(accountId: string, mode: AuthMode, lang: Lang): string {
  const text = JSON.stringify(TEXT[lang].pairing).replaceAll("<", "\\u003c")
  return page({
    lang,
    title: accountId,
    accountId,
    bodyAttributes: `data-account="${accountId}" data-mode="${mode}"`,
    main: `
  <section class="hero">
    <div>
      <h1 id="title" aria-live="polite"></h1>
      <p id="who" class="who" hidden></p>
      <p id="lead" class="lead"></p>
      <p id="live" class="live" hidden><span class="dot" aria-hidden="true"></span><span id="live-text"></span><span id="bump" class="bump" aria-live="polite"></span></p>
      <ol id="steps" class="steps" hidden></ol>
      <p id="note" class="note" hidden></p>
      <p id="detail" class="detail" hidden></p>
      <div class="actions">
        <button id="link" class="primary" type="button" hidden></button>
        <button id="unlink" class="quiet" type="button" hidden></button>
      </div>
    </div>
    <div id="viewfinder" class="viewfinder" data-mode="wait">
      <div id="visual" class="visual"></div>
    </div>
  </section>
  <section id="connect" class="connect" hidden>
    <h2 id="connect-title"></h2>
    <p id="connect-lead"></p>
    ${["endpoint", "claude", "pi"].map(copyableField).join("")}
  </section>`,
    scripts: `<script type="application/json" id="text">${text}</script>
<script>${SCRIPT}</script>`,
  })
}
