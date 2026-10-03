// Package demo sends realistic sample AI traces to an OTLP/HTTP endpoint,
// in both Langfuse and OpenTelemetry GenAI flavors.
package demo

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// Run sends count rounds of traces, then, with every > 0, one more round
// every interval until interrupted.
func Run(endpoint string, count int, every time.Duration) error {
	for i := 0; i < count; i++ {
		if err := Send(endpoint, Round(time.Now().Add(-time.Duration(count-i)*90*time.Second))); err != nil {
			return err
		}
	}
	fmt.Printf("sent %d round(s) of sample traces to %s\n", count, endpoint)
	if every <= 0 {
		return nil
	}
	for range time.Tick(every) {
		if err := Send(endpoint, Round(time.Now())); err != nil {
			return err
		}
		fmt.Println("sent a round of sample traces")
	}
	return nil
}

// Send posts one protobuf export request.
func Send(endpoint string, data *tracepb.TracesData) error {
	body, err := proto.Marshal(data)
	if err != nil {
		return err
	}
	resp, err := http.Post(endpoint, "application/x-protobuf", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(b))
	}
	return nil
}

// Round builds one set of sample traces starting around t.
func Round(t time.Time) *tracepb.TracesData {
	return &tracepb.TracesData{ResourceSpans: []*tracepb.ResourceSpans{
		resource("lingotutor", "@langfuse/tracing", langfuseTutor(t), langfuseWriting(t.Add(20*time.Second))),
		resource("support-agent", "openai-agents", genaiAgent(t.Add(35*time.Second)), genaiFailed(t.Add(50*time.Second))),
		resource("docs-search", "opentelemetry-instrumentation-openai", ragTrace(t.Add(65*time.Second))),
	}}
}

type b struct {
	trace []byte
	spans []*tracepb.Span
}

func newTrace() *b { return &b{trace: id(16)} }

func (t *b) span(parent *tracepb.Span, name string, kind tracepb.Span_SpanKind, start time.Time, d time.Duration, attrs ...*commonpb.KeyValue) *tracepb.Span {
	s := &tracepb.Span{
		TraceId: t.trace, SpanId: id(8), Name: name, Kind: kind,
		StartTimeUnixNano: uint64(start.UnixNano()), EndTimeUnixNano: uint64(start.Add(d).UnixNano()),
		Attributes: attrs,
	}
	if parent != nil {
		s.ParentSpanId = parent.SpanId
	}
	t.spans = append(t.spans, s)
	return s
}

func resource(service, scope string, traces ...*b) *tracepb.ResourceSpans {
	var spans []*tracepb.Span
	for _, t := range traces {
		spans = append(spans, t.spans...)
	}
	return &tracepb.ResourceSpans{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
			kv("service.name", service), kv("service.version", "1.4.2"),
			kv("telemetry.sdk.language", "nodejs"), kv("telemetry.sdk.name", "opentelemetry"),
		}},
		ScopeSpans: []*tracepb.ScopeSpans{{Scope: &commonpb.InstrumentationScope{Name: scope}, Spans: spans}},
	}
}

const tutorSystem = "You are LingoTutor, a patient English tutor. Explain words for an intermediate learner: give the meaning in context, two example sentences and common collocations. Use the dictionary tool when unsure."

