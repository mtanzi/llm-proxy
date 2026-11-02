package postgres

import (
	"testing"
	"time"
)

func TestUntilNextRun(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 11, 2, h, m, 0, 0, time.UTC) }
	cases := []struct {
		name   string
		now    time.Time
		runAt  time.Duration
		jitter time.Duration
		want   time.Duration
	}{
		{"before_today", at(1, 0), 3 * time.Hour, 0, 2 * time.Hour},
		{"after_today_goes_to_tomorrow", at(4, 0), 3 * time.Hour, 0, 23 * time.Hour},
		{"exactly_now_goes_to_tomorrow", at(3, 0), 3 * time.Hour, 0, 24 * time.Hour},
		{"jitter_adds", at(1, 0), 3 * time.Hour, 10 * time.Minute, 2*time.Hour + 10*time.Minute},
		{"midnight", at(23, 0), 0, 0, time.Hour},
		{"other_zone_is_read_as_utc", at(1, 0).In(time.FixedZone("X", 5*3600)), 3 * time.Hour, 0, 2 * time.Hour},
	}
	for _, c := range cases {
		if got := untilNextRun(c.now, c.runAt, c.jitter); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
