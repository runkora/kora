/**
 * Vitest setup for @orbit/terra component tests.
 *
 * Extends Vitest's `expect` with @testing-library/jest-dom matchers
 * (toBeInTheDocument, toHaveAttribute, toBeDisabled, etc.).
 *
 * RTL's automatic cleanup (afterEach) is registered by @testing-library/react
 * when it detects a global `afterEach` — which requires `globals: true` in the
 * Vitest config. See vitest.config.ts.
 */
import '@testing-library/jest-dom/vitest';
