package webui

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var assets embed.FS

// Static returns the embedded filesystem rooted at the static/ directory.
func Static() (fs.FS, error) {
	return fs.Sub(assets, "static")
}
