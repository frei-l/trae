// Package ui holds the frontend: plain HTML, CSS and JavaScript, embedded
// into the binary.
package ui

import (
	"embed"
	"io/fs"
	"os"
)

//go:embed web
var files embed.FS

// Assets returns the frontend files. With TRAE_DEV_ASSETS set to a
// directory they are read from disk on every request, so edits show on
// reload without rebuilding.
func Assets() fs.FS {
	if dir := os.Getenv("TRAE_DEV_ASSETS"); dir != "" {
		return os.DirFS(dir)
	}
	sub, err := fs.Sub(files, "web")
	if err != nil {
		panic(err)
	}
	return sub
}
