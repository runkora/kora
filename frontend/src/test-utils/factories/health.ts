import type { HealthResponse } from '@runkora/shared/api';

/**
 * Creates a `HealthResponse` with sensible defaults.
 * Pass partial overrides to customise specific fields.
 */
export function createHealthResponse(overrides: Partial<HealthResponse> = {}): HealthResponse {
  return {
    status: 'healthy',
    service: 'orbit-identity',
    timestamp: new Date().toISOString(),
    ...overrides,
  };
}
