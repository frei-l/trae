// trae is a local trace viewer for AI apps. It receives OpenTelemetry
// traces over OTLP/HTTP on 127.0.0.1:4318, keeps them in SQLite and shows
// model calls, messages, tool calls and tokens in a desktop window.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/frei-l/trae/internal/demo"
	"github.com/frei-l/trae/internal/shell"
)

// version is the fallback for builds that mygo.json doesn't version: `go run`
// and `go build`. Packaged builds report mygo.json's version.
var version = "0.1.0-dev"

// icon is the app icon, which packaged builds take from mygo.json. Other
// builds set it at run time (see shell.Options.Icon).
//
//go:embed resources/icon.png
var icon []byte

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "trae:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `trae — local AI trace viewer

Usage:
  trae                 open the app (receives OTLP on 127.0.0.1:4318)
  trae serve           receiver + UI in your browser, no window
  trae demo            send sample AI traces to a running trae
  trae version

Run "trae <command> -h" for a command's flags.
`)
}

func run(args []string) error {
	// Name and version first: they decide the data directory, and a
	// packaged build reports the version mygo.json gave it.
	shell.Configure(version)
	cmd := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "", "app", "gui":
		if len(args) > 0 && (args[0] == "-v" || args[0] == "--version") {
			fmt.Println("trae", mygo.App.Version())
			return nil
		}
		fs := flag.NewFlagSet("trae", flag.ExitOnError)
		fs.Usage = usage
		otlpAddr := fs.String("otlp", shell.DefaultOTLPAddr, "OTLP/HTTP listen address")
		data := fs.String("data", "", "data directory (default: the app's user data directory)")
		fs.Parse(args)
		return shell.RunGUI(shell.Options{DataDir: *data, OTLPAddr: *otlpAddr, Version: mygo.App.Version(), Icon: icon})
	case "serve", "web":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		otlpAddr := fs.String("otlp", shell.DefaultOTLPAddr, "OTLP/HTTP listen address")
		uiAddr := fs.String("ui", shell.DefaultUIAddr, "UI listen address")
		data := fs.String("data", defaultDataDir(), "data directory")
		fs.Parse(args)
		return shell.Serve(shell.Options{DataDir: *data, OTLPAddr: *otlpAddr, Version: mygo.App.Version()}, *uiAddr)
	case "demo":
		fs := flag.NewFlagSet("demo", flag.ExitOnError)
		endpoint := fs.String("endpoint", "http://"+shell.DefaultOTLPAddr+"/v1/traces", "OTLP/HTTP traces endpoint")
		count := fs.Int("count", 1, "how many rounds of sample traces to send")
		every := fs.Duration("every", 0, "keep sending a round at this interval (e.g. 3s)")
		fs.Parse(args)
		return demo.Run(*endpoint, *count, *every)
	case "version":
		fmt.Println("trae", mygo.App.Version())
		return nil
	case "help":
		usage()
		return nil
	}
	usage()
	return fmt.Errorf("unknown command %q", cmd)
}

// defaultDataDir is the app's user data directory, so the window and
// `trae serve` share their traces.
func defaultDataDir() string {
	if dir, err := shell.DataDir(); err == nil {
		return dir
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "trae-data"
	}
	return filepath.Join(dir, mygo.App.Name())
}
