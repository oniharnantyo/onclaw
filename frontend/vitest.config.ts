import { defineConfig } from "vite-plus";

// Separate from vite.config.ts (which is a vite-plus config): tests run
// through plain vitest so `npm run test` stays independent of the app build.
export default defineConfig({
  resolve: {
    alias: {
      // Plain vitest reads neither vite.config.ts nor tsconfig paths; without
      // this every vendored `@/…` import fails under `npm test`.
      "@": `${import.meta.dirname}/src`,
    },
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
    // jsdom omits browser observer APIs the vendored registry uses; the setup
    // file stubs them as no-ops (test-only, never imported by the app entry).
    setupFiles: [`${import.meta.dirname}/src/test-setup.ts`],
  },
});
