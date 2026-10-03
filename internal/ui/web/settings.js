// Settings: where to send traces, how trae looks, how long it keeps them.
"use strict";

const SNIPPETS = {
  node: {
    label: "Node",
    lang: "js",
    code: (ep) => `import { NodeSDK } from "@opentelemetry/sdk-node";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-proto";
import { BatchSpanProcessor } from "@opentelemetry/sdk-trace-base";

const sdk = new NodeSDK({
  spanProcessors: [
    new BatchSpanProcessor(
      new OTLPTraceExporter({ url: "${ep}" }),
      { scheduledDelayMillis: 500 },
    ),
  ],
});
sdk.start();`,
  },
  langfuse: {
    label: "Langfuse SDK",
    lang: "js",
    code: (ep) => `// Already tracing with @langfuse/tracing? Keep every span as it is and
// add trae next to (or instead of) the Langfuse processor.
import { LangfuseSpanProcessor } from "@langfuse/otel";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-proto";
import { BatchSpanProcessor } from "@opentelemetry/sdk-trace-base";

const sdk = new NodeSDK({
  spanProcessors: [
    new LangfuseSpanProcessor(), // optional
    new BatchSpanProcessor(new OTLPTraceExporter({ url: "${ep}" })),
  ],
});`,
  },
  python: {
    label: "Python",
    lang: "py",
    code: (ep) => `from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

provider = TracerProvider()
provider.add_span_processor(
    BatchSpanProcessor(OTLPSpanExporter(endpoint="${ep}"))
)
trace.set_tracer_provider(provider)`,
  },
  env: {
    label: "Env vars",
    lang: "sh",
    code: (ep) => `# Any OpenTelemetry SDK that reads the standard variables
export OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=${ep}
export OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf`,
  },
};

