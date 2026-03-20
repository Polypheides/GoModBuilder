package internal

import (
	"embed"
	_ "embed"
)

// IconPNG is ALWAYS embedded for the GUI
//go:embed bin/icon.png
var IconPNG []byte

//fs embeds the entire tool suite
//go:embed bin/*
var fs embed.FS

func init() {
	getEmbeddedData = func(name string) ([]byte, error) {
		return fs.ReadFile("bin/" + name)
	}
}
