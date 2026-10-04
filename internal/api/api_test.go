package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/frei-l/trae/internal/model"
	"github.com/frei-l/trae/internal/store"
)

func setup(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	st, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := LoadSettings(t.TempDir() + "/settings.json")
	s := &Server{
		Store: st, Settings: settings, Version: "test", LongPoll: 2 * time.Second,
		Receiver: func() ReceiverStatus { return ReceiverStatus{Addr: "127.0.0.1:4318", Listening: true} },
		Assets:   fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>trae</title>")}},
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() { srv.Close(); st.Close() })
	return s, srv
}

func get(t *testing.T, url string, into any) int {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if into != nil {
		if err := json.NewDecoder(res.Body).Decode(into); err != nil {
			t.Fatalf("%s: %v", url, err)
		}
	}
	return res.StatusCode
}

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	res, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func span(trace, id string) model.Span {
	now := time.Now().UnixNano()
	return model.Span{TraceID: trace, SpanID: id, Name: "chat", Service: "svc", StartNs: now, EndNs: now + 1e9,
		Attrs: map[string]any{"k": "v"}, Resource: map[string]any{}, AI: model.AI{Kind: model.KindLLM, Model: "m", InTokens: 3}}
}

func TestEndpoints(t *testing.T) {
	s, srv := setup(t)
	s.Store.Insert([]model.Span{span("t1", "s1"), span("t2", "s2")})

	var page store.TracePage
	if code := get(t, srv.URL+"/api/traces?limit=1", &page); code != 200 || len(page.Traces) != 1 || page.Next == 0 || page.Totals.Traces != 2 {
		t.Errorf("traces %d %+v", code, page)
	}
	var tr store.Trace
	if code := get(t, srv.URL+"/api/traces/t1", &tr); code != 200 || len(tr.Spans) != 1 || tr.Spans[0].Attrs["k"] != "v" {
		t.Errorf("trace %d %+v", code, tr)
	}
	var e map[string]string
	if code := get(t, srv.URL+"/api/traces/nope", &e); code != 404 || e["error"] == "" {
		t.Errorf("missing trace %d %v", code, e)
	}
	var sp model.Span
	if code := get(t, srv.URL+"/api/spans/t1/s1", &sp); code != 200 || sp.AI.Model != "m" {
		t.Errorf("span %d %+v", code, sp)
	}
	var calls store.CallPage
	if get(t, srv.URL+"/api/calls?q=chat", &calls); len(calls.Calls) != 2 {
		t.Errorf("calls %+v", calls)
	}
	var facets store.Facets
	if get(t, srv.URL+"/api/facets", &facets); len(facets.Models) != 1 {
		t.Errorf("facets %+v", facets)
	}
	var status map[string]any
	if get(t, srv.URL+"/api/status", &status); status["version"] != "test" || status["receiver"].(map[string]any)["listening"] != true {
		t.Errorf("status %+v", status)
	}
	if res := postJSON(t, srv.URL+"/api/traces/t1/delete", "{}"); res.StatusCode != 204 {
		t.Errorf("delete %d", res.StatusCode)
	}
	if get(t, srv.URL+"/api/traces", &page); page.Totals.Traces != 1 {
		t.Errorf("after delete %+v", page.Totals)
	}
	if res := postJSON(t, srv.URL+"/api/clear", "{}"); res.StatusCode != 204 {
		t.Errorf("clear %d", res.StatusCode)
	}
	if code := get(t, srv.URL+"/api/nothing", &e); code != 404 {
		t.Errorf("unknown endpoint %d", code)
	}
	if res := postJSON(t, srv.URL+"/api/demo", "{}"); res.StatusCode != http.StatusNotImplemented {
		t.Errorf("demo without hook %d", res.StatusCode)
	}
}

