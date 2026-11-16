/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // `make dev` runs the Go API here; the UI calls it on the same origin. ws: the web
    // terminal's websocket (/api/v1/terminal/socket) goes through the same proxy.
    proxy: { '/api': { target: 'http://127.0.0.1:8090', ws: true } },
  },
  build: {
    // Embedded into the hostisan binary (go:embed).
    outDir: '../hostisan/internal/webui/dist',
    emptyOutDir: true,
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.{ts,tsx}'],
    setupFiles: ['src/test/setup.ts'],
    // Several suites run at once while tracks are built in parallel, and the default
    // 5s per test is too tight on a loaded machine.
    testTimeout: 20000,
  },
})
