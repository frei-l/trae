// Package genai reads the AI meaning of a span out of its attributes. It
// understands Langfuse's langfuse.* attributes and the OpenTelemetry GenAI
// semantic conventions (gen_ai.*, current and legacy), preferring Langfuse
// when both are present.
package genai

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/frei-l/trae/internal/model"
)

// Normalize fills s.Service and s.AI from the span's attributes, events and
// resource.
func Normalize(s *model.Span) {
	s.Service = str(s.Resource["service.name"])
	if s.Service == "" {
		s.Service = "unknown"
	}
	a := s.Attrs
	ai := model.AI{Kind: model.KindSpan}

	// Newer GenAI instrumentations put messages on an event instead of the
	// span; fold those attributes in without overriding span attributes.
	merged := a
	for _, e := range s.Events {
		if e.Name == "gen_ai.client.inference.operation.details" {
			merged = withFallback(merged, e.Attrs)
		}
	}
	a = merged

	if t := str(a["langfuse.observation.type"]); t != "" {
		ai.Source = "langfuse"
		ai.Kind = langfuseKind(t)
	}
	if op := str(a["gen_ai.operation.name"]); op != "" {
		if ai.Source == "" {
			ai.Source = "gen_ai"
		}
		if ai.Kind == model.KindSpan {
			ai.Kind = genaiKind(op)
		}
	}

	ai.Model = first(a, "langfuse.observation.model.name", "gen_ai.response.model", "gen_ai.request.model")
	ai.Provider = first(a, "gen_ai.provider.name", "gen_ai.system")
	if ai.Model != "" && ai.Kind == model.KindSpan {
		ai.Kind = model.KindLLM
	}
	if ai.Source == "" && (ai.Model != "" || hasPrefix(a, "gen_ai.")) {
		ai.Source = "gen_ai"
	}
	if ai.Source == "" && hasPrefix(a, "langfuse.") {
		ai.Source = "langfuse"
	}

	usage(a, &ai)
	ai.Cost = cost(a)
	ai.SessionID = first(a, "langfuse.session.id", "session.id", "gen_ai.conversation.id")
	ai.UserID = first(a, "langfuse.user.id", "user.id")
	ai.TraceName = str(a["langfuse.trace.name"])
	ai.Tags = strList(a["langfuse.trace.tags"])
	ai.Level = strings.ToUpper(str(a["langfuse.observation.level"]))
	if p, ok := parseJSON(a["langfuse.observation.model.parameters"]).(map[string]any); ok && len(p) > 0 {
		ai.Params = p
	} else {
		ai.Params = requestParams(a)
	}
	if cst := str(a["langfuse.observation.completion_start_time"]); cst != "" {
		if t, err := time.Parse(time.RFC3339Nano, strings.Trim(cst, `"`)); err == nil && s.StartNs > 0 {
			if d := t.UnixNano() - s.StartNs; d > 0 {
				ai.TTFTNs = d
			}
		}
	}
	if ai.TTFTNs == 0 {
		if v, ok := num(a["gen_ai.server.time_to_first_token"]); ok && v > 0 {
			ai.TTFTNs = int64(v * 1e9)
		}
	}
	if ai.Level == "" && s.StatusCode == "error" {
		ai.Level = "ERROR"
	}
	if s.StatusMsg == "" {
		s.StatusMsg = str(a["langfuse.observation.status_message"])
	}

	inRole, outRole := "input", "output"
	if v, ok := a["langfuse.observation.input"]; ok {
		ai.Input = Messages(v, inRole)
	} else if v, ok := a["langfuse.trace.input"]; ok && s.ParentID == "" {
		ai.Input = Messages(v, inRole)
	}
	if v, ok := a["langfuse.observation.output"]; ok {
		ai.Output = Messages(v, outRole)
	} else if v, ok := a["langfuse.trace.output"]; ok && s.ParentID == "" {
		ai.Output = Messages(v, outRole)
	}
	if ai.Input == nil {
		var in []model.Message
		if v, ok := a["gen_ai.system_instructions"]; ok {
			in = append(in, model.Message{Role: "system", Parts: partsOf(parseJSON(v))})
		}
		if v, ok := a["gen_ai.input.messages"]; ok {
			in = append(in, Messages(v, inRole)...)
		} else {
			in = append(in, legacyMessages(a, "gen_ai.prompt")...)
			in = append(in, eventMessages(s.Events, false)...)
		}
		if len(in) > 0 {
			ai.Input = in
		}
	}
	if ai.Output == nil {
		if v, ok := a["gen_ai.output.messages"]; ok {
			ai.Output = Messages(v, outRole)
		} else if out := append(legacyMessages(a, "gen_ai.completion"), eventMessages(s.Events, true)...); len(out) > 0 {
			ai.Output = out
		}
	}

	if ai.Kind == model.KindTool || str(a["gen_ai.tool.name"]) != "" {
		t := &model.Tool{
			Name:   first(a, "gen_ai.tool.name"),
			CallID: first(a, "gen_ai.tool.call.id"),
			Args:   text(a["gen_ai.tool.call.arguments"]),
			Result: text(a["gen_ai.tool.call.result"]),
		}
		if t.Name == "" {
			t.Name = s.Name
		}
		if t.Args == "" {
			t.Args = text(a["langfuse.observation.input"])
		}
		if t.Result == "" {
			t.Result = text(a["langfuse.observation.output"])
		}
		if ai.Kind == model.KindSpan {
			ai.Kind = model.KindTool
		}
		ai.Tool = t
		if ai.Input == nil && t.Args != "" {
			ai.Input = Messages(t.Args, inRole)
		}
		if ai.Output == nil && t.Result != "" {
			ai.Output = Messages(t.Result, outRole)
		}
	}
	s.AI = ai
}

