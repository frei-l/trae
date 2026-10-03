package shell

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// DefaultUIAddr is where `trae serve` serves the UI.
const DefaultUIAddr = "127.0.0.1:4380"

// Serve runs trae without a window: the receiver plus the UI on uiAddr for
// a browser. It returns on SIGINT or SIGTERM.
func Serve(opts Options, uiAddr string) error {
	opts.Web = true
	c, err := Open(opts)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.StartReceiver(); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", uiAddr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: c.Handler(), ReadHeaderTimeout: 10 * time.Second}
	fmt.Printf("trae %s\n  receiving  %s\n  ui         http://%s/\n  data       %s\n", opts.Version, c.Receiver().Endpoint, ln.Addr(), opts.DataDir)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
