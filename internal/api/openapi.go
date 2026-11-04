package api

import "go.yaml.in/yaml/v3"

// decodeYAML decodes a YAML document into values json.Marshal accepts: yaml v3
// gives string keys for the maps of an OpenAPI document.
func decodeYAML(b []byte) (any, error) {
	var v any
	if err := yaml.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}