func langfuseKind(t string) string {
	switch strings.ToLower(t) {
	case "generation":
		return model.KindLLM
	case "embedding":
		return model.KindEmbedding
	case "tool":
		return model.KindTool
	case "agent":
		return model.KindAgent
	case "chain":
		return model.KindChain
	case "retriever":
		return model.KindRetriever
	case "evaluator":
		return model.KindEvaluator
	case "guardrail":
		return model.KindGuardrail
	case "event":
		return model.KindEvent
	}
	return model.KindSpan
}

func genaiKind(op string) string {
	switch strings.ToLower(op) {
	case "chat", "text_completion", "generate_content", "completion", "responses":
		return model.KindLLM
	case "embeddings", "embedding":
		return model.KindEmbedding
	case "execute_tool":
		return model.KindTool
	case "invoke_agent", "create_agent":
		return model.KindAgent
	}
	return model.KindSpan
}

func usage(a map[string]any, ai *model.AI) {
	if u, ok := parseJSON(a["langfuse.observation.usage_details"]).(map[string]any); ok {
		for k, v := range u {
			n, ok := num(v)
			if !ok {
				continue
			}
			lk := strings.ToLower(k)
			switch {
			case strings.Contains(lk, "cache") && (strings.Contains(lk, "read") || strings.Contains(lk, "cached")):
				ai.CacheTokens += int64(n)
			case strings.Contains(lk, "cache"):
				// cache writes are part of input in most providers' accounting
			case lk == "input" || lk == "prompt_tokens" || lk == "input_tokens" || lk == "prompttokens" || lk == "inputtokens":
				ai.InTokens += int64(n)
			case lk == "output" || lk == "completion_tokens" || lk == "output_tokens" || lk == "completiontokens" || lk == "outputtokens":
				ai.OutTokens += int64(n)
			case lk == "total" || lk == "total_tokens" || lk == "totaltokens":
				ai.TotalTokens = int64(n)
			}
		}
	}
	if ai.InTokens == 0 {
		ai.InTokens = int64(firstNum(a, "gen_ai.usage.input_tokens", "gen_ai.usage.prompt_tokens"))
	}
	if ai.OutTokens == 0 {
		ai.OutTokens = int64(firstNum(a, "gen_ai.usage.output_tokens", "gen_ai.usage.completion_tokens"))
	}
	if ai.CacheTokens == 0 {
		ai.CacheTokens = int64(firstNum(a, "gen_ai.usage.cache_read.input_tokens", "gen_ai.usage.cache_read_input_tokens"))
	}
	if ai.TotalTokens == 0 {
		ai.TotalTokens = int64(firstNum(a, "gen_ai.usage.total_tokens"))
	}
	if ai.TotalTokens == 0 {
		ai.TotalTokens = ai.InTokens + ai.OutTokens
	}
}

