// Package toon is an encode-only subset of TOON, the compact output format of
// AXI agent-facing CLIs (ADR 0055). It targets TOON 3.3 (the
// @toon-format/spec 3.3.0 release, a working draft when this was written). A
// change of the spec is a change to this package, not to the commands.
//
// The encoder takes an explicit, ordered input model, not reflection: an
// Object is an ordered list of pairs, an Array holds scalars, and a Table is a
// field list plus rows. Go maps have no order and the output depends on
// field order. The package has no domain knowledge and imports only the
// standard library.
//
// Subset: objects, primitive values, arrays of scalars, and tabular arrays of
// objects with a shared field list. A List is an array of scalars in the
// expanded form of the spec's list arrays, one "- item" line each, for
// sentences that would otherwise sit on one long inline line. The delimiter is always a comma. Output
// has no trailing newline, as the spec says.
//
// Scalars are string, bool, nil, int, int64 and float64. A float64 prints in
// canonical decimal form (no exponent, no trailing zeros); NaN and the
// infinities print as null. In a string, a control character other than
// newline, carriage return and tab, and any invalid UTF-8, becomes U+FFFD,
// because the spec has no escape for them and the encoder must not fail on
// data it does not control.
package toon

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Pair is one key and value of an Object.
type Pair struct {
	Key   string
	Value any
}

// Object is an ordered list of key-value pairs. A value is a scalar, an
// Object, an Array or a Table.
type Object []Pair

// Array is an array of scalars.
type Array []any

// List is an array of scalars written one "- item" line each.
type List []any

// Table is a tabular array of objects that share a field list. Every row has
// one scalar per field.
type Table struct {
	Fields []string
	Rows   [][]any
}

const indent = "  "

// Encode renders a root object. It returns an error for a value outside the
// subset, or a table row whose width differs from its field list.
func Encode(root Object) (string, error) {
	var b strings.Builder
	if err := writeObject(&b, root, 0); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

func writeObject(b *strings.Builder, o Object, depth int) error {
	pad := strings.Repeat(indent, depth)
	for _, p := range o {
		key := quoteKey(p.Key)
		switch v := p.Value.(type) {
		case Object:
			b.WriteString(pad + key + ":\n")
			if err := writeObject(b, v, depth+1); err != nil {
				return err
			}
		case Array:
			cells, err := scalars(v)
			if err != nil {
				return fmt.Errorf("toon: key %q: %w", p.Key, err)
			}
			b.WriteString(pad + key + "[" + strconv.Itoa(len(v)) + "]:")
			if len(v) > 0 {
				b.WriteString(" " + strings.Join(cells, ","))
			}
			b.WriteString("\n")
		case List:
			cells, err := scalars(v)
			if err != nil {
				return fmt.Errorf("toon: key %q: %w", p.Key, err)
			}
			b.WriteString(pad + key + "[" + strconv.Itoa(len(v)) + "]:\n")
			for _, c := range cells {
				b.WriteString(pad + indent + "- " + c + "\n")
			}
		case Table:
			if len(v.Rows) == 0 {
				b.WriteString(pad + key + "[0]:\n")
				continue
			}
			fields := make([]string, len(v.Fields))
			for i, f := range v.Fields {
				fields[i] = quoteKey(f)
			}
			b.WriteString(pad + key + "[" + strconv.Itoa(len(v.Rows)) + "]{" + strings.Join(fields, ",") + "}:\n")
			for i, row := range v.Rows {
				if len(row) != len(v.Fields) {
					return fmt.Errorf("toon: key %q: row %d has %d cells for %d fields", p.Key, i, len(row), len(v.Fields))
				}
				cells, err := scalars(row)
				if err != nil {
					return fmt.Errorf("toon: key %q: row %d: %w", p.Key, i, err)
				}
				b.WriteString(pad + indent + strings.Join(cells, ",") + "\n")
			}
		default:
			s, err := scalar(v)
			if err != nil {
				return fmt.Errorf("toon: key %q: %w", p.Key, err)
			}
			b.WriteString(pad + key + ": " + s + "\n")
		}
	}
	return nil
}

func scalars(vs []any) ([]string, error) {
	out := make([]string, len(vs))
	for i, v := range vs {
		s, err := scalar(v)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

func scalar(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "null", nil
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return "null", nil
		}
		if x == 0 { // also -0
			return "0", nil
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case string:
		return quoteValue(x), nil
	default:
		return "", fmt.Errorf("unsupported value of type %T", v)
	}
}

// quoteValue returns s bare when it cannot be read as anything but a string,
// and quoted otherwise.
func quoteValue(s string) string {
	s = clean(s)
	if needsQuote(s) {
		return quote(s)
	}
	return s
}

func needsQuote(s string) bool {
	if s == "" || s != strings.TrimSpace(s) {
		return true
	}
	switch s {
	case "true", "false", "null":
		return true
	}
	if strings.HasPrefix(s, "-") || numeric(s) {
		return true
	}
	return strings.ContainsAny(s, ",:\"\\[]{}\n\r\t")
}

// numeric reports whether a decoder would read s as a number: an optional
// minus, digits, an optional fraction and an optional exponent, or a number
// with a leading zero such as 007.
func numeric(s string) bool {
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}
	t := strings.TrimPrefix(s, "-")
	return len(t) > 1 && t[0] == '0' && strings.Trim(t, "0123456789") == ""
}

func quoteKey(k string) string {
	k = clean(k)
	if keySafe(k) {
		return k
	}
	return quote(k)
}

func keySafe(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case i > 0 && (c >= '0' && c <= '9' || c == '.'):
		default:
			return false
		}
	}
	return true
}

func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// clean replaces invalid UTF-8 and the control characters the spec has no
// escape for.
func clean(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\uFFFD")
	}
	return strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\n' && r != '\r' && r != '\t') || r == 0x7f {
			return '\uFFFD'
		}
		return r
	}, s)
}
