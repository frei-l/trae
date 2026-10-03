package genai

import (
	"testing"
	"time"

	"github.com/frei-l/trae/internal/model"
)

func span(attrs map[string]any, events ...model.Event) *model.Span {
	return &model.Span{
		Name: "s", StartNs: time.Unix(100, 0).UnixNano(), EndNs: time.Unix(103, 0).UnixNano(),
		Attrs: attrs, Resource: map[string]any{"service.name": "svc"}, Events: events,
	}
}

func roles(msgs []model.Message) []string {
	var out []string
	for _, m := range msgs {
		out = append(out, m.Role)
	}
	return out
}

func eq(t *testing.T, what string, got, want any) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %#v, want %#v", what, got, want)
	}
}

// A generation as @langfuse/tracing records it for a pi-ai call: the
// context as input, the assistant message as output.
func TestLangfuseGeneration(t *testing.T) {
	s := span(map[string]any{
		"langfuse.observation.type":                  "generation",
		"langfuse.observation.model.name":            "claude-sonnet-5-5",
		"langfuse.observation.model.parameters":      `{"temperature":0.3}`,
		"langfuse.observation.usage_details":         `{"input":800,"output":90,"cacheRead":500,"total":890}`,
		"langfuse.observation.cost_details":          `{"input":0.002,"output":0.001,"total":0.003}`,
		"langfuse.observation.completion_start_time": `"1970-01-01T00:01:40.750Z"`,
		"langfuse.session.id":                        "sess_1",
		"langfuse.user.id":                           "u1",
		"langfuse.trace.tags":                        []any{"a", "b"},
		"langfuse.observation.input": `{"systemPrompt":"Be brief.","messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[{"type":"toolCall","id":"t1","name":"lookup","arguments":{"w":"x"}}]},
			{"role":"toolResult","toolCallId":"t1","toolName":"lookup","content":[{"type":"text","text":"found"}],"isError":false}]}`,
		"langfuse.observation.output": `{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"Hello!"}]}`,
	})
	Normalize(s)
	ai := s.AI
	eq(t, "service", s.Service, "svc")
	eq(t, "kind", ai.Kind, model.KindLLM)
	eq(t, "source", ai.Source, "langfuse")
	eq(t, "model", ai.Model, "claude-sonnet-5-5")
	eq(t, "in", ai.InTokens, int64(800))
	eq(t, "out", ai.OutTokens, int64(90))
	eq(t, "cache", ai.CacheTokens, int64(500))
	eq(t, "total", ai.TotalTokens, int64(890))
	eq(t, "cost", ai.Cost, 0.003)
	eq(t, "ttft", ai.TTFTNs, int64(750*time.Millisecond))
	eq(t, "session", ai.SessionID, "sess_1")
	eq(t, "user", ai.UserID, "u1")
	eq(t, "tags", len(ai.Tags), 2)
	eq(t, "params", ai.Params["temperature"], 0.3)
	eq(t, "input roles", len(ai.Input), 4)
	if r := roles(ai.Input); r[0] != "system" || r[1] != "user" || r[2] != "assistant" || r[3] != "tool" {
		t.Errorf("roles %v", r)
	}
	call := ai.Input[2].Parts[0]
	eq(t, "call type", call.Type, model.PartToolCall)
	eq(t, "call name", call.Name, "lookup")
	eq(t, "call id", call.ID, "t1")
	res := ai.Input[3].Parts[0]
	eq(t, "result type", res.Type, model.PartToolResult)
	eq(t, "result text", res.Text, "found")
	eq(t, "result id", res.ID, "t1")
	eq(t, "output parts", len(ai.Output[0].Parts), 2)
	eq(t, "thinking", ai.Output[0].Parts[0].Type, model.PartThinking)
	eq(t, "text", ai.Output[0].Parts[1].Text, "Hello!")
}

