import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [react()],
  resolve: {
    // "@" alias mirrors vite.config.ts — vendored shadcn sources import via "@/…".
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    // Unit tests live in src/**; tests/ holds Playwright specs (pnpm test:e2e).
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
