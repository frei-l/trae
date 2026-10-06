package otlp

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/frei-l/trae/internal/demo"
	"github.com/frei-l/trae/internal/model"
	"google.golang.org/protobuf/proto"
)

func post(t *testing.T, h http.Handler, ctype, enc string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if enc != "" {
		req.Header.Set("Content-Encoding", enc)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func capture() (http.Handler, *[]model.Span) {
	var got []model.Span
	return Handler(func(s []model.Span) error { got = append(got, s...); return nil }), &got
}

func TestProtobuf(t *testing.T) {
	h, got := capture()
	body, err := proto.Marshal(demo.Round(time.Unix(1_700_000_000, 0)))
	if err != nil {
		t.Fatal(err)
	}
	rec := post(t, h, "application/x-protobuf", "", body)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if len(*got) < 15 {
		t.Fatalf("got %d spans", len(*got))
	}
	var root, child *model.Span
	for i := range *got {
		s := &(*got)[i]
		if s.Name == "explain-word" {
			root = s
		}
		if s.Name == "lookup_dictionary" {
			child = s
		}
	}
	if root == nil || child == nil {
		t.Fatal("missing spans")
	}
	if len(root.TraceID) != 32 || len(root.SpanID) != 16 || root.ParentID != "" {
		t.Errorf("ids: %q %q %q", root.TraceID, root.SpanID, root.ParentID)
	}
	if child.ParentID != root.SpanID || child.TraceID != root.TraceID {
		t.Errorf("child not linked to root")
	}
	if root.Resource["service.name"] != "lingotutor" || root.Scope != "@langfuse/tracing" {
		t.Errorf("resource %v scope %q", root.Resource, root.Scope)
	}
	if root.StartNs != time.Unix(1_700_000_000, 0).UnixNano() || root.EndNs-root.StartNs != int64(6400*time.Millisecond) {
		t.Errorf("times %d %d", root.StartNs, root.EndNs)
	}
}

func TestGzipProtobuf(t *testing.T) {
	h, got := capture()
	body, _ := proto.Marshal(demo.Round(time.Now()))
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(body)
	zw.Close()
	rec := post(t, h, "application/x-protobuf", "gzip", buf.Bytes())
	if rec.Code != 200 || len(*got) == 0 {
		t.Fatalf("status %d, %d spans", rec.Code, len(*got))
	}
}

const jsonBody = `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"py-app"}}]},
"scopeSpans":[{"scope":{"name":"manual"},"spans":[
 {"traceId":"5B8EFFF798038103D269B633813FC60C","spanId":"EEE19B7EC3C1B174","parentSpanId":"","name":"chat",
  "kind":"SPAN_KIND_CLIENT","startTimeUnixNano":"1544712660000000000","endTimeUnixNano":1544712661000000000,
  "attributes":[
    {"key":"gen_ai.usage.input_tokens","value":{"intValue":"42"}},
    {"key":"ratio","value":{"doubleValue":0.5}},
    {"key":"stream","value":{"boolValue":true}},
    {"key":"tags","value":{"arrayValue":{"values":[{"stringValue":"a"},{"intValue":2}]}}},
    {"key":"obj","value":{"kvlistValue":{"values":[{"key":"k","value":{"stringValue":"v"}}]}}}],
  "events":[{"timeUnixNano":"1544712660500000000","name":"exception","attributes":[{"key":"exception.message","value":{"stringValue":"boom"}}]}],
  "status":{"code":2,"message":"boom"}}]}]}]}`

func TestJSON(t *testing.T) {
	h, got := capture()
	rec := post(t, h, "application/json", "", []byte(jsonBody))
	if rec.Code != 200 || rec.Body.String() != "{}" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body)
	}
	if len(*got) != 1 {
		t.Fatalf("got %d spans", len(*got))
	}
	s := (*got)[0]
	if s.TraceID != "5b8efff798038103d269b633813fc60c" || s.SpanID != "eee19b7ec3c1b174" {
		t.Errorf("ids %q %q", s.TraceID, s.SpanID)
	}
	if s.Kind != "client" || s.StatusCode != "error" || s.StatusMsg != "boom" {
		t.Errorf("kind %q status %q %q", s.Kind, s.StatusCode, s.StatusMsg)
	}
	if s.EndNs-s.StartNs != 1e9 {
		t.Errorf("duration %d", s.EndNs-s.StartNs)
	}
	if s.Attrs["gen_ai.usage.input_tokens"] != int64(42) || s.Attrs["ratio"] != 0.5 || s.Attrs["stream"] != true {
		t.Errorf("attrs %v", s.Attrs)
	}
	if arr, ok := s.Attrs["tags"].([]any); !ok || len(arr) != 2 || arr[1] != int64(2) {
		t.Errorf("array %v", s.Attrs["tags"])
	}
	if m, ok := s.Attrs["obj"].(map[string]any); !ok || m["k"] != "v" {
		t.Errorf("kvlist %v", s.Attrs["obj"])
	}
	if len(s.Events) != 1 || s.Events[0].Attrs["exception.message"] != "boom" || s.Resource["service.name"] != "py-app" {
		t.Errorf("events %v resource %v", s.Events, s.Resource)
	}
}

func TestRejects(t *testing.T) {
	h, _ := capture()
	for _, c := range []struct {
		ctype, enc string
		body       string
		code       int
	}{
		{"text/plain", "", "x", http.StatusUnsupportedMediaType},
		{"application/json", "", "{not json", http.StatusBadRequest},
		{"application/x-protobuf", "", "\xff\xff\xff", http.StatusBadRequest},
		{"application/json", "br", "{}", http.StatusBadRequest},
	} {
		rec := post(t, h, c.ctype, c.enc, []byte(c.body))
		if rec.Code != c.code {
			t.Errorf("%s/%s: status %d, want %d", c.ctype, c.enc, rec.Code, c.code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/traces", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/v1/traces", nil))
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("OPTIONS: %d", rec.Code)
	}
}

func TestEmptyRequest(t *testing.T) {
	called := false
	h := Handler(func([]model.Span) error { called = true; return nil })
	rec := post(t, h, "application/x-protobuf", "", nil)
	if rec.Code != 200 || called {
		t.Errorf("status %d called %v", rec.Code, called)
	}
}

func TestLangfusePath(t *testing.T) {
	h, got := capture()
	body, err := proto.Marshal(demo.Round(time.Unix(1_700_000_000, 0)))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/public/otel/v1/traces", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.SetBasicAuth("pk-lf-anything", "sk-lf-anything")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || len(*got) < 15 {
		t.Fatalf("status %d, %d spans: %s", rec.Code, len(*got), rec.Body)
	}
}

func TestLangfuseCheck(t *testing.T) {
	h, _ := capture()
	req := httptest.NewRequest(http.MethodGet, "/api/public/v2/observations?limit=1&fields=core", nil)
	req.SetBasicAuth("pk-lf-anything", "sk-lf-anything")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp struct {
		Data []any `json:"data"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &resp) != nil || resp.Data == nil {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/public/v2/observations", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", rec.Code)
	}
}
