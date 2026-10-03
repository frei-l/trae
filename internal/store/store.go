// Package store keeps spans in a local SQLite database and answers the
// queries the UI needs.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/frei-l/trae/internal/model"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS spans (
	trace_id   TEXT NOT NULL,
	span_id    TEXT NOT NULL,
	parent_id  TEXT NOT NULL DEFAULT '',
	name       TEXT NOT NULL,
	service    TEXT NOT NULL,
	start_ns   INTEGER NOT NULL,
	end_ns     INTEGER NOT NULL,
	failed     INTEGER NOT NULL DEFAULT 0,
	ai_kind    TEXT NOT NULL,
	model      TEXT NOT NULL DEFAULT '',
	in_tok     INTEGER NOT NULL DEFAULT 0,
	out_tok    INTEGER NOT NULL DEFAULT 0,
	cost       REAL NOT NULL DEFAULT 0,
	session_id TEXT NOT NULL DEFAULT '',
	trace_name TEXT NOT NULL DEFAULT '',
	preview    TEXT NOT NULL DEFAULT '',
	data       BLOB NOT NULL,
	PRIMARY KEY (trace_id, span_id)
);
CREATE INDEX IF NOT EXISTS spans_start ON spans(start_ns);
CREATE INDEX IF NOT EXISTS spans_kind_start ON spans(ai_kind, start_ns);
CREATE TABLE IF NOT EXISTS traces (
	trace_id   TEXT PRIMARY KEY,
	name       TEXT NOT NULL,
	service    TEXT NOT NULL,
	start_ns   INTEGER NOT NULL,
	end_ns     INTEGER NOT NULL,
	span_count INTEGER NOT NULL,
	error_count INTEGER NOT NULL,
	llm_count  INTEGER NOT NULL,
	in_tok     INTEGER NOT NULL,
	out_tok    INTEGER NOT NULL,
	cost       REAL NOT NULL,
	models     TEXT NOT NULL,
	session_id TEXT NOT NULL,
	preview    TEXT NOT NULL,
	seq        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS traces_start ON traces(start_ns);
`

var memID atomic.Int64

// Store is the trace database.
type Store struct {
	db   *sql.DB
	path string
	wmu  sync.Mutex // serializes writers

	mu      sync.Mutex
	seq     int64
	waiters chan struct{} // closed and replaced on every change
}

// Open opens (creating if needed) the database at path. An empty path
// opens a private in-memory database, for tests.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:mem%d?mode=memory&cache=shared&_pragma=busy_timeout(5000)", memID.Add(1))
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		dsn = "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if path == "" {
		db.SetMaxOpenConns(1)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: schema: %w", err)
	}
	s := &Store{db: db, path: path, waiters: make(chan struct{})}
	if err := db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM traces`).Scan(&s.seq); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Path is the database file.
