import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'path';

/**
 * Integration test config — MSW intercepts real fetch calls, jsdom renders
 * full component trees including server-component boundaries folded in.
 *
 * Covers: BFF route handlers, component HTTP state machines (loading/ok/error),
 *         sidebar session rendering.
 * Excludes: plain unit tests (*.test.ts?x) and the Hades package.
 */
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    include: ['src/**/*.integration.test.ts?(x)'],
    exclude: [
      '**/node_modules/**',
      'src/packages/**', // packages (e.g. Hades) own their vitest.config.ts and environment
    ],
    setupFiles: ['./src/test-utils/setup.ts'],
    globals: false,
    coverage: {
      provider: 'v8',
      reportsDirectory: './coverage/integration',
      reporter: ['text', 'json', 'html'],
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/**/*.test.{ts,tsx}',
        'src/**/*.integration.test.{ts,tsx}',
        'src/packages/**',
        'src/test-utils/**',
        'src/**/*.d.ts',
      ],
    },
  },
  resolve: {
    alias: {
      // Prevent `server-only` from throwing inside Vitest's Node runtime.
      'server-only': path.resolve(__dirname, './src/test-utils/server-only-shim.js'),
      // Workspace package — must come before the generic @orbit alias so that
      // Vite's first-match resolution picks the package src, not ./src/hades.
      '@runkora/hades': path.resolve(__dirname, './src/packages/hades/src/index.ts'),
      '@orbit/test-utils': path.resolve(__dirname, './src/test-utils'),
      // Mirror the tsconfig.json paths block so @orbit/* imports resolve.
      '@orbit': path.resolve(__dirname, './src'),
    },
  },
});
