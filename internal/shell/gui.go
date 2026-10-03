package shell

import (
	"log"

	"github.com/egoist/mygo"
)

// RunGUI runs trae as a desktop app: one window whose page is served from
// the UI handler through MyGo's mygo:// scheme, with the receiver running
// alongside in the same process.
func RunGUI(opts Options) error {
	app := mygo.App
	// The name sets the user data directory, so it comes first.
	app.SetName("trae")
	app.SetVersion(opts.Version)
	if !app.RequestSingleInstanceLock() {
		return nil
	}

	var core *Core
	var main *mygo.Window
	app.WhenReady(func() {
		if opts.DataDir == "" {
			dir, err := app.Path(mygo.PathUserData)
			if err != nil {
				log.Printf("trae: %v", err)
				app.Exit(1)
				return
			}
			opts.DataDir = dir
		}
		var err error
		core, err = Open(opts)
		if err != nil {
			log.Printf("trae: %v", err)
			app.Exit(1)
			return
		}
		if err := core.StartReceiver(); err != nil {
			// Shown in the window's header; the stored traces stay browsable.
			log.Printf("trae: %v", err)
		}
		if err := mygo.Protocol.Handle("mygo", core.Handler()); err != nil {
			log.Printf("trae: %v", err)
			app.Exit(1)
			return
		}
		app.SetMenu(menu())
		main = openWindow()
	})
	app.OnSecondInstance(func([]string, string) {
		if main == nil {
			return
		}
		if main.IsMinimized() {
			main.Restore()
		}
		main.Show()
		main.Focus()
	})
	app.OnActivate(func(hasVisible bool) {
		if !hasVisible && main != nil {
			main.Show()
		}
	})
	app.OnQuit(func() {
		if core != nil {
			core.Close()
		}
	})
	return app.Run()
}

func openWindow() *mygo.Window {
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:           "trae",
		URL:             "/",
		Width:           1200,
		Height:          780,
		MinWidth:        720,
		MinHeight:       480,
		Hidden:          true,
		TitleBarStyle:   mygo.TitleBarHiddenInset,
		BackgroundColor: "light-dark(#f4f4f6, #1a1a1e)",
		StateKey:        "main",
		Page:            mygo.PageOptions{DevTools: mygo.DevToolsEnabled},
	})
	win.OnReadyToShow(win.Show)
	return win
}

func menu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Role: mygo.RoleEditMenu},
		{Label: "View", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleReload},
			{Role: mygo.RoleToggleDevTools},
			mygo.Separator(),
			{Role: mygo.RoleResetZoom},
			{Role: mygo.RoleZoomIn},
			{Role: mygo.RoleZoomOut},
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Role: mygo.RoleWindowMenu},
	})
}
