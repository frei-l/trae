// trae — one state object per view, rendered with plain DOM. No framework.
"use strict";

const $ = (s, root) => (root || document).querySelector(s);
const $$ = (s, root) => Array.from((root || document).querySelectorAll(s));
const prefs = Object.assign({ theme: "system", textSize: 100, web: false }, window.bootPrefs || {});

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined && text !== null) e.textContent = text;
  return e;
}

// Icons: 16×16 stroked paths, drawn in the text colour.
const CHEV = "m5 6.5 3 3 3-3";
const BACK = "M9.5 4 5.5 8l4 4";
const CHECK = "m3.5 8.5 3 3 6-7";
const COPY_ICON = "M5.5 5.5V3.5h7v7h-2M3.5 5.5h7v7h-7z";
const GEAR = "M8 10.2a2.2 2.2 0 1 0 0-4.4 2.2 2.2 0 0 0 0 4.4ZM13 8.9V7.1l-1.5-.4-.4-1 .8-1.3-1.3-1.3-1.3.8-1-.4L8.9 2H7.1l-.4 1.5-1 .4-1.3-.8-1.3 1.3.8 1.3-.4 1L2 7.1v1.8l1.5.4.4 1-.8 1.3 1.3 1.3 1.3-.8 1 .4.4 1.5h1.8l.4-1.5 1-.4 1.3.8 1.3-1.3-.8-1.3.4-1z";
const TRASH = "M3 4.5h10M6.5 4.5V3h3v1.5M4.5 4.5l.6 8.5h5.8l.6-8.5";
const SEARCH = "m13 13-3-3m1-3.5a4.5 4.5 0 1 1-9 0 4.5 4.5 0 0 1 9 0Z";
const OUT = "M9 3h4v4M13 3 7.5 8.5M11 9.5V13H3V5h3.5";

function svg(d, size, stroke) {
  const ns = "http://www.w3.org/2000/svg";
  const s = document.createElementNS(ns, "svg");
  s.setAttribute("width", size || 12);
  s.setAttribute("height", size || 12);
  s.setAttribute("viewBox", "0 0 16 16");
  s.setAttribute("fill", "none");
  s.setAttribute("aria-hidden", "true");
  const p = document.createElementNS(ns, "path");
  p.setAttribute("d", d);
  p.setAttribute("stroke", "currentColor");
  p.setAttribute("stroke-width", stroke || 1.6);
  p.setAttribute("stroke-linecap", "round");
  p.setAttribute("stroke-linejoin", "round");
  s.appendChild(p);
  return s;
}

// ---- the Go side ------------------------------------------------------------

