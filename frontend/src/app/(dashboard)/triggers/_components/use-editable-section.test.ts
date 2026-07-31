import { describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

import { useEditableSection } from './use-editable-section';

/** A save that only resolves when the test says so. */
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

describe('drafts', () => {
  it('reads the stored value until something is typed', () => {
    const { result } = renderHook(() => useEditableSection('stored', 't1', vi.fn()));

    expect(result.current.value).toBe('stored');
    expect(result.current.dirty).toBe(false);
  });

  it('drops a draft belonging to another trigger', () => {
    const { result, rerender } = renderHook(
      ({ stored, key }: { stored: string; key: string }) =>
        useEditableSection(stored, key, vi.fn()),
      { initialProps: { stored: 'one', key: 't1' } },
    );

    act(() => result.current.setValue('half-typed'));
    rerender({ stored: 'two', key: 't2' });

    expect(result.current.value).toBe('two');
    expect(result.current.dirty).toBe(false);
  });

  it('reports why a value cannot be saved', () => {
    const { result } = renderHook(() =>
      useEditableSection<string>('stored', 't1', vi.fn(), {
        validate: (value) => (value.trim() ? undefined : 'Needs a name'),
      }),
    );

    expect(result.current.blocker).toBeUndefined();
    act(() => result.current.setValue('  '));
    expect(result.current.blocker).toBe('Needs a name');
  });
});

describe('saving', () => {
  it('keeps an edit made while the save was in flight', async () => {
    const gate = deferred<string | null>();
    const onSave = vi.fn().mockReturnValue(gate.promise);
    const { result } = renderHook(() => useEditableSection<string>('stored', 't1', onSave));

    act(() => result.current.setValue('first'));
    act(() => result.current.save());
    // The person keeps typing while the request is out.
    act(() => result.current.setValue('second'));

    await act(async () => {
      gate.resolve(null);
      await gate.promise;
    });

    // The response is about 'first'; 'second' is the latest intent and survives.
    expect(result.current.value).toBe('second');
    expect(result.current.dirty).toBe(true);
  });

  it('clears the draft when it still matches what was saved', async () => {
    const onSave = vi.fn().mockResolvedValue(null);
    const { result, rerender } = renderHook(
      ({ stored }: { stored: string }) => useEditableSection(stored, 't1', onSave),
      { initialProps: { stored: 'stored' } },
    );

    act(() => result.current.setValue('edited'));
    await act(async () => {
      result.current.save();
    });

    expect(result.current.dirty).toBe(false);
    // The row has not refreshed yet, so the saved value is held rather than
    // flashing back to the old one and forward again.
    expect(result.current.value).toBe('edited');

    rerender({ stored: 'edited' });
    expect(result.current.value).toBe('edited');
  });

  it('stops holding the moment the row refreshes, whatever it refreshes to', async () => {
    const onSave = vi.fn().mockResolvedValue(null);
    const { result, rerender } = renderHook(
      ({ stored }: { stored: string }) => useEditableSection(stored, 't1', onSave),
      { initialProps: { stored: 'stored' } },
    );

    act(() => result.current.setValue('edited'));
    await act(async () => {
      result.current.save();
    });

    // The platform stored something of its own; the panel shows that, not the
    // value that was sent.
    rerender({ stored: 'edited-normalised' });
    expect(result.current.value).toBe('edited-normalised');
  });

  it('keeps the draft and the reason when the save fails', async () => {
    const onSave = vi.fn().mockResolvedValue('event_type is not valid');
    const { result } = renderHook(() => useEditableSection<string>('stored', 't1', onSave));

    act(() => result.current.setValue('edited'));
    await act(async () => {
      result.current.save();
    });

    expect(result.current.value).toBe('edited');
    expect(result.current.error).toBe('event_type is not valid');
    expect(result.current.dirty).toBe(true);
  });

  it('compares drafts by content, so an object draft can settle at all', async () => {
    const onSave = vi.fn().mockResolvedValue(null);
    const isEqual = (a: { cron: string }, b: { cron: string }) => a.cron === b.cron;
    const { result } = renderHook(() =>
      useEditableSection({ cron: '0 2 * * *' }, 't1', onSave, { isEqual }),
    );

    act(() => result.current.setValue({ cron: '*/15 * * * *' }));
    await act(async () => {
      result.current.save();
    });

    // A fresh object with the same content is the same draft, so it clears.
    expect(result.current.dirty).toBe(false);
  });
});
