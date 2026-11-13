package toon

import (
	"math"
	"strings"
	"testing"
)

func enc(t *testing.T, o Object) string {
	t.Helper()
	got, err := Encode(o)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestEncodeGolden(t *testing.T) {
	tests := []struct {
		name string
		in   Object
		want string
	}{
		{"empty root", Object{}, ""},
		{"primitives", Object{{"id", 123}, {"name", "Ada"}, {"active", true}, {"none", nil}},
			"id: 123\nname: Ada\nactive: true\nnone: null"},
		{"nested object", Object{{"user", Object{{"id", 1}, {"profile", Object{{"name", "Ada"}}}}}},
			"user:\n  id: 1\n  profile:\n    name: Ada"},
		{"empty nested object", Object{{"a", Object{}}, {"b", 1}}, "a:\nb: 1"},
		{"scalar array", Object{{"tags", Array{"admin", "ops", "dev"}}}, "tags[3]: admin,ops,dev"},
		{"mixed scalar array", Object{{"v", Array{1, "x", true, nil}}}, "v[4]: 1,x,true,null"},
		{"empty array", Object{{"items", Array{}}}, "items[0]:"},
		{"list", Object{{"help", List{"spyglass calls list", "run: x, y"}}},
			"help[2]:\n  - spyglass calls list\n  - \"run: x, y\""},
		{"empty list", Object{{"help", List{}}}, "help[0]:"},
		{"tabular", Object{{"items", Table{
			Fields: []string{"sku", "qty", "price"},
			Rows:   [][]any{{"A1", 2, 9.99}, {"B2", 1, 14.5}},
		}}}, "items[2]{sku,qty,price}:\n  A1,2,9.99\n  B2,1,14.5"},
		{"empty table", Object{{"items", Table{Fields: []string{"a"}}}}, "items[0]:"},
		{"table in nested object", Object{{"a", Object{{"t", Table{Fields: []string{"x"}, Rows: [][]any{{1}}}}}}},
			"a:\n  t[1]{x}:\n    1"},
		{"key quoting", Object{{"my-key", 1}, {"a b", 2}, {"", 3}, {"9a", 4}, {"a.b_c", 5}},
			"\"my-key\": 1\n\"a b\": 2\n\"\": 3\n\"9a\": 4\na.b_c: 5"},
		{"table field quoting", Object{{"t", Table{Fields: []string{"a-b", "c"}, Rows: [][]any{{1, 2}}}}},
			"t[1]{\"a-b\",c}:\n  1,2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := enc(t, tc.in); got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestStringQuoting(t *testing.T) {
	tests := []struct{ in, want string }{
		{"hello", "hello"},
		{"hello world", "hello world"},
		{"héllo ✓", "héllo ✓"},
		{"", `""`},
		{" lead", `" lead"`},
		{"trail ", `"trail "`},
		{"true", `"true"`},
		{"false", `"false"`},
		{"null", `"null"`},
		{"42", `"42"`},
		{"-3.14", `"-3.14"`},
		{"1e-6", `"1e-6"`},
		{"05", `"05"`},
		{"-", `"-"`},
		{"-item", `"-item"`},
		{"a,b", `"a,b"`},
		{"a:b", `"a:b"`},
		{`say "hi"`, `"say \"hi\""`},
		{`back\slash`, `"back\\slash"`},
		{"[x]", `"[x]"`},
		{"{x}", `"{x}"`},
		{"line1\nline2", `"line1\nline2"`},
		{"tab\there", `"tab\there"`},
		{"cr\rx", `"cr\rx"`},
		{"nul\x00x", "nul\uFFFDx"},
		{"bad\xffbyte", "bad\uFFFDbyte"},
		{"v1.2.3", "v1.2.3"},
		{"0x1F", "0x1F"},
	}
	for _, tc := range tests {
		got := enc(t, Object{{"k", tc.in}})
		if want := "k: " + tc.want; got != want {
			t.Errorf("%q: got %s, want %s", tc.in, got, want)
		}
	}
}

func TestNumbers(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{int(-7), "-7"},
		{int64(9007199254740993), "9007199254740993"},
		{1.5, "1.5"},
		{9.812, "9.812"},
		{1e21, "1000000000000000000000"},
		{1e-7, "0.0000001"},
		{math.Copysign(0, -1), "0"},
		{math.NaN(), "null"},
		{math.Inf(1), "null"},
		{0.022, "0.022"},
	}
	for _, tc := range tests {
		if got := enc(t, Object{{"n", tc.in}}); got != "n: "+tc.want {
			t.Errorf("%v: got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEncodeErrors(t *testing.T) {
	if _, err := Encode(Object{{"a", struct{}{}}}); err == nil {
		t.Error("unsupported type: want an error")
	}
	if _, err := Encode(Object{{"a", Array{Object{}}}}); err == nil {
		t.Error("object inside an array: want an error")
	}
	_, err := Encode(Object{{"t", Table{Fields: []string{"a", "b"}, Rows: [][]any{{1}}}}})
	if err == nil || !strings.Contains(err.Error(), "row 0") {
		t.Errorf("short row: err = %v", err)
	}
}
