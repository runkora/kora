import { describe, it, expect, vi, afterEach } from 'vitest';

vi.mock('server-only', () => ({}));
vi.mock('./config', () => ({ config: { orbitAgentsUrl: 'http://agents' } }));

import { agentsClient, START_RUN_TIMEOUT_MS } from './agents-client';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function stubFetch() {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response(JSON.stringify({ id: 'run-1' }), { status: 201 })),
  );
  return vi.spyOn(AbortSignal, 'timeout');
}

describe('agentsClient upstream timeout', () => {
  it('keeps the 10s default for ordinary calls', async () => {
    const timeout = stubFetch();
    await agentsClient('tok').post('/api/v1/other', {});
    expect(timeout).toHaveBeenCalledWith(10_000);
  });

  it('outlasts an 85s pod roll when starting a run', async () => {
    const timeout = stubFetch();
    await agentsClient('tok').post('/api/v1/runs', {}, START_RUN_TIMEOUT_MS);
    expect(timeout).toHaveBeenCalledWith(START_RUN_TIMEOUT_MS);
    expect(START_RUN_TIMEOUT_MS).toBeGreaterThan(180_000);
  });
});