async function api(path, body) {
  const res = await fetch("/api/" + path, {
    method: body === undefined ? "GET" : "POST",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 204) return null;
  let data = null;
  try { data = await res.json(); } catch (e) { /* empty or not JSON */ }
  if (!res.ok) throw new Error((data && data.error) || res.status + " " + res.statusText);
  return data;
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// ---- formatting ---------------------------------------------------------------

const nf = new Intl.NumberFormat("en-US");
function fmtInt(n) { return nf.format(n || 0); }
function fmtTok(n) {
  n = n || 0;
  if (n < 1000) return String(n);
  if (n < 1e6) return (n / 1e3).toFixed(n < 1e4 ? 1 : 0).replace(/\.0$/, "") + "k";
  return (n / 1e6).toFixed(n < 1e7 ? 2 : 1).replace(/\.0+$/, "") + "M";
}
function fmtCost(c) {
  if (!c) return "—";
  if (c < 0.01) return "$" + c.toFixed(4);
  if (c < 1) return "$" + c.toFixed(3);
  return "$" + c.toFixed(2);
}
function fmtDur(ns) {
  if (ns === undefined || ns === null || ns < 0) return "—";
  const ms = ns / 1e6;
  if (ms < 1) return (ms < 0.1 ? "<0.1" : ms.toFixed(1)) + " ms";
  if (ms < 1000) return Math.round(ms) + " ms";
  const s = ms / 1000;
  if (s < 60) return s.toFixed(s < 10 ? 2 : 1) + " s";
  const m = Math.floor(s / 60);
  return m + "m " + String(Math.round(s - m * 60)).padStart(2, "0") + "s";
}
function durClass(ns) {
  const s = ns / 1e9;
  return s <= 10 ? "duration-fast" : s <= 30 ? "duration-slow" : "duration-long";
}
function fmtTime(ns) {
  const d = new Date(ns / 1e6);
  const now = new Date();
  const hms = d.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
  if (d.toDateString() === now.toDateString()) return hms;
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric" }) + " " + hms.slice(0, 5);
}
function fmtFull(ns) {
  const d = new Date(ns / 1e6);
  return d.toLocaleString("en-US", { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit", fractionalSecondDigits: 3, hour12: false });
}
function plural(n, word) { return fmtInt(n) + " " + word + (n === 1 ? "" : "s"); }

// ---- status line and copying ------------------------------------------------------

let statusTimer = 0;
function status(msg, kind) {
  const s = $("#status");
  s.textContent = msg || "";
  s.className = "status" + (kind ? " " + kind : "");
  clearTimeout(statusTimer);
  if (msg) statusTimer = setTimeout(() => { s.classList.add("fade"); }, kind === "err" ? 8000 : 3500);
}

async function copy(text, what, btn) {
  let ok = false;
  try { await navigator.clipboard.writeText(text); ok = true; } catch (e) {
    const ta = el("textarea");
    ta.value = text;
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    try { ok = document.execCommand("copy"); } catch (e2) { ok = false; }
    ta.remove();
  }
  if (!ok) { status("Couldn't copy", "err"); return; }
  status((what || "Text") + " copied", "ok");
  if (btn) {
    btn.classList.add("done");
    const old = btn.firstChild;
    btn.replaceChildren(svg(CHECK, 12, 1.8));
    setTimeout(() => { btn.classList.remove("done"); btn.replaceChildren(old); }, 1200);
  }
}

function copyButton(get, what, title) {
  const b = el("button", "copy");
  b.title = title || "Copy";
  b.setAttribute("aria-label", b.title);
  b.appendChild(svg(COPY_ICON, 12));
  b.addEventListener("click", (e) => { e.stopPropagation(); copy(typeof get === "function" ? get() : get, what, b); });
  return b;
}

// ---- JSON, highlighted -------------------------------------------------------------

// tryJSON returns the parsed value of a string holding a JSON object or
// array, or undefined.
function tryJSON(s) {
  if (typeof s !== "string") return undefined;
  const t = s.trim();
  if (t.length < 2 || (t[0] !== "{" && t[0] !== "[")) return undefined;
  try { return JSON.parse(t); } catch (e) { return undefined; }
}

const TOKENS = /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)|([{}[\],])/g;

// highlight colours JSON text into a fragment of text nodes and spans.
function highlight(text) {
  const frag = document.createDocumentFragment();
  let last = 0;
  TOKENS.lastIndex = 0;
  let m;
  while ((m = TOKENS.exec(text))) {
    if (m.index > last) frag.appendChild(document.createTextNode(text.slice(last, m.index)));
    let cls = "tk-p";
    if (m[1]) cls = m[2] ? "tk-v" : "tk-s";
    else if (m[3]) cls = "tk-k";
    else if (m[4]) cls = "tk-n";
    const span = el("span", cls, m[1] ? m[1] : m[0]);
    frag.appendChild(span);
    if (m[1] && m[2]) frag.appendChild(document.createTextNode(m[2]));
    last = TOKENS.lastIndex;
  }
  if (last < text.length) frag.appendChild(document.createTextNode(text.slice(last)));
  return frag;
}

function pretty(v) { return JSON.stringify(v, null, 2); }

// codeBox shows text in a mono box: JSON pretty-printed and coloured, and
// long content clamped behind "Show full content".
function codeBox(text, opts) {
  opts = opts || {};
  const wrap = el("div", "cx-part");
  const pre = el("pre", "cx-t" + (opts.prose ? " prose" : ""));
  const parsed = opts.json === false ? undefined : (typeof text === "string" ? tryJSON(text) : text);
  if (parsed !== undefined && typeof parsed === "object") {
    text = pretty(parsed);
    pre.appendChild(highlight(text));
    pre.classList.remove("prose");
  } else {
    text = text === undefined || text === null ? "" : String(text);
    pre.textContent = text;
  }
  if (opts.head) wrap.appendChild(opts.head);
  wrap.appendChild(pre);
  const lines = text.split("\n").length;
  if (!opts.noClamp && (text.length > 700 || lines > 9)) {
    wrap.classList.add("clamp");
    const more = el("button", "cx-more", "Show full content");
    more.addEventListener("click", () => {
      const open = wrap.classList.toggle("clamp");
      more.textContent = open ? "Show full content" : "Show less";
    });
    wrap.appendChild(more);
  }
  wrap.dataset.text = text;
  return wrap;
}

// ---- segmented controls ---------------------------------------------------------------

function slide(group) {
  const thumb = group.querySelector(".thumb");
  const on = group.querySelector("button.on");
  if (!thumb) return;
  group.classList.toggle("none", !on);
  if (!on) return;
  thumb.style.width = on.offsetWidth + "px";
  thumb.style.transform = "translateX(" + on.offsetLeft + "px)";
}

// segs wires a .segs control: onChange(value) runs when the choice changes.
function segs(group, value, onChange) {
  const set = (v) => {
    group.querySelectorAll("button").forEach((b) => b.classList.toggle("on", b.dataset.v === String(v)));
    slide(group);
  };
  set(value);
  group.addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (!b || b.classList.contains("on")) return;
    set(b.dataset.v);
    onChange(b.dataset.v);
  });
  requestAnimationFrame(() => slide(group));
  return set;
}

function makeSegs(options, value, onChange) {
  const g = el("div", "segs");
  g.appendChild(el("span", "thumb"));
  for (const [v, label] of options) {
    const b = el("button", "opt", label);
    b.dataset.v = String(v);
    g.appendChild(b);
  }
  segs(g, value, onChange);
  return g;
}

// ---- popover picker -----------------------------------------------------------------------

let openPop = null;

function closePop() {
  if (!openPop) return;
  const { pop, anchor } = openPop;
  openPop = null;
  anchor.classList.remove("open");
  pop.classList.add("leaving");
  setTimeout(() => pop.remove(), 220);
  if (pendingRefresh) { pendingRefresh = false; refreshView(); }
}

// picker drops a searchable list from anchor. items: [{v, label}]; the
// first may be the "all" choice with v "".
function picker(anchor, items, current, onPick, opts) {
  if (openPop && openPop.anchor === anchor) { closePop(); return; }
  closePop();
  opts = opts || {};
  const pop = el("div", "pop");
  const search = el("div", "search");
  search.appendChild(svg(SEARCH, 13));
  const input = el("input");
  input.placeholder = opts.placeholder || "Filter…";
  input.spellcheck = false;
  search.appendChild(input);
  pop.appendChild(search);
  const ul = el("ul");
  pop.appendChild(ul);
  let shown = [];
  let sel = 0;
  const draw = () => {
    const q = input.value.trim().toLowerCase();
    shown = items.filter((it) => !q || it.v === "" || it.label.toLowerCase().includes(q));
    ul.replaceChildren();
    shown.forEach((it, i) => {
      const li = el("li", (it.v === "" ? "none" : opts.mono ? "mono" : "") + (i === sel ? " sel" : ""));
      const chk = el("span", "chk");
      if (it.v === current) chk.appendChild(svg(CHECK, 12, 1.8));
      li.appendChild(chk);
      li.appendChild(el("span", "v", it.label));
      li.addEventListener("mousemove", () => { if (sel !== i) { sel = i; mark(); } });
      li.addEventListener("click", () => { closePop(); onPick(it.v); });
      ul.appendChild(li);
    });
  };
  const mark = () => $$("li", ul).forEach((li, i) => li.classList.toggle("sel", i === sel));
  input.addEventListener("input", () => { sel = 0; draw(); });
  input.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown" || (e.ctrlKey && e.key === "n")) { sel = Math.min(shown.length - 1, sel + 1); mark(); e.preventDefault(); }
    else if (e.key === "ArrowUp" || (e.ctrlKey && e.key === "p")) { sel = Math.max(0, sel - 1); mark(); e.preventDefault(); }
    else if (e.key === "Enter" && shown[sel]) { const v = shown[sel].v; closePop(); onPick(v); e.preventDefault(); }
    else if (e.key === "Escape") { closePop(); anchor.focus(); e.preventDefault(); e.stopPropagation(); }
  });
  draw();
  document.body.appendChild(pop);
  const r = anchor.getBoundingClientRect();
  const left = Math.max(8, Math.min(r.left, window.innerWidth - pop.offsetWidth - 8));
  pop.style.left = left + "px";
  pop.style.top = r.bottom + 6 + "px";
  pop.style.setProperty("--ox", r.left + r.width / 2 - left + "px");
  anchor.classList.add("open");
  openPop = { pop, anchor };
  input.focus();
}

