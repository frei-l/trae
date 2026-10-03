// Package shell wires the receiver, the store and the UI together, for the
// app window (gui.go) and for a browser (serve.go).
package shell

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/frei-l/trae/internal/api"
	"github.com/frei-l/trae/internal/demo"
	"github.com/frei-l/trae/internal/genai"
	"github.com/frei-l/trae/internal/model"
	"github.com/frei-l/trae/internal/otlp"
	"github.com/frei-l/trae/internal/store"
	"github.com/frei-l/trae/internal/ui"
)

// DefaultOTLPAddr is where OTLP/HTTP exporters send by default.
const DefaultOTLPAddr = "127.0.0.1:4318"

// Options configure a Core.
type Options struct {
	DataDir  string
	OTLPAddr string
	Version  string
	Web      bool
}

// Core is one running trae: database, settings, API and OTLP receiver.
type Core struct {
	Store *store.Store
	API   *api.Server
	opts  Options

	mu     sync.Mutex
	status api.ReceiverStatus
	srv    *http.Server
	cancel context.CancelFunc
}

// Open opens the database in opts.DataDir.
func Open(opts Options) (*Core, error) {
	if opts.OTLPAddr == "" {
		opts.OTLPAddr = DefaultOTLPAddr
	}
	st, err := store.Open(filepath.Join(opts.DataDir, "traces.db"))
	if err != nil {
		return nil, err
	}
	settings, err := api.LoadSettings(filepath.Join(opts.DataDir, "settings.json"))
	if err != nil {
		st.Close()
		return nil, err
	}
	c := &Core{Store: st, opts: opts}
	c.status = api.ReceiverStatus{Addr: opts.OTLPAddr, Endpoint: endpoint(opts.OTLPAddr)}
	c.API = &api.Server{
		Store: st, Settings: settings, Receiver: c.Receiver, Assets: ui.Assets(),
		Version: opts.Version, DataDir: opts.DataDir, Web: opts.Web,
	}
	c.API.Demo = func() error {
		if !c.Receiver().Listening {
			return errors.New("the receiver isn't running, so samples can't be sent")
		}
		return demo.Send(c.Receiver().Endpoint, demo.Round(time.Now().Add(-80*time.Second)))
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go c.API.Sweep(ctx)
	return c, nil
}

func endpoint(addr string) string { return "http://" + addr + "/v1/traces" }

// Ingest normalizes spans and stores them.
func Ingest(st *store.Store) otlp.Sink {
	return func(spans []model.Span) error {
		for i := range spans {
			genai.Normalize(&spans[i])
		}
		return st.Insert(spans)
	}
}

// StartReceiver listens for OTLP/HTTP. Failing to listen (usually because
// another collector holds the port) is reported in the UI, not fatal.
func (c *Core) StartReceiver() error {
	ln, err := net.Listen("tcp", c.opts.OTLPAddr)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.status.Listening = false
		c.status.Error = err.Error()
		var op *net.OpError
		if errors.As(err, &op) {
			c.status.Error = fmt.Sprintf("can't listen on %s: %v", c.opts.OTLPAddr, op.Err)
		}
		return err
	}
	c.srv = &http.Server{Handler: otlp.Handler(Ingest(c.Store)), ReadHeaderTimeout: 10 * time.Second}
	c.status.Listening = true
	c.status.Error = ""
	go func() {
		if err := c.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("trae: receiver: %v", err)
			c.mu.Lock()
			c.status.Listening = false
			c.status.Error = err.Error()
			c.mu.Unlock()
		}
	}()
	return nil
}

// Receiver reports the receiver's state.
func (c *Core) Receiver() api.ReceiverStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// Handler is the UI handler.
func (c *Core) Handler() http.Handler { return c.API.Handler() }

// Close stops the receiver and closes the database.
func (c *Core) Close() {
	c.cancel()
	c.mu.Lock()
	srv := c.srv
	c.mu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		srv.Shutdown(ctx)
		cancel()
	}
	c.Store.Close()
}
