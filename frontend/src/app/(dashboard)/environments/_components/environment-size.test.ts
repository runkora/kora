import { describe, expect, it } from 'vitest';
import type { EnvironmentSizes } from '@orbit/shared/api';
import { customError, presetsFor, sizeChoiceFor, sizeRequest, toAmount } from './environment-size';

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
    expect(sizeChoiceFor({ cpu: '3', memory: '6Gi', disk: '20Gi' }, SIZES)).toEqual({
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
