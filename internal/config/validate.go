package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
)

var keyHashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// validate checks every rule that needs no secret. It returns all problems.
func (c *Config) validate() []error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if err := validAddr(c.Listen.Data); err != nil {
		add("listen.data: %v", err)
	}
	if err := validAddr(c.Listen.Admin); err != nil {
		add("listen.admin: %v", err)
	} else if !IsLoopback(c.Listen.Admin) {
		add("listen.admin: %q is not a loopback address; the admin plane must listen on 127.0.0.1, ::1 or localhost", c.Listen.Admin)
	}
	if (c.Listen.TLS.CertFile == "") != (c.Listen.TLS.KeyFile == "") {
		add("listen.tls: set both cert_file and key_file, or neither")
	}

	if c.Database.MaxConns < 1 {
		add("database.max_conns: must be at least 1, got %d", c.Database.MaxConns)
	}

	errs = append(errs, c.validateProviders()...)
	errs = append(errs, c.validateKeys()...)
	errs = append(errs, c.validatePricing()...)

	if c.Retention.Calls <= 0 {
		add("retention.calls: must be greater than zero")
	}
	if c.Server.DrainTimeout <= 0 {
		add("server.drain_timeout: must be greater than zero")
	}
	if c.Server.MaxRequestBytes <= 0 {
		add("server.max_request_bytes: must be greater than zero")
	}
	if c.QueueCapacity < 1 {
		add("queue_capacity: must be at least 1, got %d", c.QueueCapacity)
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		add("log.level: unknown level %q; want debug, info, warn or error", c.Log.Level)
	}
	switch c.Log.Format {
	case "json", "text":
	default:
		add("log.format: unknown format %q; want json or text", c.Log.Format)
	}
	return errs
}

func (c *Config) validateProviders() []error {
	var errs []error
	add := func(i int, format string, args ...any) {
		errs = append(errs, fmt.Errorf("providers[%d]: %s", i, fmt.Sprintf(format, args...)))
	}
	seen := map[string]int{}
	for i, p := range c.Providers {
		switch {
		case p.Name == "":
			add(i, "name is required")
		case seen[p.Name] != 0:
			add(i, "duplicate name %q (also providers[%d])", p.Name, seen[p.Name]-1)
		default:
			seen[p.Name] = i + 1
		}
		switch p.Kind {
		case KindAnthropic, KindOpenAICompatible:
		default:
			add(i, "unknown kind %q; want %s or %s", p.Kind, KindAnthropic, KindOpenAICompatible)
		}
		if u, err := url.Parse(p.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			add(i, "base_url must be an absolute http or https URL without credentials")
		}
		switch p.AuthMode {
		case AuthProxyKey, AuthClientPassthrough:
		default:
			add(i, "unknown auth_mode %q; want %s or %s", p.AuthMode, AuthProxyKey, AuthClientPassthrough)
		}
		switch {
		case p.Credential.None && p.Kind != KindOpenAICompatible:
			add(i, "credential none is only valid for kind %s", KindOpenAICompatible)
		case p.Credential.None || !p.Credential.Ref.IsZero():
		case p.AuthMode != AuthClientPassthrough:
			add(i, "credential is required for auth_mode %s", p.AuthMode)
		}
		if p.IdleTimeout <= 0 {
			add(i, "idle_timeout must be greater than zero")
		}
		for h := range p.HeadersToAdd {
			if h == "" {
				add(i, "headers_to_add has an empty header name")
			}
		}
	}
	return errs
}

func (c *Config) validateKeys() []error {
	var errs []error
	add := func(i int, format string, args ...any) {
		errs = append(errs, fmt.Errorf("keys[%d]: %s", i, fmt.Sprintf(format, args...)))
	}
	names := map[string]int{}
	hashes := map[string]int{}
	for i, k := range c.Keys {
		if k.Name == "" {
			add(i, "name is required")
		} else if j, dup := names[k.Name]; dup {
			add(i, "duplicate name %q (also keys[%d])", k.Name, j)
		} else {
			names[k.Name] = i
		}
		if !keyHashRE.MatchString(k.SHA256) {
			add(i, "sha256 must be 64 lowercase hex characters")
		} else if j, dup := hashes[k.SHA256]; dup {
			add(i, "duplicate sha256 (also keys[%d])", j)
		} else {
			hashes[k.SHA256] = i
		}
	}
	return errs
}

func (c *Config) validatePricing() []error {
	var errs []error
	add := func(i int, format string, args ...any) {
		errs = append(errs, fmt.Errorf("pricing[%d]: %s", i, fmt.Sprintf(format, args...)))
	}
	type version struct{ provider, model, from string }
	providers := map[string]bool{}
	for _, p := range c.Providers {
		providers[p.Name] = true
	}
	seen := map[version]int{}
	for i, p := range c.Pricing {
		switch {
		case p.Provider == "":
			add(i, "provider is required")
		case !providers[p.Provider]:
			add(i, "provider %q is not declared in providers", p.Provider)
		}
		if p.Model == "" {
			add(i, "model is required")
		}
		if p.From.Time().IsZero() {
			add(i, "from is required (YYYY-MM-DD)")
			continue
		}
		v := version{p.Provider, p.Model, p.From.String()}
		if j, dup := seen[v]; dup {
			add(i, "overlaps pricing[%d]: same provider, model and from date %s", j, v.from)
		} else {
			seen[v] = i
		}
	}
	return errs
}

// validAddr checks host:port with a numeric port; the host may be empty.
func validAddr(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%q must be host:port", addr)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%q has an invalid port", addr)
	}
	return nil
}

// IsLoopback reports whether addr names only the loopback interface. An empty
// host listens on every interface and so is not loopback.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
