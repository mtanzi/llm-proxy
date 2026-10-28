package core

import (
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

var testVersions = []PriceVersion{
	{Provider: "anthropic", Model: "m", From: day("2026-10-01"), Price: Price{Input: 5_000_000, Output: 25_000_000}},
	{Provider: "anthropic", Model: "m", From: day("2026-01-01"), Price: Price{Input: 3_000_000, Output: 15_000_000}},
	{Provider: "openai", Model: "m", From: day("2026-01-01"), Price: Price{Input: 1_000_000}},
}

func TestSelectPrice(t *testing.T) {
	tests := []struct {
		name, provider, model string
		ts                    time.Time
		want                  string // pricing version, empty for no match
	}{
		{"before first version", "anthropic", "m", day("2025-12-31").Add(23 * time.Hour), ""},
		{"first day starts at midnight UTC", "anthropic", "m", day("2026-01-01"), "2026-01-01"},
		{"inside first range", "anthropic", "m", day("2026-09-30").Add(23 * time.Hour), "2026-01-01"},
		{"second version starts", "anthropic", "m", day("2026-10-01"), "2026-10-01"},
		{"after last version", "anthropic", "m", day("2030-01-01"), "2026-10-01"},
		{"other provider same model", "openai", "m", day("2026-10-01"), "2026-01-01"},
		{"unknown model", "anthropic", "other", day("2026-10-01"), ""},
		{"unknown provider", "x", "m", day("2026-10-01"), ""},
	}
	for _, tt := range tests {
		v, ok := SelectPrice(testVersions, tt.provider, tt.model, tt.ts)
		if tt.want == "" {
			if ok {
				t.Errorf("%s: matched %s, want none", tt.name, v.Version())
			}
			continue
		}
		if !ok || v.Version() != tt.want {
			t.Errorf("%s: version = %q (found %v), want %q", tt.name, v.Version(), ok, tt.want)
		}
	}
}

func TestCostAtPriceChangeBetweenTwoCalls(t *testing.T) {
	u := Usage{InputUncached: 1_000_000, Output: 100_000}
	old, oldVer := CostAt(testVersions, "anthropic", "m", day("2026-09-30"), u)
	now, nowVer := CostAt(testVersions, "anthropic", "m", day("2026-10-01"), u)
	if old == nil || *old != 3_000_000+1_500_000 || oldVer != "2026-01-01" {
		t.Errorf("old call = %v %q, want 4500000 2026-01-01", old, oldVer)
	}
	if now == nil || *now != 5_000_000+2_500_000 || nowVer != "2026-10-01" {
		t.Errorf("new call = %v %q, want 7500000 2026-10-01", now, nowVer)
	}
}

func TestCostAtUnknownModelIsNilNotZero(t *testing.T) {
	c, ver := CostAt(testVersions, "anthropic", "unknown", day("2026-10-01"), Usage{InputUncached: 1000})
	if c != nil || ver != "" {
		t.Errorf("unknown model = %v %q, want nil and empty version", c, ver)
	}
	z, _ := CostAt(testVersions, "anthropic", "m", day("2026-10-01"), Usage{})
	if z == nil || *z != 0 {
		t.Errorf("known model with no tokens = %v, want 0 not nil", z)
	}
}

func TestCostDecimalExactness(t *testing.T) {
	// $0.30 per million tokens at 1 token: 0.0000003 USD rounds to 0 micro-USD.
	// 3_333_333 tokens at $0.30 per million is 999_999.9 micro-USD, rounded to 1_000_000.
	p := Price{CacheRead: 300_000}
	if got := Cost(Usage{CacheRead: 3_333_333}, p); got != 1_000_000 {
		t.Errorf("3333333 tokens = %d, want 1000000", got)
	}
	// Ten calls of 100k tokens at $3.00 per million sum to exactly $3.00 in one call.
	p = Price{Input: 3_000_000}
	if got := Cost(Usage{InputUncached: 1_000_000}, p); got != 3_000_000 {
		t.Errorf("1M tokens = %d, want 3000000", got)
	}
	if got := Cost(Usage{InputUncached: 100_000}, p); got != 300_000 {
		t.Errorf("100k tokens = %d, want 300000", got)
	}
}
