package core

import "testing"

func TestToolStatErrorRate(t *testing.T) {
	if got := (ToolStat{}).ErrorRate(); got != 0 {
		t.Errorf("empty row rate = %v, want 0", got)
	}
	if got := (ToolStat{Count: 4, Errors: 1}).ErrorRate(); got != 0.25 {
		t.Errorf("rate = %v, want 0.25", got)
	}
}
