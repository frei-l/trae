package store

import (
	"context"
	"testing"
	"time"

	"github.com/frei-l/trae/internal/model"
)

var base = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).UnixNano()

func sp(trace, id, parent, name string, startMs, durMs int64, ai model.AI) model.Span {
	if ai.Kind == "" {
		ai.Kind = model.KindSpan
	}
	return model.Span{
		TraceID: trace, SpanID: id, ParentID: parent, Name: name, Service: "svc",
		StartNs: base + startMs*1e6, EndNs: base + (startMs+durMs)*1e6, StatusCode: "unset",
		Attrs: map[string]any{}, Resource: map[string]any{}, AI: ai,
	}
}

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func llm(model_ string, in, out int64, cost float64, q string) model.AI {
	return model.AI{Kind: model.KindLLM, Model: model_, InTokens: in, OutTokens: out, Cost: cost,
		Input: []model.Message{{Role: "user", Parts: []model.Part{{Type: model.PartText, Text: q}}}}}
}

func TestSummaryAcrossBatches(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	// Children usually finish, and are exported, before their root.
	if err := s.Insert([]model.Span{
		sp("t1", "b", "a", "generation", 10, 500, llm("claude", 100, 20, 0.01, "What is serendipity?")),
		sp("t1", "c", "a", "lookup", 520, 50, model.AI{Kind: model.KindTool}),
	}); err != nil {
		t.Fatal(err)
	}
	page, _ := s.Traces(ctx, Filter{})
	if len(page.Traces) != 1 || page.Traces[0].Name != "generation" {
		t.Fatalf("before root: %+v", page.Traces)
	}
	seq1 := s.Seq()
	agent := sp("t1", "a", "", "explain-word", 0, 1200, model.AI{Kind: model.KindAgent, InTokens: 999, TraceName: "explain"})
	failed := sp("t1", "d", "a", "generation", 600, 500, llm("claude", 200, 40, 0.02, "again"))
	failed.StatusCode = "error"
	if err := s.Insert([]model.Span{agent, failed}); err != nil {
		t.Fatal(err)
	}
	if s.Seq() <= seq1 {
		t.Error("seq did not advance")
	}
	page, _ = s.Traces(ctx, Filter{})
	tr := page.Traces[0]
	if tr.Name != "explain" || tr.SpanCount != 4 || tr.LLMCount != 2 || tr.ErrorCount != 1 {
		t.Errorf("summary %+v", tr)
	}
	// Only model calls count toward tokens; the agent's 999 would double count.
	if tr.InTokens != 300 || tr.OutTokens != 60 || tr.Cost < 0.0299 || tr.Cost > 0.0301 {
		t.Errorf("usage %+v", tr)
	}
	if tr.StartNs != base || tr.EndNs != base+1200*1e6 || len(tr.Models) != 1 || tr.Models[0] != "claude" {
		t.Errorf("range/models %+v", tr)
	}
	if tr.Preview != "What is serendipity?" {
		t.Errorf("preview %q", tr.Preview)
	}
	full, err := s.Trace(ctx, "t1")
	if err != nil || len(full.Spans) != 4 || full.Spans[0].SpanID != "a" {
		t.Fatalf("trace %v %+v", err, full)
	}
	if _, err := s.Trace(ctx, "nope"); err != ErrNotFound {
		t.Errorf("missing trace: %v", err)
	}
	one, err := s.Span(ctx, "t1", "b")
	if err != nil || one.AI.Model != "claude" || one.AI.Input[0].Parts[0].Text != "What is serendipity?" {
		t.Errorf("span %v %+v", err, one)
	}
}

