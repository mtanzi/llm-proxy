import { expect, test } from 'vitest'
import { configKeys, configPricing, configProviders, formatPricePerMillion, isPassthrough } from './configData'
import type { ConfigView } from './configData'

const price = { input: 3_000_000, output: 15_000_000, cache_read: 300_000, cache_write_5m: 3_750_000, cache_write_1h: 6_000_000 }

test('prices show as dollars per million tokens, with two decimals at least', () => {
  expect(formatPricePerMillion(3_000_000)).toBe('$3.00')
  expect(formatPricePerMillion(300_000)).toBe('$0.30')
  expect(formatPricePerMillion(3_750_000)).toBe('$3.75')
  expect(formatPricePerMillion(12_500)).toBe('$0.0125')
  expect(formatPricePerMillion(0)).toBe('$0.00')
  expect(formatPricePerMillion(null)).toBe('unknown')
})

test('keys sort enabled first and keep only the name and the flag', () => {
  const view = {
    providers: [],
    pricing: [],
    keys: [
      { name: 'old', disabled: true, hash: 'deadbeef' },
      { name: 'zed', disabled: false },
      { name: 'abc', disabled: false },
    ],
  } as unknown as ConfigView
  const keys = configKeys(view)
  expect(keys).toEqual([
    { name: 'abc', disabled: false },
    { name: 'zed', disabled: false },
    { name: 'old', disabled: true },
  ])
  expect(Object.keys(keys[2])).toEqual(['name', 'disabled'])
})

test('providers and pricing copy only their named fields', () => {
  const view = {
    providers: [{ name: 'p', kind: 'anthropic', base_url: 'https://x', auth_mode: 'proxy_key', idle_timeout: '10m', api_key: 'sk-secret' }],
    pricing: [{ provider: 'p', model: 'm', from: '2026-01-01', price, secret: 'x' }],
    keys: [],
  } as unknown as ConfigView
  expect(Object.keys(configProviders(view)[0])).toEqual(['name', 'kind', 'base_url', 'auth_mode', 'idle_timeout'])
  expect(Object.keys(configPricing(view)[0])).toEqual(['provider', 'model', 'from', 'price'])
})

test('pricing sorts by provider and model, newest price first', () => {
  const view = {
    providers: [],
    keys: [],
    pricing: [
      { provider: 'p', model: 'b', from: '2026-01-01', price },
      { provider: 'p', model: 'a', from: '2026-01-01', price },
      { provider: 'p', model: 'a', from: '2026-06-01', price },
    ],
  } as ConfigView
  expect(configPricing(view).map((p) => `${p.model}@${p.from}`)).toEqual(['a@2026-06-01', 'a@2026-01-01', 'b@2026-01-01'])
})

test('only client_passthrough is a passthrough provider', () => {
  expect(isPassthrough({ auth_mode: 'client_passthrough' })).toBe(true)
  expect(isPassthrough({ auth_mode: 'proxy_key' })).toBe(false)
})