// langfuseTutor is a Langfuse-instrumented agent with a tool call between
// two generations, the way @langfuse/tracing records them.
func langfuseTutor(t time.Time) *b {
	tr := newTrace()
	q := "What does \"serendipity\" mean in: \"Finding that café was pure serendipity\"?"
	root := tr.span(nil, "explain-word", tracepb.Span_SPAN_KIND_INTERNAL, t, 6400*time.Millisecond,
		kv("langfuse.observation.type", "agent"),
		kv("langfuse.trace.name", "explain-word"),
		kv("langfuse.session.id", "sess_7f3a21"),
		kv("langfuse.user.id", "user_42"),
		kv("langfuse.trace.tags", `["passage","explain"]`),
		kv("langfuse.observation.input", js(map[string]any{"word": "serendipity", "sentence": "Finding that café was pure serendipity"})),
		kv("langfuse.observation.output", js(map[string]any{"meaning": "a happy accident: finding something good without looking for it", "examples": 2})),
	)
	tr.span(root, "generation", tracepb.Span_SPAN_KIND_CLIENT, t.Add(40*time.Millisecond), 1900*time.Millisecond,
		kv("langfuse.observation.type", "generation"),
		kv("langfuse.observation.model.name", "claude-sonnet-5-5"),
		kv("langfuse.observation.model.parameters", `{"temperature":0.3,"maxTokens":1024}`),
		kv("langfuse.observation.completion_start_time", js(t.Add(740*time.Millisecond).UTC().Format(time.RFC3339Nano))),
		kv("langfuse.observation.input", js(map[string]any{
			"systemPrompt": tutorSystem,
			"messages":     []any{map[string]any{"role": "user", "content": q}},
			"tools":        []any{map[string]any{"name": "lookup_dictionary", "description": "Look a word up in the learner's dictionary"}},
		})),
		kv("langfuse.observation.output", js(map[string]any{
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "thinking", "thinking": "The learner wants the in-context meaning. I should confirm the register and etymology with the dictionary before answering."},
				map[string]any{"type": "text", "text": "Let me check the dictionary entry first."},
				map[string]any{"type": "toolCall", "id": "toolu_01", "name": "lookup_dictionary", "arguments": map[string]any{"word": "serendipity", "level": "B2"}},
			},
			"stopReason": "toolUse",
		})),
		kv("langfuse.observation.usage_details", `{"input":812,"output":96,"cacheRead":512,"total":908}`),
		kv("langfuse.observation.cost_details", `{"input":0.002436,"output":0.00144,"total":0.003876}`),
	)
	tr.span(root, "lookup_dictionary", tracepb.Span_SPAN_KIND_INTERNAL, t.Add(1960*time.Millisecond), 310*time.Millisecond,
		kv("langfuse.observation.type", "tool"),
		kv("langfuse.observation.input", `{"word":"serendipity","level":"B2"}`),
		kv("langfuse.observation.output", js(map[string]any{"headword": "serendipity", "pos": "noun", "definition": "the fact of something interesting or pleasant happening by chance", "register": "formal / literary", "origin": "coined by Horace Walpole, 1754"})),
	)
	tr.span(root, "generation", tracepb.Span_SPAN_KIND_CLIENT, t.Add(2300*time.Millisecond), 4050*time.Millisecond,
		kv("langfuse.observation.type", "generation"),
		kv("langfuse.observation.model.name", "claude-sonnet-5-5"),
		kv("langfuse.observation.completion_start_time", js(t.Add(2900*time.Millisecond).UTC().Format(time.RFC3339Nano))),
		kv("langfuse.observation.input", js(map[string]any{
			"systemPrompt": tutorSystem,
			"messages": []any{
				map[string]any{"role": "user", "content": q},
				map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "toolCall", "id": "toolu_01", "name": "lookup_dictionary", "arguments": map[string]any{"word": "serendipity", "level": "B2"}}}},
				map[string]any{"role": "toolResult", "toolCallId": "toolu_01", "toolName": "lookup_dictionary", "content": []any{map[string]any{"type": "text", "text": "serendipity (noun, formal): the fact of something interesting or pleasant happening by chance."}}},
			},
		})),
		kv("langfuse.observation.output", js(map[string]any{
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": "**Serendipity** means a lucky, happy accident — finding something good when you weren't looking for it.\n\nIn your sentence, the speaker didn't plan to find the café; they came across it by chance, and it turned out to be wonderful.\n\nExamples:\n1. Meeting my business partner on a delayed train was pure serendipity.\n2. Many scientific discoveries owe something to serendipity.\n\nCollocations: *pure serendipity*, *a stroke of serendipity*, *by serendipity*."}},
		})),
		kv("langfuse.observation.usage_details", `{"input":1040,"output":214,"cacheRead":768,"total":1254}`),
		kv("langfuse.observation.cost_details", `{"total":0.00633}`),
	)
	return tr
}