document.addEventListener("mousedown", (e) => {
  if (openPop && !openPop.pop.contains(e.target) && !openPop.anchor.contains(e.target)) closePop();
}, true);
window.addEventListener("resize", () => { closePop(); $$(".segs, .seg").forEach(slide); });

// facetPicker wires a .sess-pick button to a facet list (services/models).
function facetPicker(btn, state, key, onChange) {
  btn.appendChild(svg(CHEV, 11));
  const label = btn.querySelector("span");
  const sync = () => {
    label.textContent = state[key] || btn.dataset.all;
    btn.classList.toggle("set", !!state[key]);
  };
  btn.addEventListener("click", async () => {
    let f = { services: [], models: [] };
    try { f = await api("facets"); } catch (e) { status(e.message, "err"); }
    const list = f[btn.dataset.facet] || [];
    picker(btn, [{ v: "", label: btn.dataset.all }].concat(list.map((v) => ({ v, label: v }))), state[key] || "", (v) => {
      state[key] = v;
      sync();
      onChange();
    }, { mono: key === "model", placeholder: key === "model" ? "Filter models…" : "Filter services…" });
  });
  sync();
  return sync;
}

// ---- dialog ---------------------------------------------------------------------------------

function confirmDialog(title, body, okLabel, danger) {
  return new Promise((resolve) => {
    const modal = el("div", "modal");
    const d = el("div", "dialog");
    d.setAttribute("role", "dialog");
    const b = el("div", "d-body");
    b.appendChild(el("h3", "", title));
    b.appendChild(el("p", "", body));
    d.appendChild(b);
    const btns = el("div", "d-btns");
    const cancel = el("button", "text action", "Cancel");
    const ok = el("button", "text action " + (danger ? "danger" : "primary"), okLabel);
    btns.append(cancel, ok);
    d.appendChild(btns);
    modal.appendChild(d);
    const done = (v) => { modal.remove(); document.removeEventListener("keydown", key, true); resolve(v); };
    const key = (e) => {
      if (e.key === "Escape") { e.stopPropagation(); e.preventDefault(); done(false); }
      if (e.key === "Enter") { e.stopPropagation(); e.preventDefault(); done(true); }
    };
    document.addEventListener("keydown", key, true);
    cancel.addEventListener("click", () => done(false));
    ok.addEventListener("click", () => done(true));
    modal.addEventListener("mousedown", (e) => { if (e.target === modal) done(false); });
    document.body.appendChild(modal);
    ok.focus();
  });
}

