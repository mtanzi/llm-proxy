import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// jsdom has no ResizeObserver, which TanStack Virtual needs.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver ??= ResizeObserverStub as unknown as typeof ResizeObserver

// Vitest globals are off, so Testing Library does not clean up by itself.
afterEach(cleanup)

// The router scrolls to top on navigation; jsdom does not implement it.
window.scrollTo = () => {}
