/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The admin listener serves the build under /ui/ and the JSON API under /admin/
// on one origin (ADR 0029). In dev, Vite proxies /admin to a local `spyglass serve`.
export default defineConfig({
  base: '/ui/',
  plugins: [react()],
  build: {
    // The admin UI is served from the local admin listener, so one ~530 kB
    // (~160 kB gzipped) bundle is acceptable. If the bundle grows past this
    // limit, switch routes in src/router.tsx to lazy loading instead of
    // raising the limit again.
    chunkSizeWarningLimit: 700,
  },
  server: {
    proxy: {
      '/admin': 'http://localhost:9090',
    },
  },
  test: {
    setupFiles: ['./src/test-setup.ts'],
    environment: 'jsdom',
    testTimeout: 15_000,
  },
})
