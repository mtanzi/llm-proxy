package core

import "testing"

func TestCostEachColumn(t *testing.T) {
	// Distinct prices per column so a mixed-up column changes the answer.
	p := Price{Input: 5_000_000, Output: 25_000_000, CacheRead: 500_000, CacheWrite5m: 6_250_000, CacheWrite1h: 10_000_000}
	tests := []struct {
		name string
		u    Usage
		want MicroUSD
	}{
		{"zero", Usage{}, 0},
		{"input only", Usage{InputUncached: 1_000_000}, 5_000_000},
		{"output only", Usage{Output: 1_000_000}, 25_000_000},
		{"cache read only", Usage{CacheRead: 1_000_000}, 500_000},
		{"cache write 5m only", Usage{CacheWrite5m: 1_000_000}, 6_250_000},
		{"cache write 1h only", Usage{CacheWrite1h: 1_000_000}, 10_000_000},
		{"reasoning is not billed again", Usage{Output: 1_000_000, Reasoning: 400_000}, 25_000_000},
		{"all columns", Usage{InputUncached: 1000, CacheRead: 2000, CacheWrite5m: 3000, CacheWrite1h: 4000, Output: 500}, 5_000 + 1_000 + 18_750 + 40_000 + 12_500},
	}
	for _, tt := range tests {
		if got := Cost(tt.u, p); got != tt.want {
			t.Errorf("%s: Cost = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestCostRoundsOnceHalfUp(t *testing.T) {
	p := Price{Input: 1, CacheRead: 1} // one micro-dollar per million tokens
	if got := Cost(Usage{InputUncached: 499_999}, p); got != 0 {
		t.Errorf("below half = %d, want 0", got)
	}
	if got := Cost(Usage{InputUncached: 500_000}, p); got != 1 {
		t.Errorf("half = %d, want 1", got)
	}
	// Two columns of 300k each: 0.6 micro-dollars summed first, then rounded once.
	if got := Cost(Usage{InputUncached: 300_000, CacheRead: 300_000}, p); got != 1 {
		t.Errorf("sum before rounding = %d, want 1", got)
	}
}