func cost(a map[string]any) float64 {
	c, ok := parseJSON(a["langfuse.observation.cost_details"]).(map[string]any)
	if !ok {
		n, _ := num(a["gen_ai.usage.cost"])
		return n
	}
	if t, ok := num(c["total"]); ok {
		return t
	}
	var sum float64
	for _, v := range c {
		if n, ok := num(v); ok {
			sum += n
		}
	}
	return sum
}

func requestParams(a map[string]any) map[string]any {
	p := map[string]any{}
	for k, v := range a {
		if rest, ok := strings.CutPrefix(k, "gen_ai.request."); ok && rest != "model" {
			p[rest] = v
		}
	}
	if len(p) == 0 {
		return nil
	}
	return p
}

// Messages turns an input or output value into chat messages. Chat-shaped
// values (arrays of {role, content}, {messages: …}, OpenAI choices, GenAI
// parts) become one message each; anything else becomes a single message
// with the given role.
func Messages(v any, role string) []model.Message {
	raw, _ := v.(string)
	v = parseJSON(v)
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		if isChat(x) {
			var out []model.Message
			for _, e := range x {
				if m, ok := e.(map[string]any); ok {
					out = append(out, message(m, role)...)
				} else {
					out = append(out, model.Message{Role: role, Parts: partsOf(e)})
				}
			}
			return out
		}
	case map[string]any:
		if msgs, ok := x["messages"].([]any); ok {
			var out []model.Message
			for _, k := range []string{"systemPrompt", "system", "instructions"} {
				if s, ok := x[k]; ok && s != nil && s != "" {
					out = append(out, model.Message{Role: "system", Parts: partsOf(s)})
				}
			}
			return append(out, Messages(msgs, role)...)
		}
		if choices, ok := x["choices"].([]any); ok && len(choices) > 0 {
			var out []model.Message
			for _, c := range choices {
				if cm, ok := c.(map[string]any); ok {
					if m, ok := cm["message"].(map[string]any); ok {
						out = append(out, message(m, "assistant")...)
					}
				}
			}
			if len(out) > 0 {
				return out
			}
		}
		if _, ok := x["role"]; ok {
			return message(x, role)
		}
	}
	switch v.(type) {
	case map[string]any, []any:
		if raw != "" {
			// Keep the sender's JSON as written: its key order reads better
			// than a re-encoded map's.
			return []model.Message{{Role: role, Parts: []model.Part{{Type: model.PartValue, Text: raw}}}}
		}
	}
	return []model.Message{{Role: role, Parts: partsOf(v)}}
}

func isChat(arr []any) bool {
	if len(arr) == 0 {
		return false
	}
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			return false
		}
		if _, ok := m["role"]; !ok {
			return false
		}
	}
	return true
}

func message(m map[string]any, fallback string) []model.Message {
	role := normRole(str(m["role"]))
	if role == "" {
		role = fallback
	}
	msg := model.Message{Role: role, Name: str(m["name"])}
	switch {
	case m["parts"] != nil:
		msg.Parts = partsOf(m["parts"])
	case m["content"] != nil:
		msg.Parts = partsOf(m["content"])
	case m["text"] != nil:
		msg.Parts = partsOf(m["text"])
	}
	// OpenAI assistant tool calls.
	if calls, ok := m["tool_calls"].([]any); ok {
		for _, c := range calls {
			if cm, ok := c.(map[string]any); ok {
				msg.Parts = append(msg.Parts, toolCall(cm))
			}
		}
	}
	// Tool results: OpenAI {role: tool, tool_call_id}, pi-ai {role: toolResult, toolCallId, toolName}.
	if role == "tool" {
		id := first(m, "tool_call_id", "toolCallId", "tool_use_id")
		name := first(m, "toolName", "name")
		var b strings.Builder
		for _, p := range msg.Parts {
			if p.Type == model.PartToolResult {
				return []model.Message{msg}
			}
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			if p.Text != "" {
				b.WriteString(p.Text)
			} else {
				b.WriteString(p.Args)
			}
		}
		if e, _ := m["isError"].(bool); e {
			name += " (error)"
		}
		msg.Parts = []model.Part{{Type: model.PartToolResult, ID: id, Name: strings.TrimSpace(name), Text: b.String()}}
	}
	return []model.Message{msg}
}

