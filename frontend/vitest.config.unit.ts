import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'path';

/**
 * Unit test config — fast, no network, no iron-session.
 *
 * Covers: pure functions, routing helpers, component render snapshots.
 * Excludes: integration test files (*.integration.test.ts?x) and the Hades
 * package (which owns its own vitest.config.ts and happy-dom environment).
 */
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    include: [
      'src/**/*.test.ts?(x)',
      'scripts/**/*.test.ts',
      'eslint/**/*.test.mjs',
      '.storybook/theme-preview/**/*.test.ts',
    ],
    exclude: [
      '**/node_modules/**',
      'src/**/*.integration.test.ts?(x)',
      'src/packages/**', // Hades owns its own vitest.config.ts and environment
    ],
    setupFiles: ['./src/test-utils/setup.ts'],
    globals: false,
    coverage: {
      provider: 'v8',
      reportsDirectory: './coverage/unit',
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
      // The subpath exports go first for the same reason: the bare alias would
      // otherwise swallow them.
      '@clear-route/hades/theme': path.resolve(
        __dirname,
        './src/packages/hades/src/theme/index.ts',
      ),
      '@clear-route/hades/manifest.json': path.resolve(
        __dirname,
        './src/packages/hades/manifest.json',
      ),
      '@clear-route/hades': path.resolve(__dirname, './src/packages/hades/src/index.ts'),
      '@orbit/test-utils': path.resolve(__dirname, './src/test-utils'),
      // Mirror the tsconfig.json paths block so @orbit/* imports resolve.
      '@orbit': path.resolve(__dirname, './src'),
    },
  },
});
