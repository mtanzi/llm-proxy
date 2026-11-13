package cli

import (
	"reflect"
	"strings"
	"testing"
)

// The CLI owns its decode types, so drift from api/openapi.yaml is caught
// here, not by a user (ADR 0055, "Where the client gets its types").

func (s *spec) schema(name string) map[string]any {
	m, _ := s.doc["components"].(map[string]any)["schemas"].(map[string]any)[name].(map[string]any)
	return m
}

// resolve follows a $ref to a component schema, and picks the non-null branch
// of a oneOf.
func (s *spec) resolve(n map[string]any) map[string]any {
	if ref, ok := n["$ref"].(string); ok {
		return s.resolve(s.schema(strings.TrimPrefix(ref, "#/components/schemas/")))
	}
	if one, ok := n["oneOf"].([]any); ok {
		for _, b := range one {
			if bm, ok := b.(map[string]any); ok && bm["type"] != "null" {
				return s.resolve(bm)
			}
		}
	}
	return n
}

func (s *spec) props(n map[string]any) map[string]any {
	p, _ := s.resolve(n)["properties"].(map[string]any)
	return p
}

// walk resolves a dotted path of fields from a schema, or nil.
func (s *spec) walk(schema string, path string) map[string]any {
	n := s.schema(schema)
	for _, f := range strings.Split(path, ".") {
		next, ok := s.props(n)[f].(map[string]any)
		if !ok {
			return nil
		}
		n = next
	}
	return n
}

// checkStruct requires every json tag of typ to be a property of the schema,
// and recurses into nested structs and slices of structs.
func checkStruct(t *testing.T, s *spec, typ reflect.Type, node map[string]any, where string) {
	t.Helper()
	props := s.props(node)
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if f.Anonymous {
			checkStruct(t, s, f.Type, node, where)
			continue
		}
		if tag == "" || tag == "-" || !f.IsExported() {
			continue
		}
		p, ok := props[tag].(map[string]any)
		if !ok {
			t.Errorf("%s: the CLI reads %q, which the OpenAPI schema does not have", where, tag)
			continue
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice {
			ft = ft.Elem()
			if items, ok := s.resolve(p)["items"].(map[string]any); ok {
				p = items
			}
		}
		if ft.Kind() == reflect.Struct {
			checkStruct(t, s, ft, p, where+"."+tag)
		}
	}
}

func TestDecodeTypesMatchTheOpenAPISchemas(t *testing.T) {
	s := loadSpec(t)
	for name, typ := range map[string]reflect.Type{
		"CallSummary": reflect.TypeFor[callSummary](), "CallList": reflect.TypeFor[callList](),
		"CallDetail": reflect.TypeFor[callDetail](), "ToolUse": reflect.TypeFor[toolUse](), "ToolResult": reflect.TypeFor[toolResult](),
		"SessionSummary": reflect.TypeFor[sessionSummary](), "SessionList": reflect.TypeFor[sessionList](),
		"SessionDetail": reflect.TypeFor[sessionDetail](), "Agent": reflect.TypeFor[agent](), "Prompt": reflect.TypeFor[prompt](),
		"UsageBucket": reflect.TypeFor[usageBucket](), "UsageSeries": reflect.TypeFor[usageSeries](),
		"ConfigView": reflect.TypeFor[configView](), "ProviderView": reflect.TypeFor[providerView](),
		"PricingView": reflect.TypeFor[pricingView](), "Price": reflect.TypeFor[price](), "KeyView": reflect.TypeFor[keyView](),
		"Usage": reflect.TypeFor[usage](), "ErrorResponse": reflect.TypeFor[errorResponse](),
	} {
		if s.schema(name) == nil {
			t.Errorf("no schema %s in the OpenAPI document", name)
			continue
		}
		checkStruct(t, s, typ, s.schema(name), name)
	}
}

func TestColumnTablesNameRealAPIFields(t *testing.T) {
	s := loadSpec(t)
	for _, tb := range allTables() {
		if s.schema(tb.Schema) == nil {
			t.Errorf("%s: no schema %s", tb.ID, tb.Schema)
			continue
		}
		names := map[string]bool{}
		for _, c := range tb.Cols {
			if names[c.Name] {
				t.Errorf("%s: duplicate column %s", tb.ID, c.Name)
			}
			names[c.Name] = true
			if c.Desc == "" {
				t.Errorf("%s.%s: describe has no description", tb.ID, c.Name)
			}
			if len(c.API) == 0 && c.Name != "api_equivalent" {
				t.Errorf("%s.%s reads no API field", tb.ID, c.Name)
			}
			for _, p := range c.API {
				if s.walk(tb.Schema, p) == nil {
					t.Errorf("%s.%s reads %s.%s, which the OpenAPI schema does not have", tb.ID, c.Name, tb.Schema, p)
				}
			}
		}
	}
}

func TestListTablesHaveThreeToFourDefaultFields(t *testing.T) {
	for _, tb := range allTables() {
		if tb.ID != "calls" && tb.ID != "sessions" {
			continue
		}
		n := 0
		for _, c := range tb.Cols {
			if c.Deflt {
				n++
			}
		}
		if n < 3 || n > 4 {
			t.Errorf("%s has %d default fields, want 3 to 4 (AXI)", tb.ID, n)
		}
	}
}

// The endpoints and query parameters the CLI sends must exist in the spec.
func TestRequestsUseDocumentedEndpointsAndParameters(t *testing.T) {
	s := loadSpec(t)
	paths := s.doc["paths"].(map[string]any)
	params := func(path string) map[string]bool {
		op, _ := paths[path].(map[string]any)["get"].(map[string]any)
		if op == nil {
			t.Fatalf("the OpenAPI document has no GET %s", path)
		}
		out := map[string]bool{}
		list, _ := op["parameters"].([]any)
		for _, p := range list {
			pm := p.(map[string]any)
			if ref, ok := pm["$ref"].(string); ok {
				pm = s.doc["components"].(map[string]any)["parameters"].(map[string]any)[strings.TrimPrefix(ref, "#/components/parameters/")].(map[string]any)
			}
			out[pm["name"].(string)] = true
		}
		return out
	}
	check := func(path string, sent ...string) {
		t.Helper()
		have := params(path)
		for _, p := range sent {
			if !have[p] {
				t.Errorf("GET %s has no parameter %q in the OpenAPI document", path, p)
			}
		}
	}
	callParams, sessionParams := []string{"from", "to", "limit", "cursor"}, []string{"from", "to", "limit", "cursor"}
	for _, f := range callFilters {
		callParams = append(callParams, f.param)
	}
	for _, f := range sessionFilters {
		sessionParams = append(sessionParams, f.param)
	}
	check("/admin/calls", callParams...)
	check("/admin/sessions", sessionParams...)
	check("/admin/usage", "from", "to", "grain", "group_by")
	check("/admin/config")
	check("/admin/calls/{id}")
	check("/admin/sessions/{key}")
	groupBy := s.doc["components"].(map[string]any)["parameters"].(map[string]any)["GroupBy"].(map[string]any)["schema"].(map[string]any)["enum"].([]any)
	found := false
	for _, g := range groupBy {
		found = found || g == "session"
	}
	if !found {
		t.Error("/admin/usage group_by has no session value")
	}
}
