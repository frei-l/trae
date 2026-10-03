package shell

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/frei-l/trae/internal/store"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func TestReceiveSamples(t *testing.T) {
	c, err := Open(Options{DataDir: t.TempDir(), OTLPAddr: freeAddr(t), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.StartReceiver(); err != nil {
		t.Fatal(err)
	}
	if !c.Receiver().Listening {
		t.Fatal("not listening")
	}
	if err := c.API.Demo(); err != nil {
		t.Fatal(err)
	}
	page, err := c.Store.Traces(context.Background(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Totals.Traces != 5 || page.Totals.Calls != 7 || page.Totals.Errors != 1 {
		t.Errorf("totals %+v", page.Totals)
	}
	names := map[string]bool{}
	for _, tr := range page.Traces {
		names[tr.Name] = true
	}
	for _, n := range []string{"explain-word", "writing-score", "invoke_agent refund-helper", "POST /api/ask"} {
		if !names[n] {
			t.Errorf("missing trace %q in %v", n, names)
		}
	}
}

func TestBusyPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	c, err := Open(Options{DataDir: t.TempDir(), OTLPAddr: ln.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.StartReceiver(); err == nil {
		t.Fatal("expected an error")
	}
	st := c.Receiver()
	if st.Listening || !strings.Contains(st.Error, "can't listen on") {
		t.Errorf("status %+v", st)
	}
	if err := c.API.Demo(); err == nil {
		t.Error("demo should fail without a receiver")
	}
}
