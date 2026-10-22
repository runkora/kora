import { describe, it, expect, vi } from 'vitest';
import { loadScopeOptions } from './scope-options';

const vocab = {
  ok: true,
  data: {
    scopes: [
      {
        scope: 'agents:runs:read',
        resource_type: 'agents',
        path: ['runs'],
        description: 'Read runs',
      },
    ],
  },
};

describe('loadScopeOptions', () => {
  it('fetches the vocabulary when the caller holds platform:grants:read', async () => {
    const fetchVocab = vi.fn().mockResolvedValue(vocab);
    const out = await loadScopeOptions(['platform:grants:read', 'agents:runs:read'], fetchVocab);
    expect(fetchVocab).toHaveBeenCalledOnce();
    expect(out.find((o) => o.scope === 'agents:runs:read')?.description).toBe('Read runs');
  });

  it('counts a broader covering scope as permission, and a different action as none', async () => {
    const fetchVocab = vi.fn().mockResolvedValue(vocab);
    await loadScopeOptions(['platform:grants:write'], fetchVocab);
    expect(fetchVocab).not.toHaveBeenCalled();
    await loadScopeOptions(['platform:read'], fetchVocab);
    expect(fetchVocab).toHaveBeenCalledOnce();
  });

  it('does not fetch without permission and returns bare scopes', async () => {
    const fetchVocab = vi.fn();
    const out = await loadScopeOptions(['agents:runs:read'], fetchVocab);
    expect(fetchVocab).not.toHaveBeenCalled();
    expect(out).toEqual([{ scope: 'agents:runs:read' }]);
  });

  it('falls back to bare scopes when the fetch fails', async () => {
    const out = await loadScopeOptions(['platform:grants:read', 'agents:runs:read'], async () => ({
      ok: false,
    }));
    expect(out).toEqual([{ scope: 'platform:grants:read' }, { scope: 'agents:runs:read' }]);
  });
});
