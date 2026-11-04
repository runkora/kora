import { mergeConfig } from 'vitest/config';
import viteConfig from './vite.config';

/**
 * Vitest config for @runkora/hades.
 *
 * Extends the shared Vite config (react plugin + Tailwind CSS v4) so that
 * component tests run with the same transform pipeline used in Storybook.
 *
 * Environment: happy-dom — lighter than jsdom, sufficient for all Phase 1
 * targets (Button, Badge, Typography). None of these use Radix UI primitives
 * that require ResizeObserver or PointerEvent.
 *
 * If a future test file targets a Radix-based component (Popover, Tooltip,
 * DropdownMenu, etc.) that needs APIs happy-dom doesn't provide, add a
 * per-file override at the top of that file:
 *
 *   // @vitest-environment jsdom
 */
export default mergeConfig(viteConfig, {
  test: {
    environment: 'happy-dom',
    globals: true,
    setupFiles: ['./src/test-setup.ts'],
    include: ['src/**/*.test.ts?(x)', 'scripts/**/*.test.ts'],
    exclude: ['**/node_modules/**'],
  },
});