func langfuseWriting(t time.Time) *b {
	tr := newTrace()
	root := tr.span(nil, "writing-score", tracepb.Span_SPAN_KIND_INTERNAL, t, 3100*time.Millisecond,
		kv("langfuse.observation.type", "chain"),
		kv("langfuse.trace.name", "writing-score"),
		kv("langfuse.session.id", "sess_7f3a21"),
		kv("langfuse.observation.input", "Yesterday I go to the market and buyed some apple."),
	)
	tr.span(root, "embed-essay", tracepb.Span_SPAN_KIND_CLIENT, t.Add(30*time.Millisecond), 180*time.Millisecond,
		kv("langfuse.observation.type", "embedding"),
		kv("langfuse.observation.model.name", "text-embedding-3-small"),
		kv("langfuse.observation.input", "Yesterday I go to the market and buyed some apple."),
		kv("langfuse.observation.usage_details", `{"input":14}`),
	)
	tr.span(root, "score", tracepb.Span_SPAN_KIND_CLIENT, t.Add(240*time.Millisecond), 2800*time.Millisecond,
		kv("langfuse.observation.type", "generation"),
		kv("langfuse.observation.model.name", "gpt-5-mini"),
		kv("langfuse.observation.input", js([]any{
			map[string]any{"role": "system", "content": "Score the learner's sentence from 0-10 and list grammar fixes as JSON."},
			map[string]any{"role": "user", "content": "Yesterday I go to the market and buyed some apple."},
		})),
		kv("langfuse.observation.output", js(map[string]any{"role": "assistant", "content": `{"score":5,"fixes":["go → went","buyed → bought","apple → apples"]}`})),
		kv("langfuse.observation.usage_details", `{"input":58,"output":31}`),
		kv("langfuse.observation.cost_details", `{"total":0.000077}`),
	)
	return tr
}

// genaiAgent follows the current OTel GenAI conventions.
func genaiAgent(t time.Time) *b {
	tr := newTrace()
	root := tr.span(nil, "invoke_agent refund-helper", tracepb.Span_SPAN_KIND_INTERNAL, t, 5200*time.Millisecond,
		kv("gen_ai.operation.name", "invoke_agent"),
		kv("gen_ai.agent.name", "refund-helper"),
		kv("gen_ai.provider.name", "openai"),
		kv("gen_ai.conversation.id", "conv_91c"),
	)
	sys := `[{"type":"text","content":"You help customers with refunds. Always check the order before promising anything."}]`
	tr.span(root, "chat gpt-5", tracepb.Span_SPAN_KIND_CLIENT, t.Add(20*time.Millisecond), 1700*time.Millisecond,
		kv("gen_ai.operation.name", "chat"),
		kv("gen_ai.provider.name", "openai"),
		kv("gen_ai.request.model", "gpt-5"),
		kv("gen_ai.response.model", "gpt-5-2026-08-01"),
		kv("gen_ai.request.temperature", 0.2),
		kv("gen_ai.usage.input_tokens", int64(421)),
		kv("gen_ai.usage.output_tokens", int64(38)),
		kv("gen_ai.response.finish_reasons", `["tool_calls"]`),
		kv("gen_ai.system_instructions", sys),
		kv("gen_ai.input.messages", `[{"role":"user","parts":[{"type":"text","content":"I was charged twice for order #A-1043, can I get one refunded?"}]}]`),
		kv("gen_ai.output.messages", `[{"role":"assistant","parts":[{"type":"tool_call","id":"call_1","name":"get_order","arguments":{"order_id":"A-1043"}}],"finish_reason":"tool_calls"}]`),
	)
	tr.span(root, "execute_tool get_order", tracepb.Span_SPAN_KIND_INTERNAL, t.Add(1750*time.Millisecond), 420*time.Millisecond,
		kv("gen_ai.operation.name", "execute_tool"),
		kv("gen_ai.tool.name", "get_order"),
		kv("gen_ai.tool.call.id", "call_1"),
		kv("gen_ai.tool.call.arguments", `{"order_id":"A-1043"}`),
		kv("gen_ai.tool.call.result", `{"order_id":"A-1043","charges":[{"id":"ch_1","amount":49.0},{"id":"ch_2","amount":49.0}],"status":"delivered"}`),
	)
	tr.span(root, "chat gpt-5", tracepb.Span_SPAN_KIND_CLIENT, t.Add(2200*time.Millisecond), 2950*time.Millisecond,
		kv("gen_ai.operation.name", "chat"),
		kv("gen_ai.provider.name", "openai"),
		kv("gen_ai.request.model", "gpt-5"),
		kv("gen_ai.response.model", "gpt-5-2026-08-01"),
		kv("gen_ai.usage.input_tokens", int64(530)),
		kv("gen_ai.usage.output_tokens", int64(87)),
		kv("gen_ai.system_instructions", sys),
		kv("gen_ai.input.messages", `[{"role":"user","parts":[{"type":"text","content":"I was charged twice for order #A-1043, can I get one refunded?"}]},{"role":"assistant","parts":[{"type":"tool_call","id":"call_1","name":"get_order","arguments":{"order_id":"A-1043"}}]},{"role":"tool","parts":[{"type":"tool_call_response","id":"call_1","response":{"charges":2,"status":"delivered"}}]}]`),
		kv("gen_ai.output.messages", `[{"role":"assistant","parts":[{"type":"text","content":"I can see two charges of $49.00 on order A-1043. I've flagged the duplicate (ch_2) for a refund — you'll see it back on your card in 3–5 business days."}],"finish_reason":"stop"}]`),
	)
	return tr
}

