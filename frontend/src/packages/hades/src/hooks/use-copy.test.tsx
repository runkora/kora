import { describe, it, expect, vi, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useCopy } from './use-copy';

function mockClipboard(writeText: () => Promise<void>) {
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
}

describe('useCopy', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('reports copied, then resets to idle', async () => {
    vi.useFakeTimers();
    const writeText = vi.fn().mockResolvedValue(undefined);
    mockClipboard(writeText);
    const { result } = renderHook(() => useCopy('abc'));

    await act(async () => {
      await result.current.copy();
    });
    expect(writeText).toHaveBeenCalledWith('abc');
    expect(result.current.status).toBe('copied');

    act(() => {
      vi.advanceTimersByTime(1500);
    });
    expect(result.current.status).toBe('idle');
  });

  it('reports failed when the clipboard write rejects', async () => {
    mockClipboard(vi.fn().mockRejectedValue(new Error('denied')));
    const { result } = renderHook(() => useCopy('abc'));

    await act(async () => {
      await result.current.copy();
    });
    expect(result.current.status).toBe('failed');
  });
});
