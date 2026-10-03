// Package model holds the span and trace shapes shared by the receiver,
// the store and the API.
package model

// Span is one received span, flattened from OTLP, with its AI fields
// already normalized.
type Span struct {
	TraceID    string         `json:"traceId"`
	SpanID     string         `json:"spanId"`
	ParentID   string         `json:"parentId,omitempty"`
	Name       string         `json:"name"`
	Kind       string         `json:"kind"` // internal, server, client, producer, consumer
	Service    string         `json:"service"`
	Scope      string         `json:"scope,omitempty"`
	StartNs    int64          `json:"startNs"`
	EndNs      int64          `json:"endNs"`
	StatusCode string         `json:"status"` // unset, ok, error
	StatusMsg  string         `json:"statusMessage,omitempty"`
	Attrs      map[string]any `json:"attributes"`
	Resource   map[string]any `json:"resource"`
	Events     []Event        `json:"events"`
	Links      []Link         `json:"links"`
	AI         AI             `json:"ai"`
}

// Event is a span event.
type Event struct {
	Name   string         `json:"name"`
	TimeNs int64          `json:"timeNs"`
	Attrs  map[string]any `json:"attributes"`
}

// Link is a span link.
type Link struct {
	TraceID string         `json:"traceId"`
	SpanID  string         `json:"spanId"`
	Attrs   map[string]any `json:"attributes,omitempty"`
}

// AI kinds. Everything that is not recognised is KindSpan.
const (
	KindLLM       = "llm"
	KindEmbedding = "embedding"
	KindTool      = "tool"
	KindAgent     = "agent"
	KindChain     = "chain"
	KindRetriever = "retriever"
	KindEvaluator = "evaluator"
	KindGuardrail = "guardrail"
	KindEvent     = "event"
	KindSpan      = "span"
)

// AI is what a span says about a model call, a tool or an agent step.
type AI struct {
	Kind        string         `json:"kind"`
	Model       string         `json:"model,omitempty"`
	Provider    string         `json:"provider,omitempty"`
	InTokens    int64          `json:"inTokens,omitempty"`
	OutTokens   int64          `json:"outTokens,omitempty"`
	CacheTokens int64          `json:"cacheTokens,omitempty"`
	TotalTokens int64          `json:"totalTokens,omitempty"`
	Cost        float64        `json:"cost,omitempty"`
	TTFTNs      int64          `json:"ttftNs,omitempty"`
	SessionID   string         `json:"sessionId,omitempty"`
	UserID      string         `json:"userId,omitempty"`
	TraceName   string         `json:"traceName,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Level       string         `json:"level,omitempty"`
	Params      map[string]any `json:"params,omitempty"`
	Tool        *Tool          `json:"tool,omitempty"`
	Input       []Message      `json:"input,omitempty"`
	Output      []Message      `json:"output,omitempty"`
	// Source names the convention the fields came from: langfuse, gen_ai.
	Source string `json:"source,omitempty"`
}

// Tool describes a tool execution span.
type Tool struct {
	Name   string `json:"name,omitempty"`
	CallID string `json:"callId,omitempty"`
	Args   string `json:"args,omitempty"`
	Result string `json:"result,omitempty"`
}

// Message is one chat message: a role and its parts.
type Message struct {
	Role  string `json:"role"`
	Name  string `json:"name,omitempty"`
	Parts []Part `json:"parts"`
}

// Part kinds.
const (
	PartText       = "text"
	PartThinking   = "thinking"
	PartToolCall   = "tool_call"
	PartToolResult = "tool_result"
	PartValue      = "value" // anything else, kept as pretty JSON
)

// Part is one piece of a message.
type Part struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// Tool calls and results.
	Name string `json:"name,omitempty"`
	ID   string `json:"id,omitempty"`
	Args string `json:"args,omitempty"`
}

// Failed reports whether the span ended in error.
func (s *Span) Failed() bool {
	return s.StatusCode == "error" || s.AI.Level == "ERROR"
}

// TraceSummary is one row of the trace list.
type TraceSummary struct {
	TraceID    string   `json:"traceId"`
	Name       string   `json:"name"`
	Service    string   `json:"service"`
	StartNs    int64    `json:"startNs"`
	EndNs      int64    `json:"endNs"`
	SpanCount  int      `json:"spans"`
	ErrorCount int      `json:"errors"`
	LLMCount   int      `json:"llmCalls"`
	InTokens   int64    `json:"inTokens"`
	OutTokens  int64    `json:"outTokens"`
	Cost       float64  `json:"cost"`
	Models     []string `json:"models"`
	SessionID  string   `json:"sessionId,omitempty"`
	Preview    string   `json:"preview,omitempty"`
	Seq        int64    `json:"seq"`
}
