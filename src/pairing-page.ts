// The script avoids backticks and `${` so it can live inside a template.
import type { AuthMode } from "./auth/config"
import { type Lang, LOCALE, TEXT } from "./text"

const REPOSITORY_URL = "https://github.com/felipeadeildo/whatsapp-mcp"

const GITHUB_MARK = `<svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/></svg>`

export const STYLE = `
:root {
  --paper: #0a1512;
  --surface: #11201b;
  --ink: #dce9e3;
  --muted: #8ba69d;
  --line: #24372f;
  --signal: #3cc488;
  --wait: #e0a43a;
  --alert: #f0766a;
  --display: ui-rounded, "SF Pro Rounded", "Nunito", system-ui, sans-serif;
  --text: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  --code: ui-monospace, "SF Mono", "Cascadia Code", Menlo, monospace;
  color-scheme: dark;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  min-height: 100vh;
  background: var(--paper);
  color: var(--ink);
  font: 1rem/1.55 var(--text);
}
main { max-width: 60rem; margin: 0 auto; padding: 2rem 1.5rem 4rem; }
.top { display: flex; align-items: center; justify-content: space-between; gap: 1rem; flex-wrap: wrap; }
.brand { display: flex; align-items: baseline; gap: 0.5rem; color: var(--muted); font-size: 0.95rem; }
.brand strong { color: var(--ink); font-weight: 600; }
.source {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--muted);
  font-size: 0.92rem;
  text-decoration: none;
  border-radius: 0.4rem;
}
.source svg { width: 1.15rem; height: 1.15rem; }
.source:hover { color: var(--ink); }
.source:focus-visible { outline: 3px solid var(--signal); outline-offset: 3px; }
.hero {
  display: grid;
  gap: 2.5rem;
  margin-top: 3.5rem;
  align-items: center;
}
@media (min-width: 48rem) {
  .hero { grid-template-columns: minmax(0, 1fr) 19rem; gap: 4rem; }
}
h1 {
  margin: 0;
  font: 650 clamp(1.9rem, 4.2vw, 2.7rem)/1.1 var(--display);
  letter-spacing: -0.015em;
  text-wrap: balance;
}
.who { margin: 0.6rem 0 0; color: var(--muted); font-variant-numeric: tabular-nums; }
.lead { margin: 1.1rem 0 0; max-width: 34rem; font-size: 1.08rem; text-wrap: pretty; }
.lead strong { font-variant-numeric: tabular-nums; }
.steps { margin: 1.6rem 0 0; padding: 0; list-style: none; counter-reset: step; max-width: 32rem; }
.steps li {
  counter-increment: step;
  display: grid;
  grid-template-columns: 1.9rem 1fr;
  gap: 0.6rem;
  padding: 0.55rem 0;
  border-top: 1px solid var(--line);
}
.steps li::before {
  content: counter(step);
  font: 650 1.05rem/1.5 var(--display);
  color: var(--signal);
}
.note { margin: 1rem 0 0; color: var(--muted); font-size: 0.92rem; }
.detail {
  margin: 1.1rem 0 0;
  color: var(--alert);
  font-size: 0.92rem;
  overflow-wrap: anywhere;
}
.actions { display: flex; flex-wrap: wrap; gap: 0.75rem; margin-top: 1.75rem; }
.actions:empty { display: none; }
button {
  font: 600 1rem/1 var(--text);
  border-radius: 0.6rem;
  padding: 0.85rem 1.3rem;
  border: 1.5px solid transparent;
  cursor: pointer;
}
button:focus-visible { outline: 3px solid var(--signal); outline-offset: 2px; }
button:disabled { cursor: progress; opacity: 0.7; }
.primary { background: var(--ink); color: var(--paper); }
.primary:hover:not(:disabled) { background: var(--signal); }
.quiet { background: transparent; color: var(--alert); border-color: var(--line); }
.quiet:hover:not(:disabled) { border-color: var(--alert); }

.viewfinder {
  --corner: var(--line);
  position: relative;
  aspect-ratio: 1;
  width: min(100%, 19rem);
  justify-self: center;
  display: grid;
  place-items: center;
  padding: 1.4rem;
  background:
    linear-gradient(var(--corner) 0 0) top left / 2.2rem 4px,
    linear-gradient(var(--corner) 0 0) top left / 4px 2.2rem,
    linear-gradient(var(--corner) 0 0) top right / 2.2rem 4px,
    linear-gradient(var(--corner) 0 0) top right / 4px 2.2rem,
    linear-gradient(var(--corner) 0 0) bottom left / 2.2rem 4px,
    linear-gradient(var(--corner) 0 0) bottom left / 4px 2.2rem,
    linear-gradient(var(--corner) 0 0) bottom right / 2.2rem 4px,
    linear-gradient(var(--corner) 0 0) bottom right / 4px 2.2rem,
    var(--surface);
  background-repeat: no-repeat;
  border-radius: 0.35rem;
}
.viewfinder[data-mode="qr"] { --corner: var(--ink); }
.viewfinder[data-mode="wait"] { --corner: var(--wait); }
.viewfinder[data-mode="done"] { --corner: var(--signal); }
.viewfinder[data-mode="alert"] { --corner: var(--alert); }
.viewfinder[data-mode="sync"] { --corner: var(--signal); }
.visual { width: 100%; height: 100%; display: grid; place-items: center; }
.visual svg { width: 100%; height: auto; display: block; }
.visual .qr { background: #fff; padding: 0.4rem; border-radius: 0.25rem; }
.visual .glyph { width: 42%; color: var(--corner); }
.viewfinder[data-mode="idle"] .glyph { color: var(--muted); }
.fresh { animation: fresh 0.35s ease-out; }
@keyframes fresh { from { opacity: 0.25; } }
.spinner {
  width: 3.2rem;
  height: 3.2rem;
  border-radius: 50%;
  border: 4px solid var(--line);
  border-top-color: var(--wait);
  animation: spin 0.9s linear infinite;
}
@keyframes spin { to { transform: rotate(1turn); } }
.ring { position: relative; width: 64%; aspect-ratio: 1; display: grid; place-items: center; text-align: center; }
.visual .ring svg { position: absolute; inset: 0; height: 100%; transform: rotate(-90deg); }
.ring circle { fill: none; stroke-width: 3.5; }
.ring .track { stroke: var(--line); }
.ring .bar {
  stroke: var(--signal);
  stroke-linecap: round;
  stroke-dasharray: 125.66;
  stroke-dashoffset: calc(125.66 * (1 - var(--progress, 0)));
  transition: stroke-dashoffset 0.6s ease-out;
}
.ring[data-indeterminate] svg { animation: spin 1.4s linear infinite; }
.ring[data-indeterminate] .bar { stroke-dashoffset: 94; }
.ring-value { font: 650 1.7rem/1 var(--display); font-variant-numeric: tabular-nums; }
.ring-label { margin-top: 0.3rem; color: var(--muted); font-size: 0.8rem; }
.live { display: flex; align-items: center; gap: 0.55rem; margin: 1rem 0 0; color: var(--muted); font-size: 0.95rem; }
.dot {
  flex: none;
  width: 0.55rem;
  height: 0.55rem;
  border-radius: 50%;
  background: var(--signal);
  animation: pulse 2s ease-out infinite;
}
@keyframes pulse {
  from { box-shadow: 0 0 0 0 rgb(60 196 136 / 0.6); }
  to { box-shadow: 0 0 0 0.6rem rgb(60 196 136 / 0); }
}
.bump { color: var(--signal); font-weight: 600; font-variant-numeric: tabular-nums; opacity: 0; }
.bump.pop { animation: pop 2.6s ease-out forwards; }
@keyframes pop {
  0% { opacity: 0; transform: translateY(0.35rem); }
  12%, 80% { opacity: 1; transform: none; }
  100% { opacity: 0; }
}
.check path { stroke-dasharray: 60; stroke-dashoffset: 0; animation: draw 0.5s ease-out; }
@keyframes draw { from { stroke-dashoffset: 60; } }

.connect { margin-top: 4rem; padding-top: 2rem; border-top: 1px solid var(--line); }
.connect h2 { margin: 0 0 0.4rem; font: 650 1.35rem/1.2 var(--display); }
.connect > p { margin: 0 0 1.4rem; color: var(--muted); max-width: 40rem; }
.field { margin-top: 1rem; }
.field-label { margin: 0 0 0.35rem; font-weight: 600; }
.copyable { display: flex; gap: 0.6rem; align-items: stretch; }
.copyable code {
  flex: 1;
  min-width: 0;
  padding: 0.7rem 0.85rem;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 0.5rem;
  font: 0.88rem/1.5 var(--code);
  overflow-wrap: anywhere;
}
.copyable button { padding: 0.6rem 1rem; background: transparent; color: var(--ink); border-color: var(--line); }
.copyable button:hover { border-color: var(--ink); }
.narrow { max-width: 30rem; }
.card { margin-top: 3.5rem; }
.form { margin-top: 1.5rem; }
.form label { display: block; margin-bottom: 0.4rem; font-weight: 600; }
.form input[type="password"] {
  width: 100%;
  padding: 0.75rem 0.85rem;
  background: var(--surface);
  color: var(--ink);
  border: 1px solid var(--line);
  border-radius: 0.5rem;
  font: 1rem/1.4 var(--text);
}
.form input:focus-visible { outline: 3px solid var(--signal); outline-offset: 2px; }
[hidden] { display: none !important; }
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after { animation: none !important; transition: none !important; }
}
`

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