func (s *Store) Path() string { return s.path }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Insert stores spans (replacing any with the same ids) and refreshes the
// summaries of the traces they belong to.
func (s *Store) Insert(spans []model.Span) error {
	if len(spans) == 0 {
		return nil
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ins, err := tx.Prepare(`INSERT OR REPLACE INTO spans
		(trace_id, span_id, parent_id, name, service, start_ns, end_ns, failed, ai_kind, model, in_tok, out_tok, cost, session_id, trace_name, preview, data)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer ins.Close()
	traces := map[string]bool{}
	for i := range spans {
		sp := &spans[i]
		data, err := json.Marshal(sp)
		if err != nil {
			return err
		}
		if _, err := ins.Exec(sp.TraceID, sp.SpanID, sp.ParentID, sp.Name, sp.Service, sp.StartNs, sp.EndNs, sp.Failed(),
			sp.AI.Kind, sp.AI.Model, sp.AI.InTokens, sp.AI.OutTokens, sp.AI.Cost, sp.AI.SessionID, sp.AI.TraceName,
			Preview(sp), data); err != nil {
			return err
		}
		traces[sp.TraceID] = true
	}
	s.mu.Lock()
	seq := s.seq + 1
	s.mu.Unlock()
	for id := range traces {
		if err := summarize(tx, id, seq); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.bump(seq)
	return nil
}

// summarize recomputes the traces row of one trace from its spans.
func summarize(tx *sql.Tx, traceID string, seq int64) error {
	rows, err := tx.Query(`SELECT span_id, parent_id, name, service, start_ns, end_ns, failed, ai_kind, model, in_tok, out_tok, cost, session_id, trace_name, preview
		FROM spans WHERE trace_id = ? ORDER BY start_ns`, traceID)
	if err != nil {
		return err
	}
	type row struct {
		id, parent, name, service, kind, model, session, traceName, preview string
		start, end, in, out                                                 int64
		failed                                                              bool
		cost                                                                float64
	}
	var all []row
	ids := map[string]bool{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.parent, &r.name, &r.service, &r.start, &r.end, &r.failed, &r.kind, &r.model, &r.in, &r.out, &r.cost, &r.session, &r.traceName, &r.preview); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
		ids[r.id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(all) == 0 {
		return nil
	}
	t := model.TraceSummary{TraceID: traceID, StartNs: all[0].start, SpanCount: len(all)}
	var root *row
	models := map[string]bool{}
	for i := range all {
		r := &all[i]
		if r.parent == "" || !ids[r.parent] {
			if root == nil || r.parent == "" && root.parent != "" {
				root = r
			}
		}
		if r.end > t.EndNs {
			t.EndNs = r.end
		}
		if r.failed {
			t.ErrorCount++
		}
		if r.kind == model.KindLLM {
			t.LLMCount++
			// Agent and chain spans often repeat their children's usage;
			// only model calls count toward the trace's tokens.
			t.InTokens += r.in
			t.OutTokens += r.out
			t.Cost += r.cost
		}
		if r.model != "" {
			models[r.model] = true
		}
		if t.SessionID == "" {
			t.SessionID = r.session
		}
		if t.Name == "" && r.traceName != "" {
			t.Name = r.traceName
		}
	}
	if t.LLMCount == 0 {
		for _, r := range all {
			t.InTokens += r.in
			t.OutTokens += r.out
			t.Cost += r.cost
		}
	}
	if t.Name == "" {
		t.Name = root.name
	}
	t.Service = root.service
	t.Preview = root.preview
	if t.Preview == "" {
		for _, r := range all {
			if r.preview != "" && (r.kind == model.KindLLM || r.kind == model.KindAgent) {
				t.Preview = r.preview
				break
			}
		}
	}
	ms := make([]string, 0, len(models))
	for m := range models {
		ms = append(ms, m)
	}
	sort.Strings(ms)
	mj, _ := json.Marshal(ms)
	_, err = tx.Exec(`INSERT OR REPLACE INTO traces
		(trace_id, name, service, start_ns, end_ns, span_count, error_count, llm_count, in_tok, out_tok, cost, models, session_id, preview, seq)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.TraceID, t.Name, t.Service, t.StartNs, t.EndNs, t.SpanCount, t.ErrorCount, t.LLMCount, t.InTokens, t.OutTokens, t.Cost, string(mj), t.SessionID, t.Preview, seq)
	return err
}

// Preview is a one-line hint of what a span was about: the last user
// message of its input, or its first input text.
func Preview(sp *model.Span) string {
	pick := ""
	for _, m := range sp.AI.Input {
		for _, p := range m.Parts {
			if (p.Type == model.PartText || p.Type == model.PartValue) && strings.TrimSpace(p.Text) != "" {
				if m.Role == "user" || m.Role == "input" || pick == "" {
					pick = p.Text
				}
				break
			}
		}
	}
	pick = strings.Join(strings.Fields(readable(pick)), " ")
	if r := []rune(pick); len(r) > 160 {
		pick = string(r[:160]) + "…"
	}
	return pick
}

// readable turns a JSON object into "key: value · key: value" of its
// scalar fields, in their written order, and leaves other text alone.
func readable(text string) string {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "{") {
		return text
	}
	dec := json.NewDecoder(strings.NewReader(t))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return text
	}
	var parts []string
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return text
		}
		var v any
		if err := dec.Decode(&v); err != nil {
			return text
		}
		switch v.(type) {
		case string, json.Number, bool:
			parts = append(parts, fmt.Sprintf("%v: %v", k, v))
		}
	}
	if len(parts) == 0 {
		return text
	}
	return strings.Join(parts, " · ")
}

