// Package api serves the UI: its static files and the JSON API it calls.
// The same handler backs the app window (through MyGo's mygo:// scheme)
// and `trae serve` in a browser.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/frei-l/trae/internal/store"
)

// ReceiverStatus describes the OTLP listener.
type ReceiverStatus struct {
	Addr      string `json:"addr"`
	Endpoint  string `json:"endpoint"`
	Listening bool   `json:"listening"`
	Error     string `json:"error,omitempty"`
}

// Server holds what the handlers need.
type Server struct {
	Store    *store.Store
	Settings *Settings
	Receiver func() ReceiverStatus
	Assets   fs.FS
	Version  string
	DataDir  string
	Web      bool // served to a browser rather than the app window
	// LongPoll bounds GET /api/changes?wait=1.
	LongPoll time.Duration
	// Demo sends sample traces through the receiver, when set.
	Demo func() error
}

// Handler returns the UI handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", noCache(http.FileServer(http.FS(s.Assets))))
	mux.HandleFunc("GET /boot.js", s.boot)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/traces", s.traces)
	mux.HandleFunc("GET /api/traces/{id}", s.trace)
	mux.HandleFunc("POST /api/traces/{id}/delete", s.deleteTrace)
	mux.HandleFunc("GET /api/spans/{trace}/{span}", s.span)
	mux.HandleFunc("GET /api/calls", s.calls)
	mux.HandleFunc("GET /api/facets", s.facets)
	mux.HandleFunc("GET /api/changes", s.changes)
	mux.HandleFunc("POST /api/clear", s.clear)
	mux.HandleFunc("POST /api/demo", s.demo)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("POST /api/settings", s.setSettings)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "no such endpoint")
	})
	return guard(mux)
}

// guard keeps other web pages out. The UI is served on localhost, which
// any page in a browser can reach: a Host check stops DNS rebinding, and
// requiring JSON on POSTs forces a CORS preflight, which this server never
// grants, on cross-origin writes such as /api/clear.
func guard(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !localHost(r.Host) {
			fail(w, http.StatusForbidden, "trae only answers on localhost")
			return
		}
		if r.Method == http.MethodPost {
			if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
				fail(w, http.StatusUnsupportedMediaType, "POST bodies must be application/json")
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

func localHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("trae: api: %v", err)
	}
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func failErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "not found")
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	fail(w, http.StatusInternalServerError, err.Error())
}

func (s *Server) boot(w http.ResponseWriter, r *http.Request) {
	p := s.Settings.Get()
	b, _ := json.Marshal(map[string]any{"theme": p.Theme, "textSize": p.TextSize, "web": s.Web, "version": s.Version})
	w.Header().Set("Content-Type", "text/javascript")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, "window.bootPrefs = %s;\n", b)
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	st, err := s.Store.Stats(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, map[string]any{
		"receiver": s.Receiver(),
		"version":  s.Version,
		"dataDir":  s.DataDir,
		"database": s.Store.Path(),
		"stats":    st,
		"seq":      s.Store.Seq(),
	})
}

func filter(r *http.Request) store.Filter {
	q := r.URL.Query()
	f := store.Filter{
		Query:   q.Get("q"),
		Service: q.Get("service"),
		Model:   q.Get("model"),
		Errors:  q.Get("status") == "error",
	}
	f.Before, _ = strconv.ParseInt(q.Get("before"), 10, 64)
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	return f
}

func (s *Server) traces(w http.ResponseWriter, r *http.Request) {
	page, err := s.Store.Traces(r.Context(), filter(r))
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, page)
}

func (s *Server) trace(w http.ResponseWriter, r *http.Request) {
	tr, err := s.Store.Trace(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, tr)
}

func (s *Server) deleteTrace(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Delete(r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) span(w http.ResponseWriter, r *http.Request) {
	sp, err := s.Store.Span(r.Context(), r.PathValue("trace"), r.PathValue("span"))
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, sp)
}

func (s *Server) calls(w http.ResponseWriter, r *http.Request) {
	page, err := s.Store.Calls(r.Context(), filter(r))
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, page)
}

func (s *Server) facets(w http.ResponseWriter, r *http.Request) {
	f, err := s.Store.Facets(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, f)
}

// changes answers with the current change counter. With wait=1 it holds
// the request until the counter passes `after`, for live updates.
func (s *Server) changes(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	seq := s.Store.Seq()
	if r.URL.Query().Get("wait") == "1" && seq <= after {
		d := s.LongPoll
		if d <= 0 {
			d = 25 * time.Second
		}
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		seq = s.Store.Wait(ctx, after)
	}
	reply(w, map[string]int64{"seq": seq})
}

func (s *Server) clear(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Clear(); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) demo(w http.ResponseWriter, r *http.Request) {
	if s.Demo == nil {
		fail(w, http.StatusNotImplemented, "samples are not available")
		return
	}
	if err := s.Demo(); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	reply(w, s.Settings.Get())
}

func (s *Server) setSettings(w http.ResponseWriter, r *http.Request) {
	p := s.Settings.Get()
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&p); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := s.Settings.Set(p)
	if err != nil {
		failErr(w, err)
		return
	}
	if p.RetentionDays > 0 {
		go s.Prune()
	}
	reply(w, p)
}

// Prune applies the retention setting once.
func (s *Server) Prune() {
	days := s.Settings.Get().RetentionDays
	if days <= 0 {
		return
	}
	if err := s.Store.Prune(time.Now().AddDate(0, 0, -days)); err != nil {
		log.Printf("trae: prune: %v", err)
	}
}

// Sweep prunes now and then hourly until ctx ends.
func (s *Server) Sweep(ctx context.Context) {
	s.Prune()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Prune()
		}
	}
}
