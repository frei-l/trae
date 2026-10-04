package shell

import (
	"log"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/frei-l/trae/internal/api"
)

// Configure names the app and sets its version, as MyGo expects. A packaged
// app (`mygo build`, `mygo dev`) already knows both from mygo.json, "trae",
// or "trae Dev" under `mygo dev`, and keeps them. A plain `go run` or
// `go build` binary would be named after its executable; it is named here,
// "trae Dev" unless MYGO_ENV=production, so that development never shares
// the installed app's data directory or single instance lock. fallback is
// the version of builds that have none of their own.
func Configure(fallback string) {
	app := mygo.App
	if !app.IsPackaged() {
		name := "trae"
		if mygo.IsDev() {
			name += " Dev"
		}
		app.SetName(name)
	}
	if app.Version() == "" {
		app.SetVersion(fallback)
	}
}

// DataDir is the app's user data directory, which the window and
// `trae serve` share.
func DataDir() (string, error) { return mygo.App.Path(mygo.PathUserData) }

// titleBarHeight is the height of the page's header, which the window's
// controls sit in (see .top in app.css).
const titleBarHeight = 46

// textSizes are the steps of View → Zoom In and Zoom Out, the same as the
// text sizes in Settings.
var textSizes = []int{100, 110, 125, 150}

// gui is trae as a desktop app: one window whose page is served from the UI
// handler through MyGo's mygo:// scheme, with the receiver running alongside
// in the same process. Its fields belong to the main thread, where MyGo
// calls the listeners that use them.
type gui struct {
	opts Options
	core *Core
	main *mygo.Window
}

// RunGUI runs the app until it quits. Call Configure first.
func RunGUI(opts Options) error {
	app := mygo.App
	if !app.RequestSingleInstanceLock() {
		return nil
	}
	g := &gui{opts: opts}
	app.WhenReady(g.ready)
	app.OnSecondInstance(func([]string, string) { g.reopen() })
	app.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			g.reopen()
		}
	})
	// On macOS an app keeps running with its Dock icon when its last window
	// closes, and trae keeps receiving traces; a click on the icon opens
	// the window again. Elsewhere, closing the window quits.
	app.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" {
			app.Quit()
		}
	})
	app.OnQuit(func() {
		if g.core != nil {
			g.core.Close()
		}
	})
	return app.Run()
}

func (g *gui) ready() {
	app := mygo.App
	if g.opts.DataDir == "" {
		dir, err := DataDir()
		if err != nil {
			log.Printf("trae: %v", err)
			app.Exit(1)
			return
		}
		g.opts.DataDir = dir
	}
	core, err := Open(g.opts)
	if err != nil {
		log.Printf("trae: %v", err)
		app.Exit(1)
		return
	}
	g.core = core
	core.API.OnPrefs = g.apply
	// The theme is set before the window opens, so it never shows the
	// other one first.
	g.apply(core.API.Settings.Get())
	if err := core.StartReceiver(); err != nil {
		// Shown in the window's header; the stored traces stay browsable.
		log.Printf("trae: %v", err)
	}
	if err := mygo.Protocol.Handle("mygo", core.Handler()); err != nil {
		log.Printf("trae: %v", err)
		app.Exit(1)
		return
	}
	app.SetMenu(g.menu())
	g.reopen()
}

// apply makes the native side follow the settings: the windows' and pages'
// appearance, and the pages' zoom, which is the text size. Native zoom,
// unlike CSS zoom, keeps the title bar insets right (MyGo resends them).
func (g *gui) apply(p api.Prefs) {
	switch p.Theme {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
	for _, w := range mygo.Windows() {
		if page := w.Page(); page != nil {
			page.SetZoomFactor(zoom(p))
		}
	}
}

func zoom(p api.Prefs) float64 { return float64(p.TextSize) / 100 }

// reopen shows the main window, creating it if it was closed.
func (g *gui) reopen() {
	if g.core == nil {
		return
	}
	if g.main == nil {
		g.main = g.newWindow()
		return
	}
	if g.main.IsMinimized() {
		g.main.Restore()
	}
	g.main.Show()
	g.main.Focus()
}

func (g *gui) newWindow() *mygo.Window {
	opts := mygo.WindowOptions{
		Title:           mygo.App.Name(),
		URL:             "/",
		Width:           1200,
		Height:          780,
		MinWidth:        720,
		MinHeight:       480,
		Hidden:          true,
		TitleBarStyle:   mygo.TitleBarHidden,
		TitleBarHeight:  titleBarHeight,
		BackgroundColor: "light-dark(#f4f4f6, #1a1a1e)",
		StateKey:        "main",
		// DevTools stays at its default: the inspector in development
		// builds only.
		Page: mygo.PageOptions{ZoomFactor: zoom(g.core.API.Settings.Get())},
	}
	if runtime.GOOS == "darwin" {
		// The traffic lights centered in the header: the close button is
		// 14 points tall.
		opts.TrafficLightPosition = &mygo.Point{X: 16, Y: (titleBarHeight - 14) / 2}
	}
	win := mygo.NewWindow(opts)
	win.OnReadyToShow(win.Show)
	win.OnClosed(func() {
		if g.main == win {
			g.main = nil
		}
	})
	return win
}

func (g *gui) menu() *mygo.Menu {
	view := []*mygo.MenuItem{{Role: mygo.RoleReload}}
	if mygo.IsDev() {
		view = append(view, &mygo.MenuItem{Role: mygo.RoleToggleDevTools})
	}
	// Zoom is the text size setting, so the menu and Settings agree and the
	// choice is kept.
	view = append(view,
		mygo.Separator(),
		&mygo.MenuItem{Label: "Actual Size", Accelerator: "CmdOrCtrl+0", Click: g.zoomTo(0)},
		&mygo.MenuItem{Label: "Zoom In", Accelerator: "CmdOrCtrl+=", Click: g.zoomTo(1)},
		&mygo.MenuItem{Label: "Zoom Out", Accelerator: "CmdOrCtrl+-", Click: g.zoomTo(-1)},
		mygo.Separator(),
		&mygo.MenuItem{Role: mygo.RoleToggleFullScreen},
	)
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Role: mygo.RoleEditMenu},
		{Label: "View", Submenu: view},
		{Role: mygo.RoleWindowMenu},
	})
}

// zoomTo returns a menu action that steps the text size: 0 resets it, 1 and
// -1 move to the next size up or down.
func (g *gui) zoomTo(step int) func(*mygo.MenuItem, *mygo.Window) {
	return func(*mygo.MenuItem, *mygo.Window) {
		p := g.core.API.Settings.Get()
		p.TextSize = stepTextSize(p.TextSize, step)
		if _, err := g.core.API.UpdatePrefs(p); err != nil {
			log.Printf("trae: %v", err)
		}
	}
}

func stepTextSize(cur, step int) int {
	if step == 0 {
		return textSizes[0]
	}
	i := 0
	for j, s := range textSizes {
		if s <= cur {
			i = j
		}
	}
	i += step
	return textSizes[max(0, min(len(textSizes)-1, i))]
}
