// Package web holds the assets embedded into the vitrine binary: the
// frontend and the default configuration.
package web

import (
	"embed"
	"io/fs"
)

//go:embed public
var public embed.FS

//go:embed conf
var conf embed.FS

// Public returns the frontend files (css, js, images).
func Public() fs.FS {
	sub, err := fs.Sub(public, "public")
	if err != nil {
		panic(err)
	}
	return sub
}

// Conf returns the default configuration files.
func Conf() fs.FS {
	sub, err := fs.Sub(conf, "conf")
	if err != nil {
		panic(err)
	}
	return sub
}