func normRole(r string) string {
	switch strings.ToLower(r) {
	case "human":
		return "user"
	case "ai", "model", "bot":
		return "assistant"
	case "toolresult", "tool_result", "function", "tool":
		return "tool"
	case "developer":
		return "system"
	}
	return strings.ToLower(r)
}

// partsOf turns message content into parts.
func partsOf(v any) []model.Part {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return []model.Part{{Type: model.PartText, Text: x}}
	case []any:
		var out []model.Part
		for _, e := range x {
			out = append(out, partsOf1(e)...)
		}
		return out
	case map[string]any:
		return partsOf1(x)
	}
	return []model.Part{{Type: model.PartText, Text: fmt.Sprint(v)}}
}

func partsOf1(e any) []model.Part {
	m, ok := e.(map[string]any)
	if !ok {
		return partsOf(e)
	}
	typ := strings.ToLower(str(m["type"]))
	switch typ {
	case "text", "input_text", "output_text":
		return []model.Part{{Type: model.PartText, Text: first(m, "text", "content")}}
	case "thinking", "reasoning", "redacted_thinking":
		t := first(m, "thinking", "content", "text")
		if t == "" {
			t = text(m["summary"])
		}
		return []model.Part{{Type: model.PartThinking, Text: t}}
	case "tool_call", "toolcall", "tool_use", "function_call", "function", "tool-call":
		return []model.Part{toolCall(m)}
	case "tool_call_response", "tool_result", "toolresult", "function_call_output", "tool-result":
		return []model.Part{{Type: model.PartToolResult, ID: first(m, "id", "tool_use_id", "toolCallId", "call_id", "toolCallId"), Name: first(m, "name", "toolName"),
			Text: text(firstAny(m, "response", "content", "result", "output"))}}
	}
	if typ == "" {
		if t, ok := m["text"].(string); ok && len(m) <= 2 {
			return []model.Part{{Type: model.PartText, Text: t}}
		}
	}
	return []model.Part{{Type: model.PartValue, Text: pretty(m)}}
}

func toolCall(m map[string]any) model.Part {
	p := model.Part{Type: model.PartToolCall, ID: first(m, "id", "call_id", "toolCallId")}
	if f, ok := m["function"].(map[string]any); ok {
		p.Name = str(f["name"])
		p.Args = text(f["arguments"])
		return p
	}
	p.Name = first(m, "name", "toolName")
	p.Args = text(firstAny(m, "arguments", "input", "args"))
	return p
}

