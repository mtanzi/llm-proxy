package storetest

import (
	"context"
	"testing"
)

var noBodiesCases = []testCase{
	{"no_stored_value_holds_body_text", func(t *testing.T, e *env) {
		g := e.gen
		use, result := g.ToolPair("toolu_1", "s1")
		e.write(t, use, result,
			g.Call(g.WithUse("toolu_2", "Edit"), g.WithResult("toolu_1", true), WithAgent("a1", "a0", "subagent")),
			g.Call(WithError(529, "upstream_overloaded")),
		)
		values, err := e.StoredValues(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(values) == 0 {
			t.Fatal("the scan reached no stored value")
		}
		for _, v := range values {
			if body, found := contains(v.Value, g.Bodies()); found {
				t.Errorf("%s.%s holds body text %q", v.Table, v.Column, body)
			}
		}
	}},
}