// Filter narrows trace and call lists.
type Filter struct {
	Query   string
	Service string
	Model   string
	Errors  bool
	Before  int64 // start_ns cursor, exclusive; 0 = newest
	Limit   int
}

func (f Filter) limit() int {
	if f.Limit <= 0 || f.Limit > 500 {
		return 100
	}
	return f.Limit
}

// Totals summarise a filtered list.
type Totals struct {
	Traces    int     `json:"traces"`
	Calls     int     `json:"llmCalls"`
	Errors    int     `json:"errors"`
	InTokens  int64   `json:"inTokens"`
	OutTokens int64   `json:"outTokens"`
	Cost      float64 `json:"cost"`
}

// TracePage is one page of the trace list.
type TracePage struct {
	Traces []model.TraceSummary `json:"traces"`
	Next   int64                `json:"next,omitempty"`
	Totals Totals               `json:"totals"`
	Seq    int64                `json:"seq"`
}

func like(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

func traceWhere(f Filter) (string, []any) {
	var conds []string
	var args []any
	if q := strings.TrimSpace(f.Query); q != "" {
		l := like(q)
		conds = append(conds, `(t.name LIKE ? ESCAPE '\' OR t.preview LIKE ? ESCAPE '\' OR t.models LIKE ? ESCAPE '\' OR t.trace_id LIKE ? ESCAPE '\' OR t.session_id LIKE ? ESCAPE '\'
			OR EXISTS (SELECT 1 FROM spans s WHERE s.trace_id = t.trace_id AND (s.name LIKE ? ESCAPE '\' OR s.preview LIKE ? ESCAPE '\')))`)
		args = append(args, l, l, l, l, l, l, l)
	}
	if f.Service != "" {
		conds = append(conds, `t.service = ?`)
		args = append(args, f.Service)
	}
	if f.Model != "" {
		conds = append(conds, `t.models LIKE ? ESCAPE '\'`)
		args = append(args, like(`"`+f.Model+`"`))
	}
	if f.Errors {
		conds = append(conds, `t.error_count > 0`)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// Traces lists trace summaries, newest first.
func (s *Store) Traces(ctx context.Context, f Filter) (*TracePage, error) {
	where, args := traceWhere(f)
	page := &TracePage{Traces: []model.TraceSummary{}, Seq: s.Seq()}
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(llm_count),0), COALESCE(SUM(error_count > 0),0), COALESCE(SUM(in_tok),0), COALESCE(SUM(out_tok),0), COALESCE(SUM(cost),0) FROM traces t`+where, args...).
		Scan(&page.Totals.Traces, &page.Totals.Calls, &page.Totals.Errors, &page.Totals.InTokens, &page.Totals.OutTokens, &page.Totals.Cost)
	if err != nil {
		return nil, err
	}
	if f.Before > 0 {
		if where == "" {
			where = " WHERE "
		} else {
			where += " AND "
		}
		where += "t.start_ns < ?"
		args = append(args, f.Before)
	}
	lim := f.limit()
	rows, err := s.db.QueryContext(ctx, `SELECT trace_id, name, service, start_ns, end_ns, span_count, error_count, llm_count, in_tok, out_tok, cost, models, session_id, preview, seq
		FROM traces t`+where+` ORDER BY start_ns DESC LIMIT ?`, append(args, lim+1)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t model.TraceSummary
		var models string
		if err := rows.Scan(&t.TraceID, &t.Name, &t.Service, &t.StartNs, &t.EndNs, &t.SpanCount, &t.ErrorCount, &t.LLMCount, &t.InTokens, &t.OutTokens, &t.Cost, &models, &t.SessionID, &t.Preview, &t.Seq); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(models), &t.Models)
		if t.Models == nil {
			t.Models = []string{}
		}
		page.Traces = append(page.Traces, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Traces) > lim {
		page.Traces = page.Traces[:lim]
		page.Next = page.Traces[lim-1].StartNs
	}
	return page, nil
}

// ErrNotFound is returned for unknown traces and spans.
var ErrNotFound = errors.New("not found")

// Trace is a trace with all its spans.
type Trace struct {
	Summary model.TraceSummary `json:"summary"`
	Spans   []model.Span       `json:"spans"`
}

// Trace returns one trace and its spans ordered by start time.
func (s *Store) Trace(ctx context.Context, id string) (*Trace, error) {
	page, err := s.traceSummary(ctx, id)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT data FROM spans WHERE trace_id = ? ORDER BY start_ns, span_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tr := &Trace{Summary: *page, Spans: []model.Span{}}
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var sp model.Span
		if err := json.Unmarshal(data, &sp); err != nil {
			return nil, err
		}
		tr.Spans = append(tr.Spans, sp)
	}
	return tr, rows.Err()
}

func (s *Store) traceSummary(ctx context.Context, id string) (*model.TraceSummary, error) {
	var t model.TraceSummary
	var models string
	err := s.db.QueryRowContext(ctx, `SELECT trace_id, name, service, start_ns, end_ns, span_count, error_count, llm_count, in_tok, out_tok, cost, models, session_id, preview, seq
		FROM traces WHERE trace_id = ?`, id).Scan(&t.TraceID, &t.Name, &t.Service, &t.StartNs, &t.EndNs, &t.SpanCount, &t.ErrorCount, &t.LLMCount, &t.InTokens, &t.OutTokens, &t.Cost, &models, &t.SessionID, &t.Preview, &t.Seq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(models), &t.Models)
	return &t, nil
}

// Span returns one span.
func (s *Store) Span(ctx context.Context, traceID, spanID string) (*model.Span, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT data FROM spans WHERE trace_id = ? AND span_id = ?`, traceID, spanID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var sp model.Span
	return &sp, json.Unmarshal(data, &sp)
}

// Call is one model call in the flat call ledger.
type Call struct {
	TraceID     string  `json:"traceId"`
	SpanID      string  `json:"spanId"`
	Name        string  `json:"name"`
	TraceName   string  `json:"traceName"`
	Service     string  `json:"service"`
	Kind        string  `json:"kind"`
	Model       string  `json:"model"`
	Provider    string  `json:"provider"`
	StartNs     int64   `json:"startNs"`
	EndNs       int64   `json:"endNs"`
	InTokens    int64   `json:"inTokens"`
	OutTokens   int64   `json:"outTokens"`
	CacheTokens int64   `json:"cacheTokens"`
	Cost        float64 `json:"cost"`
	TTFTNs      int64   `json:"ttftNs"`
	Failed      bool    `json:"failed"`
	StatusMsg   string  `json:"statusMessage,omitempty"`
	Preview     string  `json:"preview"`
}

// CallPage is one page of the call ledger.
type CallPage struct {
	Calls  []Call `json:"calls"`
	Next   int64  `json:"next,omitempty"`
	Totals Totals `json:"totals"`
	Seq    int64  `json:"seq"`
}

// Calls lists model calls (LLM and embedding spans), newest first.
func (s *Store) Calls(ctx context.Context, f Filter) (*CallPage, error) {
	conds := []string{`s.ai_kind IN ('llm', 'embedding')`}
	var args []any
	if q := strings.TrimSpace(f.Query); q != "" {
		l := like(q)
		conds = append(conds, `(s.name LIKE ? ESCAPE '\' OR s.model LIKE ? ESCAPE '\' OR s.preview LIKE ? ESCAPE '\' OR s.trace_id LIKE ? ESCAPE '\')`)
		args = append(args, l, l, l, l)
	}
	if f.Service != "" {
		conds = append(conds, `s.service = ?`)
		args = append(args, f.Service)
	}
	if f.Model != "" {
		conds = append(conds, `s.model = ?`)
		args = append(args, f.Model)
	}
	if f.Errors {
		conds = append(conds, `s.failed = 1`)
	}
	where := " WHERE " + strings.Join(conds, " AND ")
	page := &CallPage{Calls: []Call{}, Seq: s.Seq()}
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT trace_id), COUNT(*), COALESCE(SUM(failed),0), COALESCE(SUM(in_tok),0), COALESCE(SUM(out_tok),0), COALESCE(SUM(cost),0) FROM spans s`+where, args...).
		Scan(&page.Totals.Traces, &page.Totals.Calls, &page.Totals.Errors, &page.Totals.InTokens, &page.Totals.OutTokens, &page.Totals.Cost)
	if err != nil {
		return nil, err
	}
	if f.Before > 0 {
		where += " AND s.start_ns < ?"
		args = append(args, f.Before)
	}
	lim := f.limit()
	rows, err := s.db.QueryContext(ctx, `SELECT s.data, COALESCE(t.name, '') FROM spans s LEFT JOIN traces t ON t.trace_id = s.trace_id`+where+` ORDER BY s.start_ns DESC LIMIT ?`, append(args, lim+1)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		var traceName string
		if err := rows.Scan(&data, &traceName); err != nil {
			return nil, err
		}
		var sp model.Span
		if err := json.Unmarshal(data, &sp); err != nil {
			return nil, err
		}
		page.Calls = append(page.Calls, Call{
			TraceID: sp.TraceID, SpanID: sp.SpanID, Name: sp.Name, TraceName: traceName, Service: sp.Service,
			Kind: sp.AI.Kind, Model: sp.AI.Model, Provider: sp.AI.Provider, StartNs: sp.StartNs, EndNs: sp.EndNs,
			InTokens: sp.AI.InTokens, OutTokens: sp.AI.OutTokens, CacheTokens: sp.AI.CacheTokens, Cost: sp.AI.Cost,
			TTFTNs: sp.AI.TTFTNs, Failed: sp.Failed(), StatusMsg: sp.StatusMsg, Preview: Preview(&sp),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Calls) > lim {
		page.Calls = page.Calls[:lim]
		page.Next = page.Calls[lim-1].StartNs
	}
	return page, nil
}

// Facets are the values the list filters offer.
type Facets struct {
	Services []string `json:"services"`
	Models   []string `json:"models"`
}

// Facets returns every service and model seen.
func (s *Store) Facets(ctx context.Context) (*Facets, error) {
	f := &Facets{Services: []string{}, Models: []string{}}
	for _, q := range []struct {
		sql string
		out *[]string
	}{
		{`SELECT DISTINCT service FROM traces ORDER BY service`, &f.Services},
		{`SELECT DISTINCT model FROM spans WHERE model != '' ORDER BY model`, &f.Models},
	} {
		rows, err := s.db.QueryContext(ctx, q.sql)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return nil, err
			}
			*q.out = append(*q.out, v)
		}
		rows.Close()
	}
	return f, nil
}

// Stats counts what is stored.
type Stats struct {
	Traces int   `json:"traces"`
	Spans  int   `json:"spans"`
	Bytes  int64 `json:"bytes"`
}

// Stats returns counts and the database size.
func (s *Store) Stats(ctx context.Context) (*Stats, error) {
	st := &Stats{}
	if err := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM traces), (SELECT COUNT(*) FROM spans)`).Scan(&st.Traces, &st.Spans); err != nil {
		return nil, err
	}
	if s.path != "" {
		for _, p := range []string{s.path, s.path + "-wal"} {
			if fi, err := os.Stat(p); err == nil {
				st.Bytes += fi.Size()
			}
		}
	}
	return st, nil
}

// Delete removes one trace.
func (s *Store) Delete(id string) error {
	return s.deleteWhere(`trace_id = ?`, id)
}

// Clear removes every trace.
func (s *Store) Clear() error {
	if err := s.deleteWhere(`1 = 1`); err != nil {
		return err
	}
	_, err := s.db.Exec(`VACUUM`)
	return err
}

// Prune removes traces that ended before cutoff.
func (s *Store) Prune(cutoff time.Time) error {
	return s.deleteWhere(`end_ns < ?`, cutoff.UnixNano())
}

func (s *Store) deleteWhere(cond string, args ...any) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM spans WHERE trace_id IN (SELECT trace_id FROM traces WHERE `+cond+`)`, args...)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM traces WHERE `+cond, args...); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 || cond == "1 = 1" {
		s.mu.Lock()
		seq := s.seq + 1
		s.mu.Unlock()
		s.bump(seq)
	}
	return nil
}
