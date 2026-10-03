// The conversation renderer: what a span sent and got back, as messages
// with role pills, tool calls and folded thinking.
"use strict";

const ROLE_LABEL = { user: "You", assistant: "Assistant", system: "System", tool: "Tool", input: "Input", output: "Output" };

function partText(p) {
  if (p.type === "tool_call") return p.args || "";
  return p.text || "";
}

function messageText(m) {
  return (m.parts || []).map((p) => {
    if (p.type === "tool_call") return "→ " + (p.name || "tool") + "(" + (p.args || "") + ")";
    return partText(p);
  }).join("\n\n");
}

// renderPart draws one part of a message under its header.
function renderPart(m, p, first) {
  const head = el("div", "cx-h");
  const role = m.role || "input";
  const copyText = () => (p.type === "tool_call" ? p.args || "" : p.text || "");
  if (p.type === "thinking") {
    const d = el("details", "cx-fold");
    const sum = el("summary");
    sum.appendChild(svg(CHEV, 11));
    sum.appendChild(el("span", "cx-role", "Thinking"));
    d.appendChild(sum);
    d.appendChild(codeBox(p.text, { json: false }));
    return d;
  }
  if (p.type === "tool_call") {
    head.appendChild(el("span", "cx-role call", "Tool call"));
    if (p.name) head.appendChild(el("span", "cx-name", p.name));
    if (p.id) head.appendChild(el("span", "cx-id", p.id));
  } else if (p.type === "tool_result") {
    head.appendChild(el("span", "cx-role tool", "Tool result"));
    if (p.name) head.appendChild(el("span", "cx-name", p.name));
    if (p.id) head.appendChild(el("span", "cx-id", p.id));
  } else if (first) {
    head.appendChild(el("span", "cx-role " + role, ROLE_LABEL[role] || role));
    if (m.name) head.appendChild(el("span", "cx-name", m.name));
  } else {
    head.classList.add("cont");
  }
  head.appendChild(el("span", "grow"));
  head.appendChild(copyButton(copyText, "Message"));
  const prose = p.type === "text" && tryJSON(p.text) === undefined;
  return codeBox(p.type === "tool_call" ? p.args || "{}" : p.text, { head, prose });
}

function renderMessages(msgs, box) {
  if (!msgs || !msgs.length) {
    box.appendChild(el("p", "cx-none", "Nothing recorded."));
    return;
  }
  for (const m of msgs) {
    const parts = m.parts && m.parts.length ? m.parts : [{ type: "text", text: "" }];
    // The role goes on the first part that doesn't carry a label of its own.
    let labeled = false;
    for (const p of parts) {
      const own = p.type === "thinking" || p.type === "tool_call" || p.type === "tool_result";
      box.appendChild(renderPart(m, p, !own && !labeled));
      if (!own) labeled = true;
    }
  }
}

function section(title, count, extra) {
  const s = el("div", "sec");
  const h = el("h4");
  h.appendChild(document.createTextNode(title));
  if (count) h.appendChild(el("span", "n", count));
  if (extra) { h.appendChild(el("span", "grow")); h.appendChild(extra); }
  s.appendChild(h);
  return s;
}

// renderConversation draws a span's input and output into box.
function renderConversation(span, box) {
  const ai = span.ai || {};
  const chat = ai.kind === "llm" || ai.kind === "embedding";
  const inTitle = chat ? "Input" : ai.kind === "tool" ? "Arguments" : "Input";
  const outTitle = chat ? "Output" : ai.kind === "tool" ? "Result" : "Output";
  const any = (ai.input && ai.input.length) || (ai.output && ai.output.length);
  if (!any) return false;
  const tokIn = ai.inTokens ? fmtInt(ai.inTokens) + " tokens" : "";
  const tokOut = ai.outTokens ? fmtInt(ai.outTokens) + " tokens" : "";
  const inSec = section(inTitle, [ai.input && ai.input.length > 1 ? ai.input.length + " messages" : "", tokIn].filter(Boolean).join(" · "),
    copyButton(() => (ai.input || []).map(messageText).join("\n\n"), inTitle, "Copy " + inTitle.toLowerCase()));
  renderMessages(ai.input, inSec);
  box.appendChild(inSec);
  const outSec = section(outTitle, tokOut,
    copyButton(() => (ai.output || []).map(messageText).join("\n\n"), outTitle, "Copy " + outTitle.toLowerCase()));
  renderMessages(ai.output, outSec);
  box.appendChild(outSec);
  return true;
}

// errorBox shows why a span failed: its status and any exception events.
function errorBox(span) {
  const failed = span.status === "error" || (span.ai && span.ai.level === "ERROR");
  if (!failed) return null;
  const box = el("div", "err-box");
  box.appendChild(el("b", "", span.statusMessage || "Error"));
  for (const e of span.events || []) {
    if (e.name !== "exception") continue;
    const a = e.attributes || {};
    if (a["exception.type"] || a["exception.message"]) box.appendChild(el("div", "", [a["exception.type"], a["exception.message"]].filter(Boolean).join(": ")));
    if (a["exception.stacktrace"]) box.appendChild(el("pre", "", a["exception.stacktrace"]));
  }
  return box;
}
