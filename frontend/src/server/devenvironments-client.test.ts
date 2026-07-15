import { describe, it, expect, vi } from 'vitest';

// devenvironments-client.ts imports @orbit/server/config, which eagerly reads
// required env vars at module-load time — mock it so this test can import the
// module without a real environment (mirrors session.integration.test.ts's
// strategy).
vi.mock('@orbit/server/config', () => ({
  config: { orbitApiUrl: 'http://test-orbit-api', devEnvironmentId: null },
}));

import {
  DEVENV_KUBECONFIG_TIMEOUT_MS,
  DEVENV_BACKEND_SIGN_TIMEOUT_SECONDS_DEFAULT,
} from './devenvironments-client';

describe('DEVENV_KUBECONFIG_TIMEOUT_MS', () => {
  it('comfortably exceeds the backend kubeconfig sign timeout budget', () => {
    // If the BFF's own abort fires before or at the same time the backend's
    // wait would time out, the caller only ever sees the BFF's generic abort
    // error and never the backend's own ErrKubeconfigSigningTimedOut
    // (with its Retry-After hint) — this assertion is what keeps that from
    // silently regressing if either constant changes independently.
    expect(DEVENV_KUBECONFIG_TIMEOUT_MS).toBeGreaterThan(
      DEVENV_BACKEND_SIGN_TIMEOUT_SECONDS_DEFAULT * 1000,
    );
  });
});
