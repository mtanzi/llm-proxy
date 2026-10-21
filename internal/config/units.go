package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/mtanzi/llm-proxy/internal/core"
)

// Duration is a time.Duration written as a whole number and a unit: ms, s, m,
// h or d (24 hours), for example 10m, 120s, 400d.
type Duration time.Duration

var durationUnits = []struct {
	suffix string
	unit   time.Duration
}{
	{"ms", time.Millisecond},
	{"s", time.Second},
	{"m", time.Minute},
	{"h", time.Hour},
	{"d", 24 * time.Hour},
}

// ParseDuration parses s as a Duration.
func ParseDuration(s string) (Duration, error) {
	for _, u := range durationUnits {
		num, ok := strings.CutSuffix(s, u.suffix)
		if !ok {
			continue
		}
		// "ms" also ends in "s": a leftover letter means a longer unit matched.
		n, err := strconv.ParseUint(num, 10, 32)
		if err != nil || n > math.MaxInt64/uint64(u.unit) {
			continue
		}
		return Duration(time.Duration(n) * u.unit), nil
	}
	return 0, fmt.Errorf("invalid duration %q: want a whole number and a unit ms, s, m, h or d, for example 10m", s)
}

// String formats d with the largest unit that divides it exactly.
func (d Duration) String() string {
	v := time.Duration(d)
	if v == 0 {
		return "0s"
	}
	for i := len(durationUnits) - 1; i >= 0; i-- {
		u := durationUnits[i]
		if v%u.unit == 0 {
			return strconv.FormatInt(int64(v/u.unit), 10) + u.suffix
		}
	}
	return v.String()
}

// Std returns d as a time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// MarshalText implements encoding.TextMarshaler.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	s, err := scalar(n)
	if err != nil {
		return err
	}
	v, err := ParseDuration(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// Size is a byte count written as a whole number and an optional unit: B, KiB,
// MiB or GiB, for example 32MiB. A bare number is bytes.
type Size int64

var sizeUnits = []struct {
	suffix string
	unit   int64
}{
	{"GiB", 1 << 30},
	{"MiB", 1 << 20},
	{"KiB", 1 << 10},
	{"B", 1},
}

// ParseSize parses s as a Size.
func ParseSize(s string) (Size, error) {
	num, mult := s, int64(1)
	for _, u := range sizeUnits {
		if n, ok := strings.CutSuffix(s, u.suffix); ok {
			num, mult = n, u.unit
			break
		}
	}
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil || n < 0 || n > (1<<62)/mult {
		return 0, fmt.Errorf("invalid size %q: want a whole number and an optional unit B, KiB, MiB or GiB, for example 32MiB", s)
	}
	return Size(n * mult), nil
}

// String formats s with the largest unit that divides it exactly.
func (s Size) String() string {
	if s == 0 {
		return "0B"
	}
	for _, u := range sizeUnits {
		if int64(s)%u.unit == 0 {
			return strconv.FormatInt(int64(s)/u.unit, 10) + u.suffix
		}
	}
	return strconv.FormatInt(int64(s), 10) + "B"
}

// MarshalText implements encoding.TextMarshaler.
func (s Size) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// UnmarshalYAML implements yaml.Unmarshaler.
func (s *Size) UnmarshalYAML(n *yaml.Node) error {
	str, err := scalar(n)
	if err != nil {
		return err
	}
	v, err := ParseSize(str)
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// Rate is a price in US dollars per million tokens, written as a plain decimal
// with at most six places, for example 5.00 or 0.30. It is held as core.MicroUSD,
// so no float enters the cost sum.
type Rate core.MicroUSD

// ParseRate parses s as a Rate.
func ParseRate(s string) (Rate, error) {
	whole, frac, hasDot := strings.Cut(s, ".")
	bad := fmt.Errorf("invalid price %q: want a non-negative decimal with at most six places, for example 5.00", s)
	if whole == "" || (hasDot && frac == "") || len(frac) > 6 || !allDigits(whole) || !allDigits(frac) || len(whole) > 12 {
		return 0, bad
	}
	frac += strings.Repeat("0", 6-len(frac))
	w, err1 := strconv.ParseInt(whole, 10, 64)
	f, err2 := strconv.ParseInt(frac, 10, 64)
	if err1 != nil || err2 != nil {
		return 0, bad
	}
	return Rate(w*1_000_000 + f), nil
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// MicroUSD returns r as core.MicroUSD.
func (r Rate) MicroUSD() core.MicroUSD { return core.MicroUSD(r) }

// UnmarshalYAML implements yaml.Unmarshaler.
func (r *Rate) UnmarshalYAML(n *yaml.Node) error {
	s, err := scalar(n)
	if err != nil {
		return err
	}
	v, err := ParseRate(s)
	if err != nil {
		return err
	}
	*r = v
	return nil
}

// Date is a calendar day, written YYYY-MM-DD, in UTC.
type Date time.Time

const dateLayout = "2006-01-02"

// ParseDate parses s as a Date.
func ParseDate(s string) (Date, error) {
	t, err := time.ParseInLocation(dateLayout, s, time.UTC)
	if err != nil {
		return Date{}, fmt.Errorf("invalid date %q: want YYYY-MM-DD", s)
	}
	return Date(t), nil
}

// Time returns d as a time.Time.
func (d Date) Time() time.Time { return time.Time(d) }

// String formats d as YYYY-MM-DD.
func (d Date) String() string { return time.Time(d).Format(dateLayout) }

// MarshalText implements encoding.TextMarshaler.
func (d Date) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Date) UnmarshalYAML(n *yaml.Node) error {
	s, err := scalar(n)
	if err != nil {
		return err
	}
	v, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// scalar returns the text of a scalar node as written.
func scalar(n *yaml.Node) (string, error) {
	if n.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("line %d: want a scalar value", n.Line)
	}
	return n.Value, nil
}
