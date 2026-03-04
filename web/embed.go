package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded frontend filesystem rooted at "dist".
// Returns nil if the dist directory is empty (dev mode).
func DistFS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}
	// Check if it has any real files (ignore placeholder files used for source builds).
	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.Name() != ".gitkeep" {
			return sub
		}
	}
	if len(entries) == 0 {
		return nil
	}
	return nil
}
