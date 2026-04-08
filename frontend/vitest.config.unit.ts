import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'path';

/**
 * Unit test config — fast, no network, no iron-session.
 *
 * Covers: pure functions, routing helpers, component render snapshots.
 * Excludes: integration test files (*.integration.test.ts?x) and the Terra
 * package (which owns its own vitest.config.ts and happy-dom environment).
 */
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts?(x)'],
    exclude: [
      '**/node_modules/**',
      'src/**/*.integration.test.ts?(x)',
      'src/packages/**', // Terra owns its own vitest.config.ts and environment
    ],
    setupFiles: ['./src/test-utils/setup.ts'],
    globals: false,
  },
  resolve: {
    alias: {
      // Prevent `server-only` from throwing inside Vitest's Node runtime.
      'server-only': path.resolve(__dirname, './src/test-utils/server-only-shim.js'),
      // Workspace package — must come before the generic @orbit alias so that
      // Vite's first-match resolution picks the package src, not ./src/terra.
      '@orbit/terra': path.resolve(__dirname, './src/packages/terra/src/index.ts'),
      '@orbit/test-utils': path.resolve(__dirname, './src/test-utils'),
      // Mirror the tsconfig.json paths block so @orbit/* imports resolve.
      '@orbit': path.resolve(__dirname, './src'),
    },
  },
});
