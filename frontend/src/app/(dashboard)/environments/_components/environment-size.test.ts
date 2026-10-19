import { describe, expect, it } from 'vitest';
import type { EnvironmentSizes } from '@runkora/shared/api';
import {
  customError,
  presetsFor,
  sizeChoiceFor,
  sizeRequest,
  sizeSummary,
  toAmount,
} from './environment-size';

const SIZES: EnvironmentSizes = {
  default: { cpu: '2', memory: '4Gi', disk: '10Gi' },
  max: { cpu: '8', memory: '16Gi', disk: '50Gi' },
};

describe('toAmount', () => {
  it.each([
    ['cpu', '2', 2],
    ['cpu', '500m', 0.5],
    ['memory', '4Gi', 4],
    ['memory', '512Mi', 0.5],
    ['disk', '1Ti', 1024],
    ['memory', '2G', 2e9 / 2 ** 30],
  ] as const)('reads %s %s as %d', (key, quantity, amount) => {
    expect(toAmount(key, quantity)).toBeCloseTo(amount);
  });

  it('does not read a value it does not understand', () => {
    expect(toAmount('memory', 'lots')).toBeNaN();
    expect(toAmount('cpu', '2Gi')).toBeNaN();
  });
});

describe('presetsFor', () => {
  it('uses the default for Medium and the maximum for Large', () => {
    expect(presetsFor(SIZES)).toEqual({
      small: { cpu: 1, memory: 2, disk: 5 },
      medium: { cpu: 2, memory: 4, disk: 10 },
      large: { cpu: 8, memory: 16, disk: 50 },
    });
  });

  it('caps Small at the maximum', () => {
    const small = presetsFor({
      default: SIZES.default,
      max: { cpu: '500m', memory: '1Gi', disk: '2Gi' },
    }).small;
    expect(small).toEqual({ cpu: 0.5, memory: 1, disk: 2 });
  });
});

describe('sizeChoiceFor', () => {
  it('starts a new environment on Medium', () => {
    expect(sizeChoiceFor(undefined, SIZES).preset).toBe('medium');
  });

  it('matches a saved size to its preset, in any unit', () => {
    expect(sizeChoiceFor({ cpu: '8', memory: '16384Mi', disk: '50Gi' }, SIZES).preset).toBe(
      'large',
    );
    expect(sizeChoiceFor({ cpu: '1', memory: '2Gi', disk: '5Gi' }, SIZES).preset).toBe('small');
  });

  it('holds any other size as Custom', () => {
    expect(sizeChoiceFor({ cpu: '3', memory: '6Gi', disk: '20Gi' }, SIZES)).toMatchObject({
      preset: 'custom',
      custom: { cpu: '3', memory: '6', disk: '20' },
    });
  });
});

describe('customError', () => {
  const custom = (cpu: string) => ({
    preset: 'custom' as const,
    custom: { cpu, memory: '4', disk: '10' },
  });

  it('accepts an empty or valid value', () => {
    expect(customError(custom(''), SIZES, 'cpu')).toBeUndefined();
    expect(customError(custom('8'), SIZES, 'cpu')).toBeUndefined();
  });

  it('rejects zero, text and values above the maximum', () => {
    expect(customError(custom('0'), SIZES, 'cpu')).toBe('Enter a number greater than 0.');
    expect(customError(custom('abc'), SIZES, 'cpu')).toBe('Enter a number greater than 0.');
    expect(customError(custom('9'), SIZES, 'cpu')).toBe('CPU can be at most 8 cores.');
  });
});

describe('sizeRequest', () => {
  const choice = sizeChoiceFor(undefined, SIZES);

  it('sends the platform quantities for Medium and Large', () => {
    expect(sizeRequest(choice, SIZES)).toEqual(SIZES.default);
    expect(sizeRequest({ ...choice, preset: 'large' }, SIZES)).toEqual(SIZES.max);
  });

  it('sends Small and Custom in GiB, and leaves an empty Custom field to the default', () => {
    expect(sizeRequest({ ...choice, preset: 'small' }, SIZES)).toEqual({
      cpu: '1',
      memory: '2Gi',
      disk: '5Gi',
    });
    expect(
      sizeRequest({ preset: 'custom', custom: { cpu: ' 4 ', memory: '', disk: '20' } }, SIZES),
    ).toEqual({ cpu: '4', memory: '', disk: '20Gi' });
  });
});

describe('saved sizes', () => {
  const saved = { cpu: '333m', memory: '1000Mi', disk: '3G' };

  it('sends a saved size unchanged until a field is edited', () => {
    const choice = sizeChoiceFor(saved, SIZES);
    expect(sizeRequest(choice, SIZES)).toEqual(saved);
    expect(sizeRequest({ ...choice, custom: { ...choice.custom, cpu: '2' } }, SIZES)).toEqual({
      ...saved,
      cpu: '2',
    });
  });

  it('checks an untouched field by its saved quantity', () => {
    const sizes = { default: SIZES.default, max: { cpu: '8', memory: '1000Mi', disk: '50Gi' } };
    const choice = sizeChoiceFor(saved, sizes);
    expect(customError(choice, sizes, 'memory')).toBeUndefined();
    expect(
      customError({ ...choice, custom: { ...choice.custom, memory: '1' } }, sizes, 'memory'),
    ).toBe('Memory can be at most 0.98 GiB.');
  });
});

describe('precision', () => {
  it('reads exponents and large suffixes', () => {
    expect(toAmount('memory', '1e9')).toBeCloseTo(1e9 / 2 ** 30);
    expect(toAmount('disk', '1Pi')).toBe(2 ** 20);
  });

  it('matches a preset only exactly, and sends a near miss unchanged', () => {
    const near = { cpu: '1999m', memory: '4Gi', disk: '10Gi' };
    const choice = sizeChoiceFor(near, SIZES);
    expect(choice.preset).toBe('custom');
    expect(sizeRequest(choice, SIZES)).toEqual(near);
  });

  it('keeps Small within a maximum below it', () => {
    const sizes = { default: SIZES.default, max: { cpu: '8', memory: '500Mi', disk: '50Gi' } };
    const choice = { ...sizeChoiceFor(undefined, sizes), preset: 'small' as const };
    expect(toAmount('memory', sizeRequest(choice, sizes).memory)).toBeLessThanOrEqual(
      toAmount('memory', '500Mi'),
    );
  });

  it('keeps a saved quantity it cannot read without blocking the edit', () => {
    const saved = { cpu: '2', memory: '4Zi', disk: '10Gi' };
    const choice = sizeChoiceFor(saved, SIZES);
    expect(choice.custom.memory).toBe('');
    expect(customError(choice, SIZES, 'memory')).toBeUndefined();
    expect(sizeRequest(choice, SIZES)).toEqual(saved);
  });

  it('summarises an empty Custom field as the default', () => {
    const choice = {
      preset: 'custom' as const,
      custom: { cpu: '3', memory: '', disk: '20' },
    };
    expect(sizeSummary(choice, SIZES)).toBe('Custom: 3 cores · 4 GiB · 20 GiB');
  });
});