// legacyMessages reads gen_ai.prompt.N.role/content style attributes.
func legacyMessages(a map[string]any, prefix string) []model.Message {
	idx := map[int]*model.Message{}
	type call struct{ name, args, id string }
	calls := map[int]map[int]*call{}
	for k, v := range a {
		rest, ok := strings.CutPrefix(k, prefix+".")
		if !ok {
			continue
		}
		head, field, ok := strings.Cut(rest, ".")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(head)
		if err != nil {
			continue
		}
		m := idx[n]
		if m == nil {
			m = &model.Message{Role: "user"}
			if prefix == "gen_ai.completion" {
				m.Role = "assistant"
			}
			idx[n] = m
		}
		switch {
		case field == "role":
			m.Role = normRole(str(v))
		case field == "content":
			m.Parts = append(partsOf(parseJSON(v)), m.Parts...)
		case strings.HasPrefix(field, "tool_calls."):
			parts := strings.SplitN(strings.TrimPrefix(field, "tool_calls."), ".", 2)
			if len(parts) != 2 {
				continue
			}
			ci, err := strconv.Atoi(parts[0])
			if err != nil {
				continue
			}
			if calls[n] == nil {
				calls[n] = map[int]*call{}
			}
			c := calls[n][ci]
			if c == nil {
				c = &call{}
				calls[n][ci] = c
			}
			switch parts[1] {
			case "name":
				c.name = str(v)
			case "arguments":
				c.args = str(v)
			case "id":
				c.id = str(v)
			}
		}
	}
	keys := make([]int, 0, len(idx))
	for k := range idx {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := make([]model.Message, 0, len(keys))
	for _, k := range keys {
		m := idx[k]
		var cis []int
		for ci := range calls[k] {
			cis = append(cis, ci)
		}
		sort.Ints(cis)
		for _, ci := range cis {
			c := calls[k][ci]
			m.Parts = append(m.Parts, model.Part{Type: model.PartToolCall, Name: c.name, Args: c.args, ID: c.id})
		}
		out = append(out, *m)
	}
	return out
}

// eventMessages reads the deprecated per-message GenAI events.
func eventMessages(events []model.Event, output bool) []model.Message {
	var out []model.Message
	for _, e := range events {
		switch e.Name {
		case "gen_ai.system.message", "gen_ai.user.message", "gen_ai.assistant.message", "gen_ai.tool.message":
			if output {
				continue
			}
			role := strings.TrimSuffix(strings.TrimPrefix(e.Name, "gen_ai."), ".message")
			m := map[string]any{"role": role}
			for k, v := range e.Attrs {
				m[strings.TrimPrefix(k, "gen_ai.event.")] = parseJSON(v)
			}
			if body, ok := m["body"].(map[string]any); ok {
				for k, v := range body {
					m[k] = v
				}
			}
			out = append(out, message(m, role)...)
		case "gen_ai.choice":
			if !output {
				continue
			}
			msg, _ := parseJSON(e.Attrs["message"]).(map[string]any)
			if msg == nil {
				if body, ok := parseJSON(e.Attrs["body"]).(map[string]any); ok {
					msg, _ = body["message"].(map[string]any)
				}
			}
			if msg == nil {
				msg = map[string]any{"content": e.Attrs["content"]}
			}
			if _, ok := msg["role"]; !ok {
				msg["role"] = "assistant"
			}
			out = append(out, message(msg, "assistant")...)
		}
	}
	return out
}

// helpers

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func first(a map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := str(a[k]); s != "" {
			return s
		}
	}
	return ""
}

func firstAny(a map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := a[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

func firstNum(a map[string]any, keys ...string) float64 {
	for _, k := range keys {
		if n, ok := num(a[k]); ok {
			return n
		}
	}
	return 0
}

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case int:
		return float64(x), true
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

func hasPrefix(a map[string]any, p string) bool {
	for k := range a {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

func strList(v any) []string {
	v = parseJSON(v)
	arr, ok := v.([]any)
	if !ok {
		if s := str(v); s != "" {
			return []string{s}
		}
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s := str(e); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func withFallback(a, b map[string]any) map[string]any {
	m := make(map[string]any, len(a)+len(b))
	for k, v := range b {
		m[k] = v
	}
	for k, v := range a {
		m[k] = v
	}
	return m
}

// parseJSON decodes strings that hold JSON objects or arrays (the usual
// way structured values travel in span attributes) and leaves other
// values alone.
func parseJSON(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	t := strings.TrimSpace(s)
	if len(t) < 2 || (t[0] != '{' && t[0] != '[' && t[0] != '"') {
		return v
	}
	var out any
	if err := json.Unmarshal([]byte(t), &out); err != nil {
		return v
	}
	return out
}

// text renders a value for display: strings as they are (JSON-encoded
// strings decoded), everything else as indented JSON.
func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		if p, ok := parseJSON(x).(string); ok {
			return p
		}
		return x
	}
	return pretty(v)
}

func pretty(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
