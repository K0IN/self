// Package models embeds the default model registry into the binary so a
// release needs no extra files besides the engine bundle.
package models

import _ "embed"

// Registry is the bundled registry.yml.
//
//go:embed registry.yml
var Registry []byte
