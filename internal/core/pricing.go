package core

import "time"

// PriceVersion is one price version of a provider's model. It applies from the
// start of the From day (UTC) until the next version of the same provider and
// model begins.
type PriceVersion struct {
	Provider string
	Model    string
	From     time.Time
	Price    Price
}

// Version returns the pricing_version of v: its From date as YYYY-MM-DD.
func (v PriceVersion) Version() string { return v.From.UTC().Format("2006-01-02") }

// SelectPrice returns the version in effect for provider and model at ts: the
// one with the latest From that is not after ts. It reports false when no
// version matches, for example for an unknown model or a call before the first
// version. The input order does not matter.
func SelectPrice(versions []PriceVersion, provider, model string, ts time.Time) (PriceVersion, bool) {
	var best PriceVersion
	found := false
	for _, v := range versions {
		if v.Provider != provider || v.Model != model || v.From.After(ts) {
			continue
		}
		if !found || v.From.After(best.From) {
			best, found = v, true
		}
	}
	return best, found
}

// CostAt prices u with the version in effect at ts. It returns a nil cost and
// an empty version when no version matches: an unknown cost is not zero.
func CostAt(versions []PriceVersion, provider, model string, ts time.Time, u Usage) (cost *MicroUSD, pricingVersion string) {
	v, ok := SelectPrice(versions, provider, model, ts)
	if !ok {
		return nil, ""
	}
	c := Cost(u, v.Price)
	return &c, v.Version()
}