func TestChangesLongPoll(t *testing.T) {
	s, srv := setup(t)
	var r map[string]int64
	get(t, srv.URL+"/api/changes", &r)
	start := r["seq"]
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.Store.Insert([]model.Span{span("t", "s")})
	}()
	began := time.Now()
	get(t, srv.URL+"/api/changes?wait=1&after="+itoa(start), &r)
	if r["seq"] <= start {
		t.Errorf("seq %d not past %d", r["seq"], start)
	}
	if time.Since(began) > time.Second {
		t.Errorf("long poll took %v", time.Since(began))
	}
	// With nothing new it holds until LongPoll, then answers the same seq.
	s.LongPoll = 60 * time.Millisecond
	get(t, srv.URL+"/api/changes?wait=1&after="+itoa(r["seq"]), &r)
	if r["seq"] != s.Store.Seq() {
		t.Errorf("idle poll %d", r["seq"])
	}
}

func TestChangesWakeOnPrefs(t *testing.T) {
	s, srv := setup(t)
	var applied []Prefs
	s.OnPrefs = func(p Prefs) { applied = append(applied, p) }
	var r map[string]int64
	get(t, srv.URL+"/api/changes", &r)
	go func() {
		time.Sleep(50 * time.Millisecond)
		p := s.Settings.Get()
		p.TextSize = 125
		s.UpdatePrefs(p)
	}()
	began := time.Now()
	get(t, srv.URL+"/api/changes?wait=1&after="+itoa(r["seq"])+"&prefs="+itoa(r["prefs"]), &r)
	if r["prefs"] != 1 || time.Since(began) > time.Second {
		t.Errorf("prefs %d after %v", r["prefs"], time.Since(began))
	}
	if len(applied) != 1 || applied[0].TextSize != 125 {
		t.Errorf("OnPrefs got %+v", applied)
	}
	// Saving the same prefs again is no change.
	s.UpdatePrefs(s.Settings.Get())
	if v := s.Settings.Version(); v != 1 {
		t.Errorf("version %d after an unchanged save", v)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestGuard(t *testing.T) {
	_, srv := setup(t)
	// A cross-site form post: text/plain needs no preflight.
	res, _ := http.Post(srv.URL+"/api/clear", "text/plain", strings.NewReader("{}"))
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain POST: %d", res.StatusCode)
	}
	// DNS rebinding: a foreign name pointing at 127.0.0.1.
	req, _ := http.NewRequest("GET", srv.URL+"/api/traces", nil)
	req.Host = "evil.example:4380"
	res, _ = http.DefaultClient.Do(req)
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("foreign host: %d", res.StatusCode)
	}
	for _, h := range []string{"localhost", "mygo.localhost", "127.0.0.1:4380", "[::1]:4380", "localhost:1"} {
		if !localHost(h) {
			t.Errorf("%s should be local", h)
		}
	}
	for _, h := range []string{"example.com", "10.0.0.1:80", "localhost.evil.com"} {
		if localHost(h) {
			t.Errorf("%s should not be local", h)
		}
	}
}

func TestSettingsAndBoot(t *testing.T) {
	s, srv := setup(t)
	var p Prefs
	get(t, srv.URL+"/api/settings", &p)
	if p.Theme != "system" || p.TextSize != 100 || p.RetentionDays != 7 {
		t.Errorf("defaults %+v", p)
	}
	res := postJSON(t, srv.URL+"/api/settings", `{"theme":"dark","textSize":999}`)
	json.NewDecoder(res.Body).Decode(&p)
	if p.Theme != "dark" || p.TextSize != 100 || p.RetentionDays != 7 {
		t.Errorf("saved %+v", p)
	}
	again, _ := LoadSettings(s.Settings.path)
	if again.Get().Theme != "dark" {
		t.Errorf("not persisted: %+v", again.Get())
	}
	r, _ := http.Get(srv.URL + "/boot.js")
	b, _ := io.ReadAll(r.Body)
	if !strings.Contains(string(b), `"theme":"dark"`) || r.Header.Get("Content-Type") != "text/javascript" {
		t.Errorf("boot.js %q", b)
	}
	r, _ = http.Get(srv.URL + "/")
	b, _ = io.ReadAll(r.Body)
	if !strings.Contains(string(b), "<title>trae</title>") {
		t.Errorf("index %q", b)
	}
}
