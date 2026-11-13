package cli

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/mtanzi/spyglass/internal/toon"
)

// A column is one output field of a command: its CLI name, the API fields it
// reads, a unit, a description and a value function. A command's column table
// drives its default fields, --fields, the renames (cost_micro_usd to
// cost_usd) and `describe`. Because a table names its API fields, the spec
// check of the tests covers `describe` too.
type column[T any] struct {
	name  string
	api   []string // schema fields read, dotted from the table's schema
	unit  string
	desc  string
	deflt bool
	value func(*T) any
}

// colTable is the column table of one row type, read from one OpenAPI schema.
type colTable[T any] struct {
	schema string
	cols   []column[T]
}

// columnDoc and tableDoc are the type-erased view `describe` and the tests use.
type columnDoc struct {
	Name  string
	API   []string
	Unit  string
	Desc  string
	Deflt bool
}

type tableDoc struct {
	ID     string
	Schema string
	Type   reflect.Type
	Cols   []columnDoc
}

func (t colTable[T]) doc(id string) tableDoc {
	d := tableDoc{ID: id, Schema: t.schema, Type: reflect.TypeFor[T]()}
	for _, c := range t.cols {
		d.Cols = append(d.Cols, columnDoc{Name: c.name, API: c.api, Unit: c.unit, Desc: c.desc, Deflt: c.deflt})
	}
	return d
}

func (t colTable[T]) names() []string {
	out := make([]string, len(t.cols))
	for i, c := range t.cols {
		out[i] = c.name
	}
	return out
}

// pick selects the columns to show: the declared defaults, or exactly the
// --fields list when it is given.
func (t colTable[T]) pick(cmd, fields string) ([]column[T], error) {
	byName := map[string]column[T]{}
	for _, c := range t.cols {
		byName[c.name] = c
	}
	var out []column[T]
	if fields != "" {
		seen := map[string]bool{}
		for _, n := range strings.Split(fields, ",") {
			n = strings.TrimSpace(n)
			c, ok := byName[n]
			if !ok {
				return nil, &cmdError{code: 2, typ: "usage",
					msg:  fmt.Sprintf("--fields: %q is not a field of this command; valid fields: %s", n, strings.Join(t.names(), ", ")),
					help: []string{"spyglass describe"}}
			}
			if !seen[n] {
				seen[n] = true
				out = append(out, c)
			}
		}
		return out, nil
	}
	for _, c := range t.cols {
		if c.deflt {
			out = append(out, c)
		}
	}
	return out, nil
}

func fieldNames[T any](cols []column[T]) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.name
	}
	return out
}

// table renders rows in the given columns as a TOON table.
func tableOf[T any](cols []column[T], rows []T) toon.Table {
	t := toon.Table{Fields: fieldNames(cols), Rows: make([][]any, len(rows))}
	for i := range rows {
		cells := make([]any, len(cols))
		for j, c := range cols {
			cells[j] = c.value(&rows[i])
		}
		t.Rows[i] = cells
	}
	return t
}

// objectOf renders one row as a TOON object, one pair per column.
func objectOf[T any](cols []column[T], row *T) toon.Object {
	o := make(toon.Object, len(cols))
	for i, c := range cols {
		o[i] = toon.Pair{Key: c.name, Value: c.value(row)}
	}
	return o
}

// usageCols adds the token columns every table of usage shares. get returns
// the row's usage; the paths are relative to usagePath in the row's schema.
func usageCols[T any](usagePath string, get func(*T) usage, detail bool) []column[T] {
	p := func(fields ...string) []string {
		out := make([]string, len(fields))
		for i, f := range fields {
			out[i] = usagePath + "." + f
		}
		return out
	}
	cols := []column[T]{
		{name: "tokens_in", api: p("input_uncached", "cache_read", "cache_write_5m", "cache_write_1h"), unit: "tokens",
			desc: "all input tokens: uncached, cache read and cache write", value: func(r *T) any { return get(r).in() }},
		{name: "tokens_out", api: p("output"), unit: "tokens",
			desc: "output tokens, reasoning included", value: func(r *T) any { return get(r).Output }},
		{name: "cache_read_tokens", api: p("cache_read"), unit: "tokens",
			desc: "input tokens read from the prompt cache", value: func(r *T) any { return get(r).CacheRead }},
		{name: "cache_write_tokens", api: p("cache_write_5m", "cache_write_1h"), unit: "tokens",
			desc: "input tokens written to the prompt cache (5 minute and 1 hour)", value: func(r *T) any { return get(r).cacheWrite() }},
		{name: "reasoning_tokens", api: p("reasoning"), unit: "tokens",
			desc: "the part of the output tokens spent on reasoning", value: func(r *T) any { return get(r).Reasoning }},
	}
	if detail {
		cols = append(cols,
			column[T]{name: "input_uncached_tokens", api: p("input_uncached"), unit: "tokens",
				desc: "input tokens neither read from nor written to the cache", value: func(r *T) any { return get(r).InputUncached }},
			column[T]{name: "cache_write_5m_tokens", api: p("cache_write_5m"), unit: "tokens",
				desc: "input tokens written to the 5 minute cache", value: func(r *T) any { return get(r).CacheWrite5m }},
			column[T]{name: "cache_write_1h_tokens", api: p("cache_write_1h"), unit: "tokens",
				desc: "input tokens written to the 1 hour cache", value: func(r *T) any { return get(r).CacheWrite1h }},
		)
	}
	return cols
}

const costDesc = "cost in USD, rounded to four decimals; the text `unpriced` when no price matched (unknown is not zero)"
