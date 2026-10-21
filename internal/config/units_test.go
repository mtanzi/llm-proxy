package config

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	ok := map[string]time.Duration{
		"10m":   10 * time.Minute,
		"120s":  120 * time.Second,
		"400d":  400 * 24 * time.Hour,
		"2h":    2 * time.Hour,
		"250ms": 250 * time.Millisecond,
		"0s":    0,
	}
	for in, want := range ok {
		got, err := ParseDuration(in)
		if err != nil || got.Std() != want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "10", "m", "1.5h", "-5m", "10 m", "10M", "1h30m", "10w", "99999999999d"} {
		if _, err := ParseDuration(in); err == nil {
			t.Errorf("ParseDuration(%q) succeeded; want error", in)
		}
	}
}

func TestDurationString(t *testing.T) {
	for in, want := range map[string]string{"400d": "400d", "48h": "2d", "120s": "2m", "90s": "90s", "1500ms": "1500ms", "0s": "0s"} {
		d, err := ParseDuration(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := d.String(); got != want {
			t.Errorf("Duration(%q).String() = %q; want %q", in, got, want)
		}
	}
}

func TestParseSize(t *testing.T) {
	ok := map[string]Size{"32MiB": 32 << 20, "1GiB": 1 << 30, "64KiB": 64 << 10, "100B": 100, "100": 100, "0": 0}
	for in, want := range ok {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Errorf("ParseSize(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "MiB", "32MB", "32 MiB", "1.5MiB", "-1MiB", "32mib", "99999999999GiB"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) succeeded; want error", in)
		}
	}
	if got := Size(32 << 20).String(); got != "32MiB" {
		t.Errorf("String = %q; want 32MiB", got)
	}
}

func TestParseRate(t *testing.T) {
	ok := map[string]Rate{"5.00": 5_000_000, "5": 5_000_000, "0.50": 500_000, "6.25": 6_250_000, "0.000001": 1, "0": 0, "22.5": 22_500_000}
	for in, want := range ok {
		got, err := ParseRate(in)
		if err != nil || got != want {
			t.Errorf("ParseRate(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "-1", "-0.5", ".5", "5.", "1e3", "0.0000001", "abc", "1,5", "$5", "+5"} {
		if _, err := ParseRate(in); err == nil {
			t.Errorf("ParseRate(%q) succeeded; want error", in)
		}
	}
}

func TestParseDate(t *testing.T) {
	d, err := ParseDate("2026-10-01")
	if err != nil || d.String() != "2026-10-01" {
		t.Fatalf("ParseDate = %v, %v", d, err)
	}
	for _, in := range []string{"", "2026-13-01", "2026-02-30", "10/01/2026", "2026-10-01T00:00:00Z"} {
		if _, err := ParseDate(in); err == nil {
			t.Errorf("ParseDate(%q) succeeded; want error", in)
		}
	}
}
