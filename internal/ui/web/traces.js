// Traces: every trace received, newest first, live.
"use strict";

(() => {
  const page = $("#view-traces");
  const state = { q: "", service: "", model: "", status: "all", rows: [], next: 0, seenSeq: 0, loaded: false, loading: false };
  let syncService, syncModel, timer = 0, token = 0;

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

  // load fetches the first page; keep holds the rows already shown so a
  // live refresh only adds what is new.
  async function load(keep) {
    const my = ++token;
    if (!state.loaded && !keep) skeleton();
    let res;
    try {
      res = await api("traces?" + query());
    } catch (e) {
      status(e.message, "err");
      return;
    }
    if (my !== token) return;
    const prevSeq = state.seenSeq;
    const extra = keep && state.rows.length > res.traces.length ? state.rows.slice(res.traces.length) : [];
    state.rows = res.traces.concat(extra.filter((r) => !res.traces.some((t) => t.traceId === r.traceId)));
    if (!extra.length) state.next = res.next || 0;
    state.totals = res.totals;
    state.loaded = true;
    render(keep ? prevSeq : Infinity);
    state.seenSeq = res.seq;
  }

  async function older() {
    if (!state.next || state.loading) return;
    state.loading = true;
    try {
      const res = await api("traces?" + query(state.next));
      state.rows = state.rows.concat(res.traces);
      state.next = res.next || 0;
      render(Infinity);
    } catch (e) {
      status(e.message, "err");
    } finally {
      state.loading = false;
    }
  }

  function skeleton() {
    strip($("#tr-strip"), ["Traces", "LLM calls", "Tokens", "Cost", "Errors"].map((k) => ({ k, v: " " })));
    const wrap = el("div", "led-wrap");
    const t = el("table", "led");
    const tb = el("tbody");
    for (let i = 0; i < 8; i++) {
      const tr = el("tr");
      for (const w of [52, 220, 80, 120, 40, 40, 50]) {
        const td = el("td");
        const s = el("span", "skeleton");
        s.style.width = w + "px";
        td.appendChild(s);
        tr.appendChild(td);
      }
      tb.appendChild(tr);
    }
    t.appendChild(tb);
    wrap.appendChild(t);
    $("#tr-list").replaceChildren(wrap);
  }

  function filtered() { return state.q || state.service || state.model || state.status !== "all"; }

  function render(freshAfter) {
    const t = state.totals || {};
    strip($("#tr-strip"), [
      { k: "Traces", v: fmtInt(t.traces) },
      { k: "LLM calls", v: fmtInt(t.llmCalls) },
      { k: "Tokens", v: fmtTok((t.inTokens || 0) + (t.outTokens || 0)), sub: fmtTok(t.inTokens) + " in · " + fmtTok(t.outTokens) + " out" },
      { k: "Cost", v: fmtCost(t.cost), cls: t.cost ? "cost" : "" },
      { k: "Errors", v: fmtInt(t.errors), cls: t.errors ? "bad" : "", sub: t.traces ? Math.round((100 * (t.errors || 0)) / t.traces) + "% of traces" : "" },
    ]);
    $("#tr-sum").textContent = state.rows.length
      ? "Showing " + fmtInt(state.rows.length) + " of " + plural(t.traces || 0, "trace") + (filtered() ? " matching" : "")
      : "";
    $("#tr-pager").hidden = !state.next;
    if (!state.rows.length) {
      $("#tr-list").replaceChildren(filtered() ? noMatch() : waiting());
      return;
    }
    const wrap = el("div", "led-wrap");
    const table = el("table", "led");
    const head = el("thead");
    const hr = el("tr");
    for (const [label, cls] of [["Time"], ["Trace"], ["Service", "svc"], ["Spans", "n col-spans"], ["Models"], ["In", "n"], ["Out", "n"], ["Cost", "n"], ["Duration", "n"], ["", "st"]]) {
      hr.appendChild(el("th", cls || "", label));
    }
    head.appendChild(hr);
    table.appendChild(head);
    const tb = el("tbody");
    for (const r of state.rows) tb.appendChild(row(r, r.seq > freshAfter));
    table.appendChild(tb);
    wrap.appendChild(table);
    const focused = document.activeElement && document.activeElement.dataset ? document.activeElement.dataset.id : null;
    $("#tr-list").replaceChildren(wrap);
    if (focused) { const f = tb.querySelector('tr[data-id="' + focused + '"]'); if (f) f.focus({ preventScroll: true }); }
  }

  function row(r, fresh) {
    const tr = el("tr", "row" + (r.errors ? " bad" : "") + (fresh ? " fresh" : ""));
    tr.tabIndex = 0;
    tr.dataset.id = r.traceId;
    const time = el("td", "time", fmtTime(r.startNs));
    time.title = fmtFull(r.startNs);
    tr.appendChild(time);
    const nm = el("td", "nm");
    nm.appendChild(el("b", "", r.name || "(unnamed)"));
    if (r.preview) nm.appendChild(el("span", "pv", r.preview));
    nm.title = r.name + (r.preview ? "\n" + r.preview : "");
    tr.appendChild(nm);
    tr.appendChild(el("td", "svc", r.service));
    tr.appendChild(el("td", "n muted col-spans", fmtInt(r.spans)));
    const md = el("td", "mdl");
    if (r.models && r.models.length) {
      md.appendChild(document.createTextNode(r.models[0]));
      if (r.models.length > 1) md.appendChild(el("span", "more", "+" + (r.models.length - 1)));
      md.title = r.models.join("\n");
    } else {
      md.appendChild(el("span", "faint", "—"));
    }
    tr.appendChild(md);
    tr.appendChild(el("td", "n" + (r.inTokens ? "" : " faint"), r.inTokens ? fmtTok(r.inTokens) : "—"));
    tr.appendChild(el("td", "n" + (r.outTokens ? "" : " faint"), r.outTokens ? fmtTok(r.outTokens) : "—"));
    tr.appendChild(el("td", "n " + (r.cost ? "cost" : "faint"), fmtCost(r.cost)));
    const d = r.endNs - r.startNs;
    tr.appendChild(el("td", "n " + durClass(d), fmtDur(d)));
    const st = el("td", "st");
    st.appendChild(el("span", "dot"));
    st.title = r.errors ? plural(r.errors, "failed span") : "OK";
    tr.appendChild(st);
    const open = () => show("trace", { id: r.traceId }, { push: true });
    tr.addEventListener("click", () => { if (!String(getSelection())) open(); });
    tr.addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") { open(); e.preventDefault(); } });
    return tr;
  }

  function noMatch() {
    const box = el("div", "card empty-state");
    box.appendChild(el("b", "", "No traces match"));
    box.appendChild(el("p", "", "Try a different search or clear the filters."));
    const btns = el("div", "btns");
    const clear = el("button", "text action", "Clear filters");
    clear.addEventListener("click", () => {
      state.q = state.service = state.model = "";
      state.status = "all";
      $("#tr-q").value = "";
      syncService(); syncModel(); setStatus("all");
      load();
    });
    btns.appendChild(clear);
    box.appendChild(btns);
    return box;
  }

  function waiting() {
    const box = el("div", "card empty-state");
    box.appendChild(el("b", "", "Waiting for traces"));
    const ep = (receiver && receiver.endpoint) || "http://127.0.0.1:4318/v1/traces";
    box.appendChild(el("p", "", "Point any OpenTelemetry OTLP/HTTP exporter at " + ep + ". Spans show up here as they arrive."));
    const sn = exporterSnippet(ep);
    sn.classList.add("snippet");
    box.appendChild(sn);
    const btns = el("div", "btns");
    const setup = el("button", "text action", "Setup guide");
    setup.addEventListener("click", () => show("settings", {}, { push: true }));
    btns.appendChild(setup);
    box.appendChild(btns);
    return box;
  }

  let setStatus;
  function init() {
    const q = $("#tr-q");
    q.addEventListener("input", () => {
      clearTimeout(timer);
      timer = setTimeout(() => { state.q = q.value.trim(); load(); }, 180);
    });
    syncService = facetPicker($("#tr-service"), state, "service", () => load());
    syncModel = facetPicker($("#tr-model"), state, "model", () => load());
    setStatus = segs($("#tr-status"), "all", (v) => { state.status = v; load(); });
    $("#tr-older").addEventListener("click", older);
    page.addEventListener("scroll", () => {
      if (state.next && page.scrollTop + page.clientHeight > page.scrollHeight - 200) older();
    });
  }
  init();

  registerView("traces", {
    load(p, prev) {
      requestAnimationFrame(() => slide($("#tr-status")));
      if (prev === "trace" && state.loaded) { load(true); return; }
      load(state.loaded);
    },
    refresh() { load(true); },
    key(e) {
      if (listKeys(e, $("#tr-list"))) return;
    },
  });
})();