// ---- KPI strips -------------------------------------------------------------------------------

function strip(node, blocks) {
  node.style.setProperty("--n", blocks.length);
  node.replaceChildren(...blocks.map((b) => {
    const blk = el("div", "blk");
    blk.appendChild(el("span", "k", b.k));
    blk.appendChild(el("span", "v" + (b.cls ? " " + b.cls : ""), b.v));
    if (b.sub) blk.appendChild(el("span", "sub", b.sub));
    if (b.title) blk.title = b.title;
    return blk;
  }));
}

function kindBadge(kind) {
  const labels = { llm: "LLM", embedding: "Embed", tool: "Tool", agent: "Agent", chain: "Chain", retriever: "Retr", evaluator: "Eval", guardrail: "Guard", event: "Event", span: "Span" };
  const b = el("span", "kind " + (kind || "span"), labels[kind] || "Span");
  return b;
}

// ---- views and routing ------------------------------------------------------------------------

const views = {}; // name → { el, load(params), refresh(), key(e) }
let view = "";
let params = {};

function registerView(name, impl) { views[name] = impl; }

function show(name, p, opts) {
  opts = opts || {};
  closePop();
  const prev = view;
  view = name;
  params = p || {};
  for (const k of Object.keys(views)) $("#view-" + k).hidden = k !== name;
  const tab = name === "trace" ? "traces" : name;
  $$("#tabs button").forEach((b) => b.classList.toggle("on", b.dataset.view === tab));
  slide($("#tabs"));
  $("#prefs").classList.toggle("on", name === "settings");
  const q = new URLSearchParams();
  if (name !== "traces") q.set("view", name);
  for (const [k, v] of Object.entries(params)) if (v) q.set(k, v);
  const url = location.pathname + (q.toString() ? "?" + q : "");
  if (opts.push && prev !== name) history.pushState(null, "", url);
  else history.replaceState(null, "", url);
  if (name !== "trace") native.setTitle("trae");
  views[name].load(params, prev);
}

