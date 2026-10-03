// Real OpenTelemetry JS exporters, protobuf and JSON, into a running trae,
// with spans shaped the way @langfuse/tracing and GenAI instrumentations
// write them.
const test = require("node:test");
const assert = require("node:assert/strict");
const api = require("@opentelemetry/api");
const { BasicTracerProvider, SimpleSpanProcessor } = require("@opentelemetry/sdk-trace-base");
const { resourceFromAttributes } = require("@opentelemetry/resources");
const { OTLPTraceExporter: ProtoExporter } = require("@opentelemetry/exporter-trace-otlp-proto");
const { OTLPTraceExporter: JSONExporter } = require("@opentelemetry/exporter-trace-otlp-http");
const { startTrae } = require("./helpers.cjs");

let trae;
test.before(async () => { trae = await startTrae(); });
test.after(() => trae.stop());

function provider(service, exporter) {
  return new BasicTracerProvider({
    resource: resourceFromAttributes({ "service.name": service }),
    spanProcessors: [new SimpleSpanProcessor(exporter)],
  });
}

test("protobuf exporter with Langfuse attributes", async () => {
  const p = provider("lingotutor-test", new ProtoExporter({ url: trae.endpoint }));
  const tracer = p.getTracer("@langfuse/tracing");
  const root = tracer.startSpan("explain-word", { attributes: {
    "langfuse.observation.type": "agent",
    "langfuse.trace.name": "explain-word",
    "langfuse.session.id": "sess-js",
  } });
  const ctx = api.trace.setSpan(api.context.active(), root);
  const gen = tracer.startSpan("generation", { attributes: {
    "langfuse.observation.type": "generation",
    "langfuse.observation.model.name": "claude-sonnet-5-5",
    "langfuse.observation.input": JSON.stringify({ systemPrompt: "Be a tutor.", messages: [{ role: "user", content: "Explain 'ubiquitous'." }] }),
    "langfuse.observation.output": JSON.stringify({ role: "assistant", content: [{ type: "text", text: "Found everywhere." }] }),
    "langfuse.observation.usage_details": JSON.stringify({ input: 120, output: 12 }),
    "langfuse.observation.cost_details": JSON.stringify({ total: 0.0012 }),
  } }, ctx);
  gen.end();
  root.end();
  await p.forceFlush();
  await p.shutdown();

  const page = await trae.api("traces?q=explain-word");
  assert.equal(page.traces.length, 1);
  const t = page.traces[0];
  assert.equal(t.service, "lingotutor-test");
  assert.equal(t.spans, 2);
  assert.equal(t.llmCalls, 1);
  assert.equal(t.inTokens, 120);
  assert.equal(t.outTokens, 12);
  assert.deepEqual(t.models, ["claude-sonnet-5-5"]);
  assert.equal(t.sessionId, "sess-js");
  const full = await trae.api("traces/" + t.traceId);
  const g = full.spans.find((s) => s.name === "generation");
  assert.equal(g.parentId, full.spans.find((s) => s.name === "explain-word").spanId);
  assert.deepEqual(g.ai.input.map((m) => m.role), ["system", "user"]);
  assert.equal(g.ai.output[0].parts[0].text, "Found everywhere.");
});

test("JSON exporter with gen_ai attributes and an error", async () => {
  const p = provider("py-like", new JSONExporter({ url: trae.endpoint }));
  const tracer = p.getTracer("genai");
  const span = tracer.startSpan("chat gpt-5", { kind: api.SpanKind.CLIENT, attributes: {
    "gen_ai.operation.name": "chat",
    "gen_ai.provider.name": "openai",
    "gen_ai.request.model": "gpt-5",
    "gen_ai.usage.input_tokens": 33,
    "gen_ai.usage.output_tokens": 0,
    "gen_ai.input.messages": JSON.stringify([{ role: "user", parts: [{ type: "text", content: "ping" }] }]),
  } });
  span.recordException(new Error("rate limited"));
  span.setStatus({ code: api.SpanStatusCode.ERROR, message: "429" });
  span.end();
  await p.forceFlush();
  await p.shutdown();

  const calls = await trae.api("calls?status=error&model=gpt-5");
  assert.equal(calls.calls.length, 1);
  const c = calls.calls[0];
  assert.equal(c.failed, true);
  assert.equal(c.statusMessage, "429");
  assert.equal(c.inTokens, 33);
  assert.equal(c.provider, "openai");
  assert.equal(c.service, "py-like");
  assert.equal(c.preview, "ping");
  const s = await trae.api("spans/" + c.traceId + "/" + c.spanId);
  assert.equal(s.events[0].name, "exception");
});
