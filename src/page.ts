// What every page shares: the style, the header with the source link and the credit.
import { type Lang, LOCALE, TEXT } from "./text"

const REPOSITORY_URL = "https://github.com/felipeadeildo/whatsapp-mcp"

const GITHUB_MARK = `<svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/></svg>`

const ADEILDO_DEV = "https://adeildo.dev"
const CAT = `${ADEILDO_DEV}/assets/images/cat.svg`
const FAVICON = `${ADEILDO_DEV}/assets/images/favicon.svg`

const STYLE = `
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
main {
  max-width: 60rem;
  min-height: 100dvh;
  margin: 0 auto;
  padding: 2rem 1.5rem;
  display: flex;
  flex-direction: column;
}
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
button { touch-action: manipulation; }
button:focus-visible { outline: 3px solid var(--signal); outline-offset: 2px; }
button:disabled { cursor: progress; opacity: 0.7; }
.primary { background: var(--ink); color: var(--paper); }
.primary:hover:not(:disabled) { background: var(--signal); }
.quiet { background: transparent; color: var(--alert); border-color: var(--line); }
.quiet:hover:not(:disabled) { border-color: var(--alert); }
.secondary { background: transparent; color: var(--ink); border-color: var(--line); }
.secondary:hover:not(:disabled) { border-color: var(--ink); }

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
.copyable { position: relative; }
.copyable code {
  display: block;
  padding: 0.7rem 3.2rem 0.7rem 0.85rem;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 0.5rem;
  font: 0.88rem/1.5 var(--code);
  white-space: pre-wrap;
  overflow-wrap: break-word;
}
.copy {
  position: absolute;
  top: 0.45rem;
  right: 0.45rem;
  display: grid;
  place-items: center;
  width: 2rem;
  height: 2rem;
  padding: 0;
  border-radius: 0.4rem;
  background: var(--surface);
  color: var(--muted);
}
.copy svg { width: 1rem; height: 1rem; }
.tok-bin { color: var(--signal); font-weight: 600; }
.tok-flag { color: var(--muted); }
.tok-str { color: var(--wait); }
/* A URL moves to the next line whole, and breaks inside only when it cannot fit alone. */
.tok-url { display: inline-block; max-width: 100%; overflow-wrap: anywhere; }
.copy:hover { color: var(--ink); background: var(--line); }
.copy[data-copied] { color: var(--signal); }
.field-note { margin: 0.4rem 0 0; color: var(--muted); font-size: 0.88rem; }
.field-note code { font: 0.84rem var(--code); color: var(--ink); }
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
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
.monogram { font: 650 clamp(4rem, 9vw, 6.5rem)/1 var(--display); color: var(--ink); }
@media (max-width: 47.99rem) {
  .viewfinder.aside { display: none; }
}
.credit { margin-top: auto; padding-top: 4rem; display: flex; justify-content: center; }
.credit a {
  display: inline-flex;
  align-items: center;
  gap: 0.6rem;
  color: var(--muted);
  font-size: 0.9rem;
  text-decoration: none;
  border-radius: 0.4rem;
}
.credit a:hover { color: var(--ink); }
.credit a:focus-visible { outline: 3px solid var(--signal); outline-offset: 3px; }
.credit img { display: block; }
[hidden] { display: none !important; }
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after { animation: none !important; transition: none !important; }
}
`

export function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, (char) => `&#${char.charCodeAt(0)};`)
}

export function page(options: {
  lang: Lang
  title: string
  main: string
  accountId?: string
  bodyAttributes?: string
  scripts?: string
}): string {
  const { lang, title, main, accountId, bodyAttributes = "", scripts = "" } = options
  const account = accountId ? `<span>/</span><span>${accountId}</span>` : ""
  return `<!doctype html>
<html lang="${LOCALE[lang]}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<meta name="theme-color" content="#0a1512">
<link rel="icon" href="${FAVICON}">
<title>whatsapp-mcp / ${escapeHtml(title)}</title>
<style>${STYLE}</style>
</head>
<body ${bodyAttributes}>
<main>
  <header class="top">
    <div class="brand" translate="no"><strong>whatsapp-mcp</strong>${account}</div>
    <a class="source" href="${REPOSITORY_URL}" target="_blank" rel="noopener noreferrer">${GITHUB_MARK}<span translate="no">felipeadeildo/whatsapp-mcp</span></a>
  </header>
  ${main}
  <footer class="credit">
    <a href="${ADEILDO_DEV}" target="_blank" rel="noopener noreferrer"><img src="${CAT}" width="28" height="28" alt="" referrerpolicy="no-referrer" loading="lazy"><span>${TEXT[lang].credit} <strong translate="no">adeildo.dev</strong></span></a>
  </footer>
</main>
${scripts}
</body>
</html>`
}
