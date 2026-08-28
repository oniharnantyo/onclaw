import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    // Unit tests live in src/**; tests/ holds Playwright specs (pnpm test:e2e).
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
