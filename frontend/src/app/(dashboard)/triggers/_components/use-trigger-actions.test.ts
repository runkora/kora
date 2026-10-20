import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

const api = vi.hoisted(() => ({
  deleteTrigger: vi.fn(),
  fireTrigger: vi.fn(),
  redeliverDelivery: vi.fn(),
  updateSource: vi.fn(),
  updateTrigger: vi.fn(),
}));
vi.mock('../_services/triggers-api', () => api);

import { makeTrigger } from '../_types/fixtures';
import { useTriggerActions } from './use-trigger-actions';

const row = makeTrigger({ sourceId: 'src-slack', name: 'Slack mentions' });

function setup() {
  const notify = vi.fn();
  const { result } = renderHook(() => useTriggerActions({ refresh: vi.fn(), notify }));
  return { rotate: result.current.rotateSecret, notify };
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('rotateSecret', () => {
  it('returns the generated secret when kora issues it', async () => {
    api.updateSource.mockResolvedValue({ source: { secret: 'whsec_new' }, error: null });
    const { rotate, notify } = setup();

    let secret: string | null = null;
    await act(async () => {
      secret = await rotate(row);
    });

    expect(api.updateSource).toHaveBeenCalledWith('src-slack', { rotate_secret: true });
    expect(secret).toBe('whsec_new');
    expect(notify).not.toHaveBeenCalled();
  });

  it('sends a provider-issued secret and resolves empty on success', async () => {
    api.updateSource.mockResolvedValue({ source: { id: 'src-slack' }, error: null });
    const { rotate, notify } = setup();

    let secret: string | null = null;
    await act(async () => {
      secret = await rotate(row, 'slack-secret');
    });

    expect(api.updateSource).toHaveBeenCalledWith('src-slack', {
      rotate_secret: true,
      signing_secret: 'slack-secret',
    });
    expect(secret).toBe('');
    expect(notify).not.toHaveBeenCalled();
  });

  it('resolves null and reports the error when a provider-issued secret is refused', async () => {
    api.updateSource.mockResolvedValue({ source: null, error: 'signing_secret is required' });
    const { rotate, notify } = setup();

    let secret: string | null = '';
    await act(async () => {
      secret = await rotate(row, ' ');
    });

    expect(secret).toBeNull();
    expect(notify).toHaveBeenCalledWith({
      ok: false,
      message: 'Slack mentions: signing_secret is required',
    });
  });
});
