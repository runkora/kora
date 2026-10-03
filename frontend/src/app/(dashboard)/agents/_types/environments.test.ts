import { describe, expect, it } from 'vitest';
import { environmentsKey, setDefaultEnvironment, toggleEnvironment } from '.';

describe('toggleEnvironment', () => {
  it('makes the first attached environment the default', () => {
    expect(toggleEnvironment([], 'a')).toEqual([{ environment_id: 'a', is_default: true }]);
  });

  it('attaches later environments without changing the default', () => {
    const next = toggleEnvironment([{ environment_id: 'a', is_default: true }], 'b');
    expect(next).toEqual([
      { environment_id: 'a', is_default: true },
      { environment_id: 'b', is_default: false },
    ]);
  });

  it('moves the default to the first remaining environment when the default is detached', () => {
    const next = toggleEnvironment(
      [
        { environment_id: 'a', is_default: true },
        { environment_id: 'b', is_default: false },
        { environment_id: 'c', is_default: false },
      ],
      'a',
    );
    expect(next).toEqual([
      { environment_id: 'b', is_default: true },
      { environment_id: 'c', is_default: false },
    ]);
  });

  it('detaches the last environment', () => {
    expect(toggleEnvironment([{ environment_id: 'a', is_default: true }], 'a')).toEqual([]);
  });
});

describe('setDefaultEnvironment', () => {
  it('leaves exactly one default', () => {
    const next = setDefaultEnvironment(
      [
        { environment_id: 'a', is_default: true },
        { environment_id: 'b', is_default: false },
      ],
      'b',
    );
    expect(next.filter((e) => e.is_default).map((e) => e.environment_id)).toEqual(['b']);
  });
});

describe('environmentsKey', () => {
  it('ignores order but not the default', () => {
    const a = { environment_id: 'a', is_default: true };
    const b = { environment_id: 'b', is_default: false };
    expect(environmentsKey([a, b])).toBe(environmentsKey([b, a]));
    expect(environmentsKey([a, b])).not.toBe(
      environmentsKey([
        { ...a, is_default: false },
        { ...b, is_default: true },
      ]),
    );
  });
});
