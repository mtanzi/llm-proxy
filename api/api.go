// Package api embeds the OpenAPI document of the admin API. The handlers live
// in internal/api.
package api

import _ "embed"

// OpenAPIYAML is api/openapi.yaml, the contract the UI types are generated from.
//
//go:embed openapi.yaml
var OpenAPIYAML []byte