function routeFromURL() {
  const q = new URLSearchParams(location.search);
  const name = q.get("view") || "traces";
  const p = {};
  for (const [k, v] of q) if (k !== "view" && k !== "theme") p[k] = v;
  show(views[name] ? name : "traces", p);
}

// ---- live updates ------------------------------------------------------------------------------

let seq = -1;
let pendingRefresh = false;
let liveOn = true;

function refreshView() {
  if (document.hidden || openPop || $(".modal")) { pendingRefresh = true; return; }
  pendingRefresh = false;
  const v = views[view];
  if (v && v.refresh) v.refresh();
}

// watch long-polls the change counter and refreshes the view shown.
async function watch() {
  for (;;) {
    try {
      const r = await api("changes?after=" + Math.max(seq, 0) + "&wait=1");
      if (seq >= 0 && r.seq !== seq) {
        seq = r.seq;
        if (liveOn) refreshView(); else onPaused();
      }
      seq = r.seq;
    } catch (e) {
      await sleep(2000);
    }
  }
}

let pausedNew = 0;
function onPaused() {
  pausedNew++;
  const b = $("#tr-live");
  let n = b.querySelector(".n");
  if (!n) { n = el("span", "n"); b.appendChild(n); }
  n.textContent = pausedNew > 9 ? "9+" : String(pausedNew);
}

function setLive(on) {
  liveOn = on;
  const b = $("#tr-live");
  b.classList.toggle("on", on);
  b.querySelector("span").textContent = on ? "Live" : "Paused";
  b.title = on ? "Pause live updates" : "Resume live updates";
  const n = b.querySelector(".n");
  if (n) n.remove();
  if (on && pausedNew) { pausedNew = 0; refreshView(); }
  pausedNew = 0;
}

document.addEventListener("visibilitychange", () => { if (!document.hidden && pendingRefresh) refreshView(); });

// ---- receiver status ------------------------------------------------------------------------------

