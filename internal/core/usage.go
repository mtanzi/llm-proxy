package core

// MicroUSD is an exact money amount in millionths of a US dollar. It matches
// the six decimal places of the cost column, so no rounding drift or float
// error enters sums.
type MicroUSD int64

// Usage holds the token counts of one call in disjoint columns: each token is
// counted in exactly one of the billed columns.
type Usage struct {
	InputUncached int
	CacheRead     int
	CacheWrite5m  int
	CacheWrite1h  int
	Output        int // includes reasoning tokens
	Reasoning     int // informational: the part of Output spent on reasoning; never billed again
}

// Price is the price of one million tokens per usage column, in MicroUSD.
// $5.00 per million tokens is Price{Input: 5_000_000}.
type Price struct {
	Input        MicroUSD
	Output       MicroUSD
	CacheRead    MicroUSD
	CacheWrite5m MicroUSD
	CacheWrite1h MicroUSD
}

// Cost returns the cost of u at price p: the sum of each disjoint column times
// its price per million tokens. Columns are summed before dividing, then
// rounded half up to the nearest MicroUSD once. Reasoning is not billed on its own.
func Cost(u Usage, p Price) MicroUSD {
	const perMillion = 1_000_000
	sum := int64(u.InputUncached)*int64(p.Input) +
		int64(u.CacheRead)*int64(p.CacheRead) +
		int64(u.CacheWrite5m)*int64(p.CacheWrite5m) +
		int64(u.CacheWrite1h)*int64(p.CacheWrite1h) +
		int64(u.Output)*int64(p.Output)
	return MicroUSD((sum + perMillion/2) / perMillion)
}
