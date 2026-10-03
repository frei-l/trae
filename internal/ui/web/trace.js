// One trace: its spans as a tree with a waterfall, and the selected span's
// detail beside it.
"use strict";

(() => {
  const state = { id: "", trace: null, sel: "", shut: new Set(), tab: localStorage.getItem("trae.tab") || "overview", from: "traces", flat: [] };
  const rowsBox = $("#tv-rows");
  const detail = $("#tv-detail");

  $("#tv-back").append(svg(BACK, 12, 1.8), document.createTextNode(" Traces"));
  $("#tv-del").appendChild(svg(TRASH, 14, 1.4));
  $("#tv-del").classList.add("danger");

  // back returns to the list the trace was opened from: through history
  // when it was pushed there, so the browser's back button agrees.
  function back() {
    if (state.pushed) { state.pushed = false; history.back(); }
    else show(state.from || "traces");
  }
  $("#tv-back").addEventListener("click", back);
  $("#tv-id").addEventListener("click", (e) => copy(state.id, "Trace ID", null));
  $("#tv-del").addEventListener("click", async () => {
    if (!state.trace) return;
    const ok = await confirmDialog("Delete this trace?", "“" + state.trace.summary.name + "” and its " + plural(state.trace.spans.length, "span") + " will be removed from this computer.", "Delete", true);
    if (!ok) return;
    try {
      await api("traces/" + encodeURIComponent(state.id) + "/delete", {});
      status("Trace deleted", "ok");
      state.id = "";
      show("traces");
    } catch (e) {
      status(e.message, "err");
    }
  });

  async function load(id, keep) {
    let tr;
    try {
      tr = await api("traces/" + encodeURIComponent(id));
    } catch (e) {
      if (/not found/.test(e.message)) {
        status("That trace is gone", "warn");
        show("traces");
      } else status(e.message, "err");
      return;
    }
    if (id !== state.id) return;
    const scroll = keep ? [rowsBox.scrollTop, (detail.querySelector(".db") || {}).scrollTop || 0] : null;
    state.trace = tr;
    if (!keep) {
      state.shut.clear();
      state.sel = pickDefault(tr.spans);
    } else if (!tr.spans.some((s) => s.spanId === state.sel)) {
      state.sel = pickDefault(tr.spans);
    }
    renderHead();
    renderTree();
    renderDetail();
    if (scroll) {
      rowsBox.scrollTop = scroll[0];
      const db = detail.querySelector(".db");
      if (db) db.scrollTop = scroll[1];
    }
  }

  function pickDefault(spans) {
    const want = params.span;
    if (want && spans.some((s) => s.spanId === want)) return want;
    const failed = spans.find((s) => s.status === "error" && s.ai.kind === "llm") || spans.find((s) => s.status === "error");
    if (failed) return failed.spanId;
    const llm = spans.find((s) => s.ai.kind === "llm");
    if (llm) return llm.spanId;
    return spans.length ? spans[0].spanId : "";
  }

  function renderHead() {
    const s = state.trace.summary;
    const spans = state.trace.spans;
    $("#tv-name").textContent = s.name || "(unnamed)";
    $("#tv-name").title = s.name;
    native.setTitle((s.name || "trace") + " — trae");
    const badges = $("#tv-badges");
    badges.replaceChildren();
    badges.appendChild(el("span", "tag", s.service));
    if (s.errors) badges.appendChild(el("span", "badge err", plural(s.errors, "error")));
    if (s.sessionId) {
      const t = el("span", "tag mono", s.sessionId);
      t.title = "Session";
      badges.appendChild(t);
    }
    $("#tv-id").textContent = s.traceId.slice(0, 12) + "…";
    $("#tv-id").title = "Copy trace ID " + s.traceId;
    const llms = spans.filter((x) => x.ai.kind === "llm");
    const ttfts = llms.map((x) => x.ai.ttftNs).filter(Boolean);
    strip($("#tv-strip"), [
      { k: "Duration", v: fmtDur(s.endNs - s.startNs), cls: durClass(s.endNs - s.startNs), title: fmtFull(s.startNs) },
      { k: "Spans", v: fmtInt(s.spans) },
      { k: "LLM calls", v: fmtInt(s.llmCalls), sub: ttfts.length ? "first token " + fmtDur(Math.min.apply(null, ttfts)) : "" },
      { k: "Tokens", v: fmtTok(s.inTokens + s.outTokens), sub: fmtTok(s.inTokens) + " in · " + fmtTok(s.outTokens) + " out" },
      { k: "Cost", v: fmtCost(s.cost), cls: s.cost ? "cost" : "" },
      { k: "Started", v: fmtTime(s.startNs), title: fmtFull(s.startNs) },
    ]);
  }

  // ---- tree and waterfall ---------------------------------------------------

  function tree() {
    const spans = state.trace.spans;
    const byId = new Map(spans.map((s) => [s.spanId, s]));
    const kids = new Map();
    const roots = [];
    for (const s of spans) {
      if (s.parentId && byId.has(s.parentId)) {
        if (!kids.has(s.parentId)) kids.set(s.parentId, []);
        kids.get(s.parentId).push(s);
      } else roots.push(s);
    }
    const byStart = (a, b) => a.startNs - b.startNs;
    roots.sort(byStart);
    kids.forEach((l) => l.sort(byStart));
    return { roots, kids };
  }

  function renderTree() {
    const { roots, kids } = tree();
    const t0 = Math.min.apply(null, state.trace.spans.map((x) => x.startNs));
    const t1 = Math.max.apply(null, state.trace.spans.map((x) => x.endNs));
    const total = Math.max(1, t1 - t0);
    const ticks = $("#tv-ticks");
    ticks.replaceChildren(...[0, 0.25, 0.5, 0.75, 1].map((f) => {
      const sp = el("span", "", f === 0 ? "0" : fmtDur(total * f));
      sp.style.left = f * 100 + "%";
      return sp;
    }));
    const flat = [];
    const walk = (sp, depth) => {
      flat.push({ sp, depth, hasKids: kids.has(sp.spanId) });
      if (state.shut.has(sp.spanId)) return;
      for (const k of kids.get(sp.spanId) || []) walk(k, depth + 1);
    };
    roots.forEach((r) => walk(r, 0));
    state.flat = flat;
    rowsBox.replaceChildren(...flat.map(({ sp, depth, hasKids }) => spanRow(sp, depth, hasKids, t0, total)));
  }

  function spanRow(sp, depth, hasKids, t0, total) {
    const failed = sp.status === "error" || sp.ai.level === "ERROR";
    const row = el("div", "sp" + (sp.spanId === state.sel ? " on" : "") + (failed ? " bad" : "") + (state.shut.has(sp.spanId) ? " shut" : ""));
    row.dataset.id = sp.spanId;
    const lhs = el("div", "lhs");
    lhs.style.setProperty("--d", depth);
    const tw = el("span", "tw" + (hasKids ? "" : " leaf"));
    tw.appendChild(svg(CHEV, 11, 1.8));
    tw.addEventListener("click", (e) => { e.stopPropagation(); toggle(sp.spanId); });
    lhs.appendChild(tw);
    lhs.appendChild(kindBadge(sp.ai.kind));
    lhs.appendChild(el("span", "nm", sp.name));
    if (sp.ai.model) lhs.appendChild(el("span", "md", sp.ai.model));
    const tok = (sp.ai.inTokens || 0) + (sp.ai.outTokens || 0);
    if (tok && sp.ai.kind === "llm") lhs.appendChild(el("span", "tk", fmtTok(tok) + " tok"));
    row.appendChild(lhs);
    const rhs = el("div", "rhs");
    const d = sp.endNs - sp.startNs;
    const left = ((sp.startNs - t0) / total) * 100;
    const width = Math.max(0.3, (d / total) * 100);
    const bar = el("div", "bar " + (sp.ai.kind || "span") + (failed ? " bad" : ""));
    bar.style.left = left + "%";
    bar.style.width = width + "%";
    if (sp.ai.ttftNs && d > 0) {
      const tt = el("i", "ttft");
      tt.style.width = Math.min(100, (sp.ai.ttftNs / d) * 100) + "%";
      tt.title = "Time to first token " + fmtDur(sp.ai.ttftNs);
      bar.appendChild(tt);
    }
    rhs.appendChild(bar);
    const dur = el("span", "dur", fmtDur(d));
    if (left + width < 74) dur.style.left = "calc(" + (left + width) + "% + 5px)";
    else dur.style.right = "calc(" + (100 - left) + "% + 5px)";
    rhs.appendChild(dur);
    row.appendChild(rhs);
    row.title = sp.name + " · " + fmtDur(d) + (failed && sp.statusMessage ? "\n" + sp.statusMessage : "");
    row.addEventListener("click", () => select(sp.spanId));
    row.addEventListener("dblclick", () => { if (hasKids) toggle(sp.spanId); });
    return row;
  }

  function toggle(id) {
    if (state.shut.has(id)) state.shut.delete(id); else state.shut.add(id);
    renderTree();
  }

  function select(id, scroll) {
    if (state.sel === id) return;
    state.sel = id;
    $$(".sp", rowsBox).forEach((r) => r.classList.toggle("on", r.dataset.id === id));
    params.span = id;
    const q = new URLSearchParams(location.search);
    q.set("span", id);
    history.replaceState(history.state, "", location.pathname + "?" + q);
    renderDetail();
    if (scroll) {
      const r = rowsBox.querySelector('.sp[data-id="' + id + '"]');
      if (r) r.scrollIntoView({ block: "nearest" });
    }
  }

  // ---- the selected span -------------------------------------------------------

  function renderDetail() {
    const sp = state.trace.spans.find((s) => s.spanId === state.sel);
    detail.replaceChildren();
    if (!sp) {
      const e = el("div", "empty-state");
      e.appendChild(el("b", "", "No span selected"));
      detail.appendChild(e);
      return;
    }
    const t0 = state.trace.summary.startNs;
    const head = el("div", "dh");
    const top = el("div", "dh-top");
    top.appendChild(kindBadge(sp.ai.kind));
    const h = el("h2", "", sp.name);
    h.title = sp.name;
    top.appendChild(h);
    top.appendChild(el("span", "grow"));
    top.appendChild(copyButton(sp.spanId, "Span ID", "Copy span ID"));
    head.appendChild(top);
    const sub = el("div", "dh-sub");
    const d = sp.endNs - sp.startNs;
    const bits = [["", fmtDur(d), durClass(d)], ["at ", "+" + fmtDur(sp.startNs - t0)]];
    if (sp.ai.model) bits.push(["", sp.ai.model]);
    if (sp.ai.inTokens || sp.ai.outTokens) bits.push(["", fmtInt(sp.ai.inTokens) + " → " + fmtInt(sp.ai.outTokens) + " tok"]);
    if (sp.ai.cost) bits.push(["", fmtCost(sp.ai.cost)]);
    for (const [pre, v, cls] of bits) {
      const s = el("span");
      if (pre) s.appendChild(document.createTextNode(pre));
      s.appendChild(el("b", cls || "", v));
      sub.appendChild(s);
    }
    head.appendChild(sub);
    const tabs = el("div", "dh-tabs");
    const nEvents = (sp.events || []).length;
    const nAttrs = Object.keys(sp.attributes || {}).length;
    tabs.appendChild(makeSegs([["overview", "Overview"], ["attributes", "Attributes " + nAttrs], ["events", "Events " + nEvents], ["raw", "Raw"]], state.tab, (v) => {
      state.tab = v;
      localStorage.setItem("trae.tab", v);
      body.replaceChildren();
      tabBody(sp, body);
      body.scrollTop = 0;
    }));
    head.appendChild(tabs);
    detail.appendChild(head);
    const body = el("div", "db scroll");
    detail.appendChild(body);
    tabBody(sp, body);
  }

  function tabBody(sp, body) {
    if (state.tab === "attributes") return attributes(sp, body);
    if (state.tab === "events") return events(sp, body);
    if (state.tab === "raw") {
      const s = section("Span JSON", "", copyButton(() => pretty(sp), "Span JSON"));
      s.appendChild(codeBox(sp, { noClamp: true }));
      body.appendChild(s);
      return;
    }
    overview(sp, body);
  }

  function overview(sp, body) {
    const err = errorBox(sp);
    if (err) body.appendChild(err);
    const ai = sp.ai || {};
    const dl = el("dl", "led-dl");
    const add = (k, v, cls) => {
      if (v === undefined || v === null || v === "") return;
      dl.appendChild(el("dt", "", k));
      const dd = el("dd", cls || "", v);
      dl.appendChild(dd);
    };
    add("Status", sp.status === "error" ? "Error" : sp.status === "ok" ? "OK" : "Unset", sp.status === "error" ? "bad" : "");
    add("Started", fmtFull(sp.startNs));
    if (ai.model) add("Model", ai.model, "mono");
    if (ai.provider) add("Provider", ai.provider);
    if (ai.inTokens || ai.outTokens) {
      let t = fmtInt(ai.inTokens) + " in · " + fmtInt(ai.outTokens) + " out";
      if (ai.cacheTokens) t += " · " + fmtInt(ai.cacheTokens) + " cached";
      add("Tokens", t);
    }
    if (ai.cost) add("Cost", fmtCost(ai.cost));
    if (ai.ttftNs) add("First token", fmtDur(ai.ttftNs));
    if (ai.tool && ai.tool.name) add("Tool", ai.tool.name + (ai.tool.callId ? "  (" + ai.tool.callId + ")" : ""), "mono");
    if (ai.sessionId) add("Session", ai.sessionId, "mono");
    if (ai.userId) add("User", ai.userId, "mono");
    if (ai.tags && ai.tags.length) add("Tags", ai.tags.join(", "));
    add("Service", sp.service);
    if (sp.scope) add("Scope", sp.scope, "mono");
    add("Span kind", sp.kind);
    if (ai.source) add("Read from", ai.source === "langfuse" ? "Langfuse attributes" : "OpenTelemetry GenAI attributes");
    // What was said comes first; the particulars follow.
    const talked = renderConversation(sp, body);
    const meta = section("Details");
    meta.appendChild(dl);
    body.appendChild(meta);
    if (ai.params && Object.keys(ai.params).length) {
      const ps = section("Parameters");
      ps.appendChild(kvTable(ai.params));
      body.appendChild(ps);
    }
    if (!talked) {
      const s = section("Attributes", Object.keys(sp.attributes || {}).length + "");
      const keys = Object.keys(sp.attributes || {});
      if (keys.length) s.appendChild(kvTable(sp.attributes));
      else s.appendChild(el("p", "cx-none", "This span has no attributes."));
      body.appendChild(s);
    }
  }

  function attributes(sp, body) {
    const f = el("input", "sess-filter attr-filter");
    f.placeholder = "Filter attributes…";
    f.type = "search";
    f.spellcheck = false;
    body.appendChild(f);
    const box = el("div");
    body.appendChild(box);
    const draw = () => {
      const q = f.value.trim().toLowerCase();
      box.replaceChildren();
      for (const [title, attrs] of [["Span", sp.attributes || {}], ["Resource", sp.resource || {}]]) {
        const keys = Object.keys(attrs).filter((k) => !q || k.toLowerCase().includes(q) || String(attrs[k]).toLowerCase().includes(q));
        const s = section(title, keys.length + "");
        if (keys.length) s.appendChild(kvTable(attrs, keys));
        else s.appendChild(el("p", "cx-none", q ? "No matches." : "None."));
        box.appendChild(s);
      }
    };
    f.addEventListener("input", draw);
    draw();
  }

  function kvTable(obj, keys) {
    const t = el("table", "kv");
    for (const k of (keys || Object.keys(obj)).sort()) {
      const tr = el("tr");
      tr.appendChild(el("td", "k", k));
      const td = el("td", "v");
      const v = obj[k];
      let text;
      if (typeof v === "string") {
        const parsed = tryJSON(v);
        if (parsed !== undefined) {
          text = pretty(parsed);
          const clip = el("span", "clip");
          clip.appendChild(highlight(text));
          td.appendChild(clip);
        } else {
          text = v;
          const clip = el("span", "clip", v);
          td.appendChild(clip);
        }
      } else if (typeof v === "number") {
        text = String(v);
        td.appendChild(el("span", "num", text));
      } else if (typeof v === "boolean") {
        text = String(v);
        td.appendChild(el("span", "bool", text));
      } else {
        text = pretty(v);
        const clip = el("span", "clip");
        clip.appendChild(highlight(text));
        td.appendChild(clip);
      }
      tr.appendChild(td);
      const c = el("td", "c");
      c.appendChild(copyButton(text, k));
      tr.appendChild(c);
      t.appendChild(tr);
    }
    return t;
  }

  function events(sp, body) {
    const evs = sp.events || [];
    if (!evs.length) {
      body.appendChild(el("p", "cx-none", "This span recorded no events."));
      return;
    }
    for (const e of evs) {
      const box = el("div", "evt");
      const h = el("div", "evt-h");
      h.appendChild(el("b", "", e.name));
      h.appendChild(el("span", "", "+" + fmtDur(e.timeNs - sp.startNs)));
      box.appendChild(h);
      if (Object.keys(e.attributes || {}).length) box.appendChild(kvTable(e.attributes));
      body.appendChild(box);
    }
  }

  // ---- keyboard -------------------------------------------------------------------

  function key(e) {
    if (e.key === "Escape") { back(); e.preventDefault(); return; }
    const i = state.flat.findIndex((f) => f.sp.spanId === state.sel);
    if (e.key === "ArrowDown" || e.key === "j") {
      const n = state.flat[Math.min(state.flat.length - 1, i + 1)];
      if (n) select(n.sp.spanId, true);
      e.preventDefault();
    } else if (e.key === "ArrowUp" || e.key === "k") {
      const n = state.flat[Math.max(0, i - 1)];
      if (n) select(n.sp.spanId, true);
      e.preventDefault();
    } else if (e.key === "ArrowLeft" || e.key === "h") {
      const cur = state.flat[i];
      if (!cur) return;
      if (cur.hasKids && !state.shut.has(cur.sp.spanId)) toggle(cur.sp.spanId);
      else if (cur.sp.parentId && state.flat.some((f) => f.sp.spanId === cur.sp.parentId)) select(cur.sp.parentId, true);
      e.preventDefault();
    } else if (e.key === "ArrowRight" || e.key === "l") {
      const cur = state.flat[i];
      if (cur && state.shut.has(cur.sp.spanId)) toggle(cur.sp.spanId);
      e.preventDefault();
    }
  }

  registerView("trace", {
    load(p, prev) {
      if (prev === "traces" || prev === "calls") state.from = prev;
      state.pushed = !!prev && prev !== "trace" && history.length > 1;
      if (!p.id) { show("traces"); return; }
      if (p.id !== state.id) {
        state.id = p.id;
        state.trace = null;
        rowsBox.replaceChildren();
        detail.replaceChildren();
        $("#tv-name").textContent = "";
        $("#tv-badges").replaceChildren();
        load(p.id, false);
      } else load(p.id, true);
    },
    refresh() { if (state.id) load(state.id, true); },
    key,
  });
})();
