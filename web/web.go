// Package web embeds the one-page demo UI.
package web

import _ "embed"

//go:embed index.html
var Index []byte
