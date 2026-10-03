import { afterEach, describe, expect, it, vi } from 'vitest';
import { deleteEnvironment, ENVIRONMENT_IN_USE_MESSAGE } from './environments-api';

function stubFetch(status: number, body: unknown = {}) {
  vi.stubGlobal(
    'fetch',
    vi
      .fn()
      .mockResolvedValue(new Response(status === 204 ? null : JSON.stringify(body), { status })),
  );
}

describe('deleteEnvironment', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('explains a 409 from an environment still attached to an agent', async () => {
    stubFetch(409, {
      code: 'conflict',
      message: 'environment is attached to an agent; detach it first',
    });
    expect(await deleteEnvironment('env-1')).toEqual({ error: ENVIRONMENT_IN_USE_MESSAGE });
  });

  it('passes other API errors through', async () => {
    stubFetch(404, { message: 'environment not found' });
    expect(await deleteEnvironment('env-1')).toEqual({ error: 'environment not found' });
  });

  it('succeeds on 204', async () => {
    stubFetch(204);
    expect(await deleteEnvironment('env-1')).toEqual({ error: null });
  });
});