let receiver = null;
async function pollStatus() {
  try {
    const st = await api("status");
    receiver = st.receiver;
    const r = $("#recv");
    r.classList.toggle("ok", !!st.receiver.listening);
    r.classList.toggle("bad", !st.receiver.listening);
    $("#recv-text").textContent = st.receiver.listening ? st.receiver.addr : "Not receiving";
    r.title = st.receiver.listening
      ? "Receiving OTLP/HTTP traces at " + st.receiver.endpoint + "\nClick to copy the endpoint"
      : st.receiver.error || "The OTLP receiver is not running";
    $("#foot-note").textContent = plural(st.stats.traces, "trace") + " · " + plural(st.stats.spans, "span");
  } catch (e) {
    $("#recv").classList.add("bad");
    $("#recv-text").textContent = "Offline";
  }
}

// ---- theme -----------------------------------------------------------------------------------------

function applyTheme(theme, animate) {
  const root = document.documentElement;
  if (animate) {
    root.classList.add("theming");
    setTimeout(() => root.classList.remove("theming"), 400);
  }
  if (theme === "light" || theme === "dark") root.dataset.theme = theme;
  else delete root.dataset.theme;
}

function applyTextSize(size) {
  document.documentElement.style.zoom = size && size !== 100 ? size / 100 : "";
  requestAnimationFrame(() => $$(".segs, .seg").forEach(slide));
}

// ---- keyboard ------------------------------------------------------------------------------------------

function typing(e) {
  const t = e.target;
  return t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable);
}

document.addEventListener("keydown", (e) => {
  const mod = e.metaKey || e.ctrlKey;
  if (e.key === "Escape") {
    if (openPop) { closePop(); e.preventDefault(); return; }
    if (typing(e) && e.target.value) { e.target.value = ""; e.target.dispatchEvent(new Event("input")); e.preventDefault(); return; }
    if (typing(e)) { e.target.blur(); return; }
  }
  if (mod && e.key === ",") { show("settings", {}, { push: true }); e.preventDefault(); return; }
  if ((mod && e.key.toLowerCase() === "k") || (e.key === "/" && !typing(e))) {
    const v = view === "trace" ? "traces" : view;
    if (v !== view) show(v);
    const q = $("#view-" + v + " .sess-filter");
    if (q) { q.focus(); q.select(); e.preventDefault(); }
    return;
  }
  if (mod && (e.key === "1" || e.key === "2")) { show(e.key === "1" ? "traces" : "calls", {}, { push: true }); e.preventDefault(); return; }
  const v = views[view];
  if (v && v.key && !typing(e) && !mod) v.key(e);
});

// List keyboard: ↑/↓ or j/k move between rows, Enter opens.
function listKeys(e, container) {
  const rows = $$("tr.row", container);
  if (!rows.length) return false;
  const i = rows.indexOf(document.activeElement);
  if (e.key === "ArrowDown" || e.key === "j") { (rows[i + 1] || rows[i < 0 ? 0 : i]).focus(); e.preventDefault(); return true; }
  if (e.key === "ArrowUp" || e.key === "k") { (rows[i - 1] || rows[0]).focus(); e.preventDefault(); return true; }
  return false;
}

// ---- start ---------------------------------------------------------------------------------------------

function start() {
  const body = document.body;
  body.classList.add(native.platform === "darwin" ? "mac" : native.platform === "win32" ? "win" : "linux");
  if (native.platform === "win32") document.documentElement.classList.add("win");
  body.classList.add(native.app ? "app" : "web");
  $("#prefs").appendChild(svg(GEAR, 15, 1.3));
  $("#prefs").addEventListener("click", () => show(view === "settings" ? "traces" : "settings", {}, { push: true }));
  $$("#tabs button").forEach((b) => b.addEventListener("click", () => show(b.dataset.view, {}, { push: true })));
  $("#recv").addEventListener("click", () => {
    if (receiver && receiver.listening) copy(receiver.endpoint, "Endpoint", null);
    else show("settings", {}, { push: true });
  });
  $("#tr-live").addEventListener("click", () => setLive(!liveOn));
  window.addEventListener("popstate", routeFromURL);
  routeFromURL();
  requestAnimationFrame(() => slide($("#tabs")));
  pollStatus();
  setInterval(() => { if (!document.hidden) pollStatus(); }, 5000);
  watch();
}