func genaiFailed(t time.Time) *b {
	tr := newTrace()
	root := tr.span(nil, "invoke_agent refund-helper", tracepb.Span_SPAN_KIND_INTERNAL, t, 1300*time.Millisecond,
		kv("gen_ai.operation.name", "invoke_agent"),
		kv("gen_ai.agent.name", "refund-helper"),
	)
	root.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: "upstream model error"}
	c := tr.span(root, "chat gpt-5", tracepb.Span_SPAN_KIND_CLIENT, t.Add(15*time.Millisecond), 1250*time.Millisecond,
		kv("gen_ai.operation.name", "chat"),
		kv("gen_ai.provider.name", "openai"),
		kv("gen_ai.request.model", "gpt-5"),
		kv("error.type", "rate_limit_exceeded"),
		kv("gen_ai.input.messages", `[{"role":"user","parts":[{"type":"text","content":"Where is my parcel?"}]}]`),
	)
	c.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: "429 Too Many Requests: rate limit exceeded for gpt-5"}
	c.Events = []*tracepb.Span_Event{{
		Name: "exception", TimeUnixNano: uint64(t.Add(1260 * time.Millisecond).UnixNano()),
		Attributes: []*commonpb.KeyValue{kv("exception.type", "RateLimitError"), kv("exception.message", "429 Too Many Requests"), kv("exception.stacktrace", "RateLimitError: 429 Too Many Requests\n    at OpenAI.makeRequest (node_modules/openai/core.js:412:15)\n    at async Agent.run (src/agent.ts:88:20)")},
	}}
	return tr
}

// ragTrace is an HTTP request with retrieval and a legacy-attribute chat.
func ragTrace(t time.Time) *b {
	tr := newTrace()
	root := tr.span(nil, "POST /api/ask", tracepb.Span_SPAN_KIND_SERVER, t, 2600*time.Millisecond,
		kv("http.request.method", "POST"), kv("http.route", "/api/ask"), kv("http.response.status_code", int64(200)),
		kv("url.path", "/api/ask"), kv("user_agent.original", "Mozilla/5.0"),
	)
	tr.span(root, "embeddings text-embedding-3-small", tracepb.Span_SPAN_KIND_CLIENT, t.Add(10*time.Millisecond), 140*time.Millisecond,
		kv("gen_ai.operation.name", "embeddings"),
		kv("gen_ai.system", "openai"),
		kv("gen_ai.request.model", "text-embedding-3-small"),
		kv("gen_ai.usage.input_tokens", int64(11)),
	)
	tr.span(root, "SELECT documents", tracepb.Span_SPAN_KIND_CLIENT, t.Add(160*time.Millisecond), 60*time.Millisecond,
		kv("db.system", "postgresql"), kv("db.statement", "SELECT id, chunk FROM documents ORDER BY embedding <-> $1 LIMIT 4"),
	)
	tr.span(root, "openai.chat", tracepb.Span_SPAN_KIND_CLIENT, t.Add(230*time.Millisecond), 2300*time.Millisecond,
		kv("gen_ai.system", "openai"),
		kv("gen_ai.request.model", "gpt-5-mini"),
		kv("gen_ai.response.model", "gpt-5-mini"),
		kv("gen_ai.usage.prompt_tokens", int64(1288)),
		kv("gen_ai.usage.completion_tokens", int64(142)),
		kv("gen_ai.prompt.0.role", "system"),
		kv("gen_ai.prompt.0.content", "Answer using only the provided docs. Cite sources as [n]."),
		kv("gen_ai.prompt.1.role", "user"),
		kv("gen_ai.prompt.1.content", "How do I rotate an API key without downtime?"),
		kv("gen_ai.completion.0.role", "assistant"),
		kv("gen_ai.completion.0.content", "Create a second key, deploy it alongside the old one, then revoke the old key once traffic has moved [1]. Keys can overlap for up to 24 hours [2]."),
	)
	return tr
}

func kv(k string, v any) *commonpb.KeyValue {
	var av *commonpb.AnyValue
	switch x := v.(type) {
	case string:
		av = &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: x}}
	case int64:
		av = &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: x}}
	case float64:
		av = &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: x}}
	case bool:
		av = &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: x}}
	}
	return &commonpb.KeyValue{Key: k, Value: av}
}

func js(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func id(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}