$("account").textContent = account

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

function copyButton(button, getText) {
  button.textContent = t.copy
  button.addEventListener("click", async () => {
    await navigator.clipboard.writeText(getText())
    button.textContent = t.copied
    setTimeout(() => { button.textContent = t.copy }, 1600)
  })
}

const endpoint = location.origin + base + "/mcp"
const command = "claude mcp add --transport http whatsapp " + endpoint +
  (mode === "api_key" ? ' --header "Authorization: Bearer <SECRET>"' : "")
$("endpoint").textContent = endpoint
$("command").textContent = command
copyButton($("copy-endpoint"), () => endpoint)
copyButton($("copy-command"), () => command)
$("connect-title").textContent = t.connect[mode][0]
$("connect-lead").textContent = t.connect[mode][1]
$("endpoint-label").textContent = t.endpoint
$("command-label").textContent = t.claude
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

export function pairingPage(accountId: string, mode: AuthMode, lang: Lang): string {
  return `<!doctype html>
<html lang="${LOCALE[lang]}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<meta name="theme-color" content="#0a1512">
<title>whatsapp-mcp / ${accountId}</title>
<style>${STYLE}</style>
</head>
<body data-account="${accountId}" data-mode="${mode}">
<main>
  <header class="top">
    <div class="brand"><strong>whatsapp-mcp</strong><span>/</span><span id="account"></span></div>
    <a class="source" href="${REPOSITORY_URL}" target="_blank" rel="noopener noreferrer">${GITHUB_MARK}<span>felipeadeildo/whatsapp-mcp</span></a>
  </header>
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
    <div class="field">
      <p id="endpoint-label" class="field-label"></p>
      <div class="copyable"><code id="endpoint"></code><button id="copy-endpoint" type="button"></button></div>
    </div>
    <div class="field">
      <p id="command-label" class="field-label"></p>
      <div class="copyable"><code id="command"></code><button id="copy-command" type="button"></button></div>
    </div>
  </section>
</main>
<script type="application/json" id="text">${JSON.stringify(TEXT[lang].pairing).replaceAll("<", "\\u003c")}</script>
<script>${SCRIPT}</script>
</body>
</html>`
}
