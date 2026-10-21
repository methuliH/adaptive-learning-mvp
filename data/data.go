// Package data embeds the sample question bank so the demo binary runs anywhere.
package data

import _ "embed"

//go:embed questions.json
var Questions []byte
