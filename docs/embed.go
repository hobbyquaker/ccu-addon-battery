// Package docs embeds the API's OpenAPI document, served at api/v1/openapi.json; docs/api.md is
// its prose.
package docs

import _ "embed"

// OpenAPI is openapi.json.
//
//go:embed openapi.json
var OpenAPI []byte