func TestFiltersAndPages(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	var spans []model.Span
	for i := 0; i < 25; i++ {
		m := "gpt-5"
		if i%5 == 0 {
			m = "claude_x%"
		}
		x := sp(string(rune('A'+i)), "r", "", "job", int64(i)*1000, 100, llm(m, 1, 1, 0, "question "+string(rune('a'+i))))
		if i%10 == 0 {
			x.StatusCode = "error"
			x.Service = "other"
		}
		spans = append(spans, x)
	}
	if err := s.Insert(spans); err != nil {
		t.Fatal(err)
	}
	page, _ := s.Traces(ctx, Filter{Limit: 10})
	if len(page.Traces) != 10 || page.Next == 0 || page.Totals.Traces != 25 || page.Traces[0].TraceID != "Y" {
		t.Fatalf("page 1: %d next %d totals %+v first %s", len(page.Traces), page.Next, page.Totals, page.Traces[0].TraceID)
	}
	seen := len(page.Traces)
	for page.Next != 0 {
		page, _ = s.Traces(ctx, Filter{Limit: 10, Before: page.Next})
		seen += len(page.Traces)
	}
	if seen != 25 {
		t.Errorf("paged through %d traces", seen)
	}
	for _, c := range []struct {
		f    Filter
		want int
	}{
		{Filter{Errors: true}, 3},
		{Filter{Service: "other"}, 3},
		{Filter{Model: "claude_x%"}, 5},
		{Filter{Model: "claude"}, 0}, // exact model, not a prefix
		{Filter{Query: "question c"}, 1},
		{Filter{Query: "_x%"}, 5},    // LIKE wildcards are literal
		{Filter{Query: "CLAUDE"}, 5}, // case-insensitive
	} {
		page, err := s.Traces(ctx, c.f)
		if err != nil {
			t.Fatal(err)
		}
		if page.Totals.Traces != c.want || len(page.Traces) != c.want {
			t.Errorf("%+v: totals %d rows %d, want %d", c.f, page.Totals.Traces, len(page.Traces), c.want)
		}
	}
	calls, _ := s.Calls(ctx, Filter{Errors: true})
	if calls.Totals.Calls != 3 || len(calls.Calls) != 3 || !calls.Calls[0].Failed {
		t.Errorf("calls %+v", calls.Totals)
	}
	calls, _ = s.Calls(ctx, Filter{Model: "gpt-5", Limit: 5})
	if calls.Totals.Calls != 20 || len(calls.Calls) != 5 || calls.Next == 0 || calls.Calls[0].TraceName != "job" {
		t.Errorf("calls by model %+v next %d", calls.Totals, calls.Next)
	}
	f, _ := s.Facets(ctx)
	if len(f.Services) != 2 || len(f.Models) != 2 {
		t.Errorf("facets %+v", f)
	}
}

func TestDeleteClearPrune(t *testing.T) {
	s := open(t)
	old := sp("old", "1", "", "old", -3*24*3600*1000, 10, model.AI{})
	mid := sp("mid", "1", "", "mid", 0, 10, model.AI{})
	s.Insert([]model.Span{old, mid, sp("new", "1", "", "new", 10, 10, model.AI{})})
	if err := s.Prune(time.Unix(0, base).Add(-24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	st, _ := s.Stats(context.Background())
	if st.Traces != 2 || st.Spans != 2 {
		t.Errorf("after prune %+v", st)
	}
	s.Delete("mid")
	st, _ = s.Stats(context.Background())
	if st.Traces != 1 {
		t.Errorf("after delete %+v", st)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	st, _ = s.Stats(context.Background())
	if st.Traces != 0 || st.Spans != 0 {
		t.Errorf("after clear %+v", st)
	}
}

func TestWait(t *testing.T) {
	s := open(t)
	start := s.Seq()
	done := make(chan int64)
	go func() { done <- s.Wait(context.Background(), start) }()
	time.Sleep(20 * time.Millisecond)
	s.Insert([]model.Span{sp("w", "1", "", "w", 0, 1, model.AI{})})
	select {
	case got := <-done:
		if got <= start {
			t.Errorf("woke with %d", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not wake")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if got := s.Wait(ctx, s.Seq()); got != s.Seq() {
		t.Errorf("timeout returned %d", got)
	}
}

func TestReopen(t *testing.T) {
	path := t.TempDir() + "/traces.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Insert([]model.Span{sp("x", "1", "", "x", 0, 1, model.AI{})})
	seq := s.Seq()
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Seq() != seq {
		t.Errorf("seq %d after reopen, want %d", s.Seq(), seq)
	}
	if tr, err := s.Trace(context.Background(), "x"); err != nil || tr.Summary.Name != "x" {
		t.Errorf("reopened: %v", err)
	}
}

func TestPreview(t *testing.T) {
	x := sp("p", "1", "", "p", 0, 1, model.AI{Input: []model.Message{
		{Role: "input", Parts: []model.Part{{Type: model.PartValue, Text: `{"word":"serendipity","nested":{"a":1},"n":2}`}}},
	}})
	if got := Preview(&x); got != "word: serendipity · n: 2" {
		t.Errorf("preview %q", got)
	}
}
