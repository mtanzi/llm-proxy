/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The admin listener serves the build under /ui/ and the JSON API under /admin/
// on one origin (ADR 0029). In dev, Vite proxies /admin to a local llm-proxy.
export default defineConfig({
  base: '/ui/',
  plugins: [react()],
  server: {
    proxy: {
      '/admin': 'http://localhost:9090',
    },
  },
  test: {
    environment: 'jsdom',
  },
})
