import type { ResponseOf } from '../api/client'
import { UNKNOWN } from '../lib/format'

// Pure helpers of the config page (ADR 0035). Each helper copies only the
// fields the page shows, so a field the API might add later (and a hash or a
// secret in particular) never reaches the screen by accident.

export type ConfigView = ResponseOf<'/admin/config'>
export type ProviderView = ConfigView['providers'][number]
export type PricingView = ConfigView['pricing'][number]
export type KeyView = ConfigView['keys'][number]

export function configProviders(config: ConfigView | undefined): ProviderView[] {
  return (config?.providers ?? [])
    .map((p) => ({ name: p.name, kind: p.kind, base_url: p.base_url, auth_mode: p.auth_mode, idle_timeout: p.idle_timeout }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

/** Prices by provider, model, then the day the price starts, newest first within a model. */
export function configPricing(config: ConfigView | undefined): PricingView[] {
  return (config?.pricing ?? [])
    .map((p) => ({
      provider: p.provider,
      model: p.model,
      from: p.from,
      price: {
        input: p.price.input,
        output: p.price.output,
        cache_read: p.price.cache_read,
        cache_write_5m: p.price.cache_write_5m,
        cache_write_1h: p.price.cache_write_1h,
      },
    }))
    .sort((a, b) => a.provider.localeCompare(b.provider) || a.model.localeCompare(b.model) || b.from.localeCompare(a.from))
}

/** Enabled keys first, then by name. Only the name and the disabled flag are kept. */
export function configKeys(config: ConfigView | undefined): KeyView[] {
  return (config?.keys ?? [])
    .map((k) => ({ name: k.name, disabled: k.disabled }))
    .sort((a, b) => Number(a.disabled) - Number(b.disabled) || a.name.localeCompare(b.name))
}

export function isPassthrough(provider: Pick<ProviderView, 'auth_mode'>): boolean {
  return provider.auth_mode === 'client_passthrough'
}

/** Micro-USD per million tokens as dollars per million tokens, e.g. 3000000 gives "$3.00". */
export function formatPricePerMillion(microUsd: number | null | undefined): string {
  if (microUsd == null) return UNKNOWN
  const [whole, fraction] = (microUsd / 1_000_000).toFixed(6).split('.')
  // Two decimals at least; more only where the price has them ($0.0125 stays $0.0125).
  return `$${whole}.${fraction.replace(/0+$/, '').padEnd(2, '0')}`
}