func TestLangfuseToolAndAgent(t *testing.T) {
	tool := span(map[string]any{
		"langfuse.observation.type":   "tool",
		"langfuse.observation.input":  `{"word":"x","level":"B2"}`,
		"langfuse.observation.output": "plain result",
	})
	tool.Name = "lookup"
	Normalize(tool)
	eq(t, "kind", tool.AI.Kind, model.KindTool)
	eq(t, "tool name", tool.AI.Tool.Name, "lookup")
	// The sender's JSON is kept as written, key order included.
	eq(t, "args", tool.AI.Input[0].Parts[0].Text, `{"word":"x","level":"B2"}`)
	eq(t, "args part", tool.AI.Input[0].Parts[0].Type, model.PartValue)
	eq(t, "result", tool.AI.Output[0].Parts[0].Text, "plain result")

	agent := span(map[string]any{"langfuse.observation.type": "agent", "langfuse.observation.level": "error", "langfuse.observation.status_message": "bad"})
	Normalize(agent)
	eq(t, "agent kind", agent.AI.Kind, model.KindAgent)
	eq(t, "failed", agent.Failed(), true)
	eq(t, "status msg", agent.StatusMsg, "bad")
}

func TestGenAICurrent(t *testing.T) {
	s := span(map[string]any{
		"gen_ai.operation.name":      "chat",
		"gen_ai.provider.name":       "openai",
		"gen_ai.request.model":       "gpt-5",
		"gen_ai.response.model":      "gpt-5-2026",
		"gen_ai.request.temperature": 0.2,
		"gen_ai.usage.input_tokens":  int64(10),
		"gen_ai.usage.output_tokens": int64(5),
		"gen_ai.system_instructions": `[{"type":"text","content":"sys"}]`,
		"gen_ai.input.messages":      `[{"role":"user","parts":[{"type":"text","content":"q"}]},{"role":"tool","parts":[{"type":"tool_call_response","id":"c1","response":{"ok":true}}]}]`,
		"gen_ai.output.messages":     `[{"role":"assistant","parts":[{"type":"tool_call","id":"c2","name":"f","arguments":{"a":1}},{"type":"reasoning","content":"why"}]}]`,
	})
	Normalize(s)
	ai := s.AI
	eq(t, "kind", ai.Kind, model.KindLLM)
	eq(t, "source", ai.Source, "gen_ai")
	eq(t, "model", ai.Model, "gpt-5-2026")
	eq(t, "provider", ai.Provider, "openai")
	eq(t, "tokens", ai.InTokens+ai.OutTokens, int64(15))
	eq(t, "param", ai.Params["temperature"], 0.2)
	if r := roles(ai.Input); len(r) != 3 || r[0] != "system" || r[1] != "user" || r[2] != "tool" {
		t.Fatalf("roles %v", r)
	}
	eq(t, "sys", ai.Input[0].Parts[0].Text, "sys")
	eq(t, "q", ai.Input[1].Parts[0].Text, "q")
	eq(t, "tool result", ai.Input[2].Parts[0].Type, model.PartToolResult)
	eq(t, "tool result id", ai.Input[2].Parts[0].ID, "c1")
	eq(t, "call", ai.Output[0].Parts[0].Name, "f")
	eq(t, "reasoning", ai.Output[0].Parts[1].Type, model.PartThinking)
}

func TestGenAIOnEvent(t *testing.T) {
	s := span(map[string]any{"gen_ai.operation.name": "chat", "gen_ai.request.model": "m"},
		model.Event{Name: "gen_ai.client.inference.operation.details", Attrs: map[string]any{
			"gen_ai.input.messages":  `[{"role":"user","parts":[{"type":"text","content":"from event"}]}]`,
			"gen_ai.output.messages": `[{"role":"assistant","parts":[{"type":"text","content":"answer"}]}]`,
		}})
	Normalize(s)
	eq(t, "input", s.AI.Input[0].Parts[0].Text, "from event")
	eq(t, "output", s.AI.Output[0].Parts[0].Text, "answer")
}

