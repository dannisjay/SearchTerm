// Package web embeds the frontend assets into the binary.
package web

import "embed"

//go:embed static
var FS embed.FS