const CODE_TOKENS = /(\/\/[^\n]*|#[^\n]*)|("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`[^`]*`)|\b(import|from|const|new|export|true|false|None|def|return)\b|\b(\d+)\b/g;

function codeHighlight(text, lang) {
  const frag = document.createDocumentFragment();
  let last = 0, m;
  CODE_TOKENS.lastIndex = 0;
  while ((m = CODE_TOKENS.exec(text))) {
    if (m[1] && lang === "js" && m[1][0] === "#") continue;
    if (m[1] && lang !== "js" && m[1][0] === "/") continue;
    if (m.index > last) frag.appendChild(document.createTextNode(text.slice(last, m.index)));
    const cls = m[1] ? "tk-c" : m[2] ? "tk-s" : m[3] ? "tk-k" : "tk-n";
    frag.appendChild(el("span", cls, m[0]));
    last = CODE_TOKENS.lastIndex;
  }
  if (last < text.length) frag.appendChild(document.createTextNode(text.slice(last)));
  return frag;
}

// exporterSnippet shows how to send traces to ep, one tab per SDK.
function exporterSnippet(ep) {
  const box = el("div", "snippet");
  const head = el("div", "sn-head");
  const pre = el("pre", "scroll");
  let cur = localStorage.getItem("trae.snippet") || "node";
  if (!SNIPPETS[cur]) cur = "node";
  const draw = () => pre.replaceChildren(codeHighlight(SNIPPETS[cur].code(ep), SNIPPETS[cur].lang));
  head.appendChild(makeSegs(Object.entries(SNIPPETS).map(([k, v]) => [k, v.label]), cur, (v) => {
    cur = v;
    localStorage.setItem("trae.snippet", v);
    draw();
  }));
  head.appendChild(el("span", "grow"));
  head.appendChild(copyButton(() => SNIPPETS[cur].code(ep), "Snippet", "Copy snippet"));
  box.append(head, pre);
  draw();
  return box;
}

(() => {
  const page = $("#view-settings");

  function row(name, sub, control) {
    const r = el("div", "pref");
    const w = el("div", "words");
    w.appendChild(el("div", "name", name));
    if (sub) {
      const s = el("div", "sub");
      if (typeof sub === "string") s.textContent = sub; else s.appendChild(sub);
      w.appendChild(s);
    }
    r.appendChild(w);
    if (control) r.appendChild(control);
    return r;
  }

  function head(text) {
    const h = el("div", "row-head");
    h.appendChild(el("span", "label", text));
    return h;
  }

  function bytes(n) {
    if (n < 1024) return n + " B";
    if (n < 1024 * 1024) return (n / 1024).toFixed(0) + " KB";
    return (n / 1024 / 1024).toFixed(1) + " MB";
  }

  async function save(patch) {
    try {
      const p = await api("settings", patch);
      Object.assign(prefs, p);
      return p;
    } catch (e) {
      status(e.message, "err");
      return null;
    }
  }

  async function load() {
    let st, p;
    try {
      [st, p] = await Promise.all([api("status"), api("settings")]);
    } catch (e) {
      status(e.message, "err");
      return;
    }
    Object.assign(prefs, p);
    const r = st.receiver;
    const nodes = [];

    nodes.push(head("Receiver"));
    const recv = el("div", "list");
    const epCtl = el("div", "ctl");
    const code = el("code", "", r.endpoint);
    epCtl.append(code, copyButton(r.endpoint, "Endpoint"));
    const sub = el("span");
    if (r.listening) {
      sub.appendChild(document.createTextNode("Listening for OTLP/HTTP (protobuf or JSON). Langfuse and gen_ai.* attributes are read as model calls."));
    } else {
      sub.appendChild(el("span", "", r.error || "Not listening."));
      sub.style.color = "var(--red)";
    }
    recv.appendChild(row("OTLP/HTTP endpoint", sub, epCtl));
    const sn = el("div", "pref block");
    sn.appendChild(exporterSnippet(r.endpoint));
    recv.appendChild(sn);
    const sample = el("button", "text action", "Send samples");
    sample.addEventListener("click", async () => {
      sample.disabled = true;
      try {
        await api("demo", {});
        status("Sample traces sent", "ok");
      } catch (e) {
        status(e.message, "err");
      } finally {
        sample.disabled = false;
      }
    });
    recv.appendChild(row("Sample traces", "Send a few realistic agent, tool, RAG and failing traces to try trae out.", sample));
    nodes.push(recv);

    nodes.push(head("Appearance"));
    const look = el("div", "list");
    look.appendChild(row("Theme", "", makeSegs([["system", "System"], ["light", "Light"], ["dark", "Dark"]], prefs.theme, async (v) => {
      applyTheme(v, true);
      await save({ theme: v });
    })));
    look.appendChild(row("Text size", "", makeSegs([[100, "100%"], [110, "110%"], [125, "125%"], [150, "150%"]], prefs.textSize, async (v) => {
      applyTextSize(Number(v));
      await save({ textSize: Number(v) });
    })));
    nodes.push(look);

    nodes.push(head("Data"));
    const data = el("div", "list");
    data.appendChild(row("Keep traces for", "Older traces are removed every hour.", makeSegs([[1, "1 day"], [7, "7 days"], [30, "30 days"], [0, "Forever"]], prefs.retentionDays, async (v) => {
      const p2 = await save({ retentionDays: Number(v) });
      if (p2) status(Number(v) ? "Keeping traces for " + v + (v === "1" ? " day" : " days") : "Keeping traces forever", "ok");
    })));
    const where = el("span");
    where.appendChild(document.createTextNode(plural(st.stats.traces, "trace") + " · " + plural(st.stats.spans, "span") + " · " + bytes(st.stats.bytes) + " in "));
    where.appendChild(el("code", "", st.database));
    data.appendChild(row("Storage", where, copyButton(st.database, "Path", "Copy database path")));
    const clear = el("button", "text action danger", "Clear all…");
    clear.addEventListener("click", async () => {
      const ok = await confirmDialog("Clear all traces?", "Every trace and span stored on this computer will be deleted. This can't be undone.", "Clear all", true);
      if (!ok) return;
      try {
        await api("clear", {});
        status("All traces cleared", "ok");
        load();
        pollStatus();
      } catch (e) {
        status(e.message, "err");
      }
    });
    data.appendChild(row("Clear traces", "Remove everything trae has received.", clear));
    nodes.push(data);

    nodes.push(head("About"));
    const about = el("div", "list");
    about.appendChild(row("trae " + st.version, "A local trace viewer for AI apps. Built with MyGo; nothing leaves this computer.", null));
    const keys = el("span", "", "/ or ⌘K search · ⌘1 Traces · ⌘2 LLM Calls · ⌘, Settings · ↑↓ move · ←→ fold spans · Esc back");
    about.appendChild(row("Keyboard", keys, null));
    nodes.push(about);

    page.replaceChildren(...nodes);
    requestAnimationFrame(() => $$(".segs", page).forEach(slide));
  }

  registerView("settings", { load, refresh() {}, key(e) { if (e.key === "Escape") show("traces"); } });
})();
