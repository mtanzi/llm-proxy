package core

import (
	"context"
	"errors"
)

// ErrNotFound is returned by CallReader when a call or session does not exist.
var ErrNotFound = errors.New("core: not found")

// CallWriter receives finished calls from the recorder. It inserts only.
type CallWriter interface {
	// WriteBatch inserts the calls with their tool halves.
	// Idempotent on CallRecord.ID, atomic per batch. Returns after commit.
	WriteBatch(ctx context.Context, calls []CallRecord) error
}

// CallReader serves every admin view. All relational views are derived here.
// Lists order by (ts, id) descending. Missing items return ErrNotFound.
type CallReader interface {
	ListCalls(ctx context.Context, f CallFilter, p Page) ([]CallSummary, *Cursor, error)
	// CountCalls counts matching calls up to max; capped is true when more exist.
	CountCalls(ctx context.Context, f CallFilter, max int) (n int, capped bool, err error)
	GetCall(ctx context.Context, id ID) (CallDetail, error) // with its tool halves
	ListSessions(ctx context.Context, f SessionFilter, p Page) ([]SessionSummary, *Cursor, error)
	GetSession(ctx context.Context, key string) (SessionDetail, error) // agents, prompts, calls
	ListToolCalls(ctx context.Context, f ToolCallFilter, p Page) ([]ToolCall, *Cursor, error)
	ToolSummary(ctx context.Context, f ToolCallFilter) ([]ToolStat, error)
	Usage(ctx context.Context, q UsageQuery) ([]UsageBucket, error) // tokens, cost, latency percentiles
}
