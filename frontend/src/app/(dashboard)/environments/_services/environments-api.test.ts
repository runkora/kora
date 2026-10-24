import { afterEach, describe, expect, it, vi } from 'vitest';
import { deleteEnvironment } from './environments-api';

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
    expect(await deleteEnvironment('env-1')).toEqual({
      error:
        'This environment is attached to an agent. Detach it from every agent, then delete it.',
    });
  });

  it('says a declared environment is managed in git', async () => {
    stubFetch(409, { code: 'managed_by_resource', message: 'managed by a resource' });
    expect((await deleteEnvironment('env-1')).error).toMatch(/managed in git/);
  });

  it('says a locked tenant is managed in git', async () => {
    stubFetch(403, { code: 'api_authoring_disabled', message: 'authoring disabled' });
    expect((await deleteEnvironment('env-1')).error).toMatch(/tenant is managed in git/);
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