func TestGenAILegacy(t *testing.T) {
	s := span(map[string]any{
		"gen_ai.system":                              "openai",
		"gen_ai.request.model":                       "gpt-4o",
		"gen_ai.usage.prompt_tokens":                 int64(7),
		"gen_ai.usage.completion_tokens":             int64(3),
		"gen_ai.prompt.0.role":                       "system",
		"gen_ai.prompt.0.content":                    "sys",
		"gen_ai.prompt.1.role":                       "user",
		"gen_ai.prompt.1.content":                    "hello",
		"gen_ai.completion.0.role":                   "assistant",
		"gen_ai.completion.0.tool_calls.0.name":      "search",
		"gen_ai.completion.0.tool_calls.0.arguments": `{"q":"x"}`,
	})
	Normalize(s)
	ai := s.AI
	eq(t, "kind", ai.Kind, model.KindLLM)
	eq(t, "provider", ai.Provider, "openai")
	eq(t, "in", ai.InTokens, int64(7))
	eq(t, "out", ai.OutTokens, int64(3))
	if r := roles(ai.Input); len(r) != 2 || r[0] != "system" || r[1] != "user" {
		t.Fatalf("roles %v", r)
	}
	eq(t, "hello", ai.Input[1].Parts[0].Text, "hello")
	eq(t, "tool call", ai.Output[0].Parts[0].Name, "search")
	eq(t, "tool args", ai.Output[0].Parts[0].Args, `{"q":"x"}`)
}

func TestGenAIMessageEvents(t *testing.T) {
	s := span(map[string]any{"gen_ai.system": "openai", "gen_ai.request.model": "m"},
		model.Event{Name: "gen_ai.user.message", Attrs: map[string]any{"content": "hi there"}},
		model.Event{Name: "gen_ai.choice", Attrs: map[string]any{"message": `{"content":"hello back"}`, "index": int64(0)}},
	)
	Normalize(s)
	eq(t, "user", s.AI.Input[0].Role, "user")
	eq(t, "user text", s.AI.Input[0].Parts[0].Text, "hi there")
	eq(t, "choice", s.AI.Output[0].Parts[0].Text, "hello back")
	eq(t, "choice role", s.AI.Output[0].Role, "assistant")
}

func TestGenAITool(t *testing.T) {
	s := span(map[string]any{
		"gen_ai.operation.name":      "execute_tool",
		"gen_ai.tool.name":           "get_order",
		"gen_ai.tool.call.id":        "c1",
		"gen_ai.tool.call.arguments": `{"id":1}`,
		"gen_ai.tool.call.result":    `{"ok":true}`,
	})
	Normalize(s)
	eq(t, "kind", s.AI.Kind, model.KindTool)
	eq(t, "name", s.AI.Tool.Name, "get_order")
	eq(t, "args", s.AI.Input[0].Parts[0].Text, `{"id":1}`)
	eq(t, "result", s.AI.Output[0].Parts[0].Text, `{"ok":true}`)
}

func TestPlainSpan(t *testing.T) {
	s := span(map[string]any{"http.route": "/x"})
	s.Resource = map[string]any{}
	Normalize(s)
	eq(t, "kind", s.AI.Kind, model.KindSpan)
	eq(t, "source", s.AI.Source, "")
	eq(t, "service", s.Service, "unknown")
	if s.AI.Input != nil || s.AI.Output != nil {
		t.Errorf("unexpected messages %v %v", s.AI.Input, s.AI.Output)
	}
}

func TestMessagesShapes(t *testing.T) {
	// OpenAI chat completion response.
	m := Messages(`{"choices":[{"message":{"role":"assistant","content":"x","tool_calls":[{"id":"1","function":{"name":"f","arguments":"{}"}}]}}]}`, "output")
	eq(t, "choice", len(m), 1)
	eq(t, "choice parts", len(m[0].Parts), 2)
	eq(t, "fn", m[0].Parts[1].Name, "f")
	// A plain string stays a single message with the given role.
	m = Messages("just text", "input")
	eq(t, "plain role", m[0].Role, "input")
	eq(t, "plain text", m[0].Parts[0].Text, "just text")
	// A JSON-encoded string is decoded.
	m = Messages(`"quoted"`, "input")
	eq(t, "quoted", m[0].Parts[0].Text, "quoted")
	// Anthropic-style content blocks.
	m = Messages(`[{"role":"user","content":[{"type":"tool_result","tool_use_id":"x","content":"r"}]}]`, "input")
	eq(t, "anthropic result", m[0].Parts[0].Type, model.PartToolResult)
	eq(t, "anthropic result text", m[0].Parts[0].Text, "r")
	if Messages(nil, "input") != nil {
		t.Error("nil should give no messages")
	}
}
