package config

import "github.com/mtanzi/spyglass/internal/core"

// View is the read-only, redacted picture of the config that GET /admin/config
// serves (ADR 0023). It holds providers, pricing and key names. It never holds
// a secret, a secret source, a key hash or the database URL.
type View struct {
	Providers []ProviderView `json:"providers"`
	Pricing   []PricingView  `json:"pricing"`
	Keys      []KeyView      `json:"keys"`
}

// ProviderView is a provider without its credential.
type ProviderView struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	BaseURL     string   `json:"base_url"`
	AuthMode    string   `json:"auth_mode"`
	IdleTimeout Duration `json:"idle_timeout"`
}

// PricingView is one price version. Prices are micro-USD per million tokens.
type PricingView struct {
	Provider string    `json:"provider"`
	Model    string    `json:"model"`
	From     Date      `json:"from"`
	Price    PriceView `json:"price"`
}

// PriceView holds the per-column prices of one price version in micro-USD per
// million tokens. Its keys match the config file's pricing keys.
type PriceView struct {
	Input        core.MicroUSD `json:"input"`
	Output       core.MicroUSD `json:"output"`
	CacheRead    core.MicroUSD `json:"cache_read"`
	CacheWrite5m core.MicroUSD `json:"cache_write_5m"`
	CacheWrite1h core.MicroUSD `json:"cache_write_1h"`
}

// KeyView is a key name and its disabled flag, never its hash.
type KeyView struct {
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
}

// View returns the redacted read-only view. The slices are copies.
func (c *Config) View() View {
	v := View{
		Providers: make([]ProviderView, len(c.Providers)),
		Pricing:   make([]PricingView, len(c.Pricing)),
		Keys:      make([]KeyView, len(c.Keys)),
	}
	for i, p := range c.Providers {
		v.Providers[i] = ProviderView{Name: p.Name, Kind: p.Kind, BaseURL: p.BaseURL, AuthMode: p.AuthMode, IdleTimeout: p.IdleTimeout}
	}
	for i, p := range c.Pricing {
		v.Pricing[i] = PricingView{Provider: p.Provider, Model: p.Model, From: p.From, Price: PriceView(p.Price())}
	}
	for i, k := range c.Keys {
		v.Keys[i] = KeyView{Name: k.Name, Disabled: k.Disabled}
	}
	return v
}
