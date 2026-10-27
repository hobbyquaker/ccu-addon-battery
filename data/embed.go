// Package data holds the files the binary embeds: the battery table (batteries.json, CC0) and
// its schema. data/README.md describes the table; data/check.py checks it.
package data

import _ "embed"

// Batteries is batteries.json as shipped.
//
//go:embed batteries.json
var Batteries []byte
