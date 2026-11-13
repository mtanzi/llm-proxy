package pgtest

import "testing"

func TestURLFromEnv(t *testing.T) {
	cases := []struct {
		name       string
		env        map[string]string
		want       string
		wantLegacy bool
	}{
		{"none", nil, "", false},
		{"new", map[string]string{EnvURL: "postgres://new"}, "postgres://new", false},
		{"legacy", map[string]string{LegacyEnvURL: "postgres://old"}, "postgres://old", true},
		{"both, new wins", map[string]string{EnvURL: "postgres://new", LegacyEnvURL: "postgres://old"}, "postgres://new", false},
	}
	for _, c := range cases {
		got, legacy := urlFromEnv(func(k string) string { return c.env[k] })
		if got != c.want || legacy != c.wantLegacy {
			t.Errorf("%s: urlFromEnv = %q, %v; want %q, %v", c.name, got, legacy, c.want, c.wantLegacy)
		}
	}
}
