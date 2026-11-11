// Fixtures for the overview dashboard tests.
import type { UsageBucket } from './dashboardData'

export function bucket(over: Partial<UsageBucket> = {}): UsageBucket {
  return {
    start: '2026-11-01T10:00:00Z',
    group: '',
    calls: 10,
    errors: 1,
    usage: { input_uncached: 60, cache_read: 40, cache_write_5m: 0, cache_write_1h: 0, output: 20, reasoning: 0 },
    cost_micro_usd: 1_000,
    unpriced_calls: 0,
    estimated_calls: 0,
    no_usage_calls: 0,
    latency_p50_ms: 100,
    latency_p95_ms: 400,
    latency_p99_ms: 900,
    cache_hit_ratio: 0.4,
    ...over,
  }
}
