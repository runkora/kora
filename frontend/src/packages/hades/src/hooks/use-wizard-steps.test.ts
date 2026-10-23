import { describe, it, expect, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useWizardSteps } from './use-wizard-steps';
import type { UseWizardStepsOptions } from './use-wizard-steps';

const base: UseWizardStepsOptions = {
  stepCount: 4,
  hints: [undefined, undefined, undefined, undefined],
};

function setup(options: Partial<UseWizardStepsOptions> = {}) {
  return renderHook(
    (props: Partial<UseWizardStepsOptions>) => useWizardSteps({ ...base, ...props }),
    {
      initialProps: options,
    },
  );
}

describe('useWizardSteps', () => {
  it('starts on the first step', () => {
    const { result } = setup();
    expect(result.current.step).toBe(0);
    expect(result.current.maxVisited).toBe(0);
    expect(result.current.isFirst).toBe(true);
    expect(result.current.isLast).toBe(false);
  });

  it('honours initialStep and initialMaxVisited', () => {
    const { result } = setup({ initialStep: 1 });
    expect(result.current.step).toBe(1);
    expect(result.current.maxVisited).toBe(1);
    const other = setup({ initialStep: 1, initialMaxVisited: 3 });
    expect(other.result.current.maxVisited).toBe(3);
  });

  it('moves with next and back and reports isLast', () => {
    const { result } = setup();
    act(() => result.current.next());
    act(() => result.current.next());
    act(() => result.current.next());
    expect(result.current.step).toBe(3);
    expect(result.current.isLast).toBe(true);
    act(() => result.current.back());
    expect(result.current.step).toBe(2);
  });

  it('only raises maxVisited', () => {
    const { result } = setup();
    act(() => result.current.goTo(2));
    act(() => result.current.goTo(0));
    expect(result.current.step).toBe(0);
    expect(result.current.maxVisited).toBe(2);
  });

  it('ignores navigation while locked and does not call onNavigate', () => {
    const onNavigate = vi.fn();
    const { result } = setup({ locked: true, onNavigate });
    act(() => result.current.goTo(2));
    expect(result.current.step).toBe(0);
    expect(onNavigate).not.toHaveBeenCalled();
  });

  it('calls onNavigate on accepted navigation', () => {
    const onNavigate = vi.fn();
    const { result } = setup({ onNavigate });
    act(() => result.current.goTo(1));
    act(() => result.current.back());
    expect(onNavigate).toHaveBeenCalledTimes(2);
  });

  it('uses the hint of the current step', () => {
    const { result } = setup({ hints: ['Fix step one.', undefined, 'Fix step three.', undefined] });
    expect(result.current.hint).toBe('Fix step one.');
    expect(result.current.nextDisabled).toBe(true);
    act(() => result.current.goTo(1));
    expect(result.current.hint).toBeUndefined();
    expect(result.current.nextDisabled).toBe(false);
  });

  it('shows the first earlier hint on the review step', () => {
    const hints = [undefined, 'Fix step two.', 'Fix step three.', undefined];
    const { result } = setup({ hints, reviewStep: 3 });
    act(() => result.current.goTo(3));
    expect(result.current.hint).toBe('Fix step two.');
    expect(result.current.nextDisabled).toBe(true);
  });

  it('uses only the review entry on the last step without reviewStep', () => {
    const { result } = setup({ hints: [undefined, 'Fix step two.', undefined, undefined] });
    act(() => result.current.goTo(3));
    expect(result.current.hint).toBeUndefined();
  });

  it('resets to the initial step and maxVisited', () => {
    const { result } = setup({ initialStep: 1 });
    act(() => result.current.goTo(3));
    act(() => result.current.reset());
    expect(result.current.step).toBe(1);
    expect(result.current.maxVisited).toBe(1);
  });

  it('clamps goTo to the valid range', () => {
    const { result } = setup();
    act(() => result.current.goTo(-1));
    expect(result.current.step).toBe(0);
    act(() => result.current.goTo(4));
    expect(result.current.step).toBe(3);
    expect(result.current.maxVisited).toBe(3);
    act(() => result.current.goTo(99));
    expect(result.current.step).toBe(3);
  });

  it('stays on the first step when going back from it', () => {
    const { result } = setup();
    act(() => result.current.back());
    expect(result.current.step).toBe(0);
    expect(result.current.maxVisited).toBe(0);
  });

  it('stays on the last step when going next from it', () => {
    const { result } = setup({ initialStep: 3 });
    act(() => result.current.next());
    expect(result.current.step).toBe(3);
    expect(result.current.maxVisited).toBe(3);
    expect(result.current.isLast).toBe(true);
  });

  it('accepts navigation once locked flips from true to false', () => {
    const onNavigate = vi.fn();
    const { result, rerender } = setup({ locked: true, onNavigate });
    act(() => result.current.goTo(2));
    expect(result.current.step).toBe(0);
    rerender({ locked: false, onNavigate });
    act(() => result.current.goTo(2));
    expect(result.current.step).toBe(2);
    expect(onNavigate).toHaveBeenCalledTimes(1);
  });

  it('does not call onNavigate on reset', () => {
    const onNavigate = vi.fn();
    const { result } = setup({ onNavigate });
    act(() => result.current.goTo(2));
    onNavigate.mockClear();
    act(() => result.current.reset());
    expect(onNavigate).not.toHaveBeenCalled();
  });
});
