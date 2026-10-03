// LLM Calls: every model call across traces, like Magpie's request ledger.
// A row opens in place to show what was sent and what came back.
"use strict";

(() => {
  const page = $("#view-calls");
  const state = { q: "", service: "", model: "", status: "all", rows: [], next: 0, open: new Set(), spans: new Map(), loaded: false, seenSeq: 0 };
  let timer = 0, token = 0, loading = false;
  const key = (c) => c.traceId + "/" + c.spanId;

  function query(before) {
    const q = new URLSearchParams();
    if (state.q) q.set("q", state.q);
    if (state.service) q.set("service", state.service);
    if (state.model) q.set("model", state.model);
    if (state.status === "error") q.set("status", "error");
    if (before) q.set("before", before);
    q.set("limit", "100");
    return q.toString();
  }

  async function load(keep) {
    const my = ++token;
    let res;
    try { res = await api("calls?" + query()); } catch (e) { status(e.message, "err"); return; }
    if (my !== token) return;
    const prevSeq = state.seenSeq;
    const extra = keep && state.rows.length > res.calls.length ? state.rows.slice(res.calls.length) : [];
    const seen = new Set(res.calls.map(key));
    state.rows = res.calls.concat(extra.filter((c) => !seen.has(key(c))));
    if (!extra.length) state.next = res.next || 0;
    state.totals = res.totals;
    state.loaded = true;
    render(keep ? prevSeq : -1, keep);
    state.seenSeq = res.seq;
  }

  async function older() {
    if (!state.next || loading) return;
    loading = true;
    try {
      const res = await api("calls?" + query(state.next));
      state.rows = state.rows.concat(res.calls);
      state.next = res.next || 0;
      render(-1, true);
    } catch (e) { status(e.message, "err"); } finally { loading = false; }
  }

  function render(prevSeq, keep) {
    const t = state.totals || {};
    strip($("#cl-strip"), [
      { k: "Calls", v: fmtInt(t.llmCalls), sub: plural(t.traces || 0, "trace") },
      { k: "Input tokens", v: fmtTok(t.inTokens) },
      { k: "Output tokens", v: fmtTok(t.outTokens) },
      { k: "Cost", v: fmtCost(t.cost), cls: t.cost ? "cost" : "" },
      { k: "Failed", v: fmtInt(t.errors), cls: t.errors ? "bad" : "", sub: t.llmCalls ? Math.round((100 * (t.errors || 0)) / t.llmCalls) + "% of calls" : "" },
    ]);
    $("#cl-sum").textContent = state.rows.length ? "Showing " + fmtInt(state.rows.length) + " of " + plural(t.llmCalls || 0, "call") : "";
    $("#cl-pager").hidden = !state.next;
    if (!state.rows.length) {
      const box = el("div", "card empty-state");
      box.appendChild(el("b", "", state.q || state.service || state.model || state.status !== "all" ? "No calls match" : "No model calls yet"));
      box.appendChild(el("p", "", "Spans read as model calls when they carry Langfuse generation attributes or OpenTelemetry gen_ai.* attributes."));
      $("#cl-list").replaceChildren(box);
      return;
    }
    // Remember inner scroll positions so a live redraw doesn't move the reader.
    const scrolls = new Map($$(".cx-t", $("#cl-list")).map((p, i) => [i, p.scrollTop]));
    const wrap = el("div", "led-wrap");
    const table = el("table", "led");
    const hr = el("tr");
    for (const [label, cls] of [["Time"], ["Model"], ["Call"], ["In", "n"], ["Out", "n"], ["Cached", "n"], ["Cost", "n"], ["TTFT", "n"], ["Duration", "n"], ["", "st"]]) hr.appendChild(el("th", cls || "", label));
    const thead = el("thead");
    thead.appendChild(hr);
    table.appendChild(thead);
    const tb = el("tbody");
    for (const c of state.rows) {
      const fresh = prevSeq >= 0 && c.startNs && !state._known?.has(key(c));
      tb.appendChild(row(c, fresh && keep));
      if (state.open.has(key(c))) tb.appendChild(detailRow(c));
    }
    state._known = new Set(state.rows.map(key));
    table.appendChild(tb);
    wrap.appendChild(table);
    $("#cl-list").replaceChildren(wrap);
    $$(".cx-t", $("#cl-list")).forEach((p, i) => { if (scrolls.get(i)) p.scrollTop = scrolls.get(i); });
  }

  function row(c, fresh) {
    const k = key(c);
    const tr = el("tr", "row" + (c.failed ? " bad" : "") + (state.open.has(k) ? " open" : "") + (fresh ? " fresh" : ""));
    tr.tabIndex = 0;
    tr.dataset.id = k;
    tr.setAttribute("aria-expanded", state.open.has(k));
    const time = el("td", "time", fmtTime(c.startNs));
    time.title = fmtFull(c.startNs);
    tr.appendChild(time);
    const md = el("td", "mdl", c.model || "—");
    md.title = [c.model, c.provider].filter(Boolean).join(" · ");
    tr.appendChild(md);
    const nm = el("td", "nm");
    nm.appendChild(el("b", "", c.traceName && c.traceName !== c.name ? c.traceName + " › " + c.name : c.name));
    if (c.preview) nm.appendChild(el("span", "pv", c.preview));
    nm.title = c.preview || c.name;
    tr.appendChild(nm);
    tr.appendChild(el("td", "n" + (c.inTokens ? "" : " faint"), c.inTokens ? fmtInt(c.inTokens) : "—"));
    tr.appendChild(el("td", "n" + (c.outTokens ? "" : " faint"), c.outTokens ? fmtInt(c.outTokens) : "—"));
    tr.appendChild(el("td", "n" + (c.cacheTokens ? " muted" : " faint"), c.cacheTokens ? fmtInt(c.cacheTokens) : "—"));
    tr.appendChild(el("td", "n " + (c.cost ? "cost" : "faint"), fmtCost(c.cost)));
    tr.appendChild(el("td", "n" + (c.ttftNs ? "" : " faint"), c.ttftNs ? fmtDur(c.ttftNs) : "—"));
    const d = c.endNs - c.startNs;
    tr.appendChild(el("td", "n " + durClass(d), fmtDur(d)));
    const st = el("td", "st");
    st.appendChild(el("span", "dot"));
    st.title = c.failed ? c.statusMessage || "Failed" : "OK";
    tr.appendChild(st);
    const toggle = () => {
      if (state.open.has(k)) state.open.delete(k); else state.open.add(k);
      render(-1, true);
      const again = $('#cl-list tr.row[data-id="' + CSS.escape(k) + '"]');
      if (again) again.focus({ preventScroll: true });
    };
    tr.addEventListener("click", () => { if (!String(getSelection())) toggle(); });
    tr.addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") { toggle(); e.preventDefault(); } });
    return tr;
  }

  function detailRow(c) {
    const tr = el("tr", "detail");
    const td = el("td");
    td.colSpan = 10;
    const box = el("div", "led-box");
    td.appendChild(box);
    tr.appendChild(td);
    const k = key(c);
    const sp = state.spans.get(k);
    if (!sp) {
      box.appendChild(el("span", "skeleton")).style.width = "40%";
      api("spans/" + encodeURIComponent(c.traceId) + "/" + encodeURIComponent(c.spanId)).then((s) => {
        state.spans.set(k, s);
        render(-1, true);
      }).catch((e) => status(e.message, "err"));
      return tr;
    }
    const err = errorBox(sp);
    if (err) box.appendChild(err);
    const dl = el("dl", "led-dl");
    const add = (k2, v, cls) => { if (!v) return; dl.appendChild(el("dt", "", k2)); dl.appendChild(el("dd", cls || "", v)); };
    add("Model", sp.ai.model, "mono");
    add("Provider", sp.ai.provider);
    add("Trace", c.traceName || c.traceId, "");
    add("Service", sp.service);
    if (sp.ai.params) add("Parameters", Object.entries(sp.ai.params).map(([a, b]) => a + "=" + (typeof b === "object" ? JSON.stringify(b) : b)).join("  "), "mono");
    add("Span ID", sp.spanId, "mono");
    const head = section("Call");
    const open = el("button", "link", "Open in trace →");
    open.addEventListener("click", (e) => { e.stopPropagation(); show("trace", { id: c.traceId, span: c.spanId }, { push: true }); });
    head.querySelector("h4").append(el("span", "grow"), open);
    head.appendChild(dl);
    box.appendChild(head);
    renderConversation(sp, box);
    return tr;
  }

  function init() {
    const q = $("#cl-q");
    q.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(() => { state.q = q.value.trim(); load(); }, 180); });
    facetPicker($("#cl-service"), state, "service", () => load());
    facetPicker($("#cl-model"), state, "model", () => load());
    segs($("#cl-status"), "all", (v) => { state.status = v; load(); });
    $("#cl-older").addEventListener("click", older);
    page.addEventListener("scroll", () => { if (state.next && page.scrollTop + page.clientHeight > page.scrollHeight - 200) older(); });
  }
  init();

  registerView("calls", {
    load() { requestAnimationFrame(() => slide($("#cl-status"))); load(state.loaded); },
    refresh() { load(true); },
    key(e) { listKeys(e, $("#cl-list")); },
  });
})();
