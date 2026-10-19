// @vitest-environment node
import { describe, expect, it } from 'vitest';

import { contrastRatio } from './contrast';

describe('contrastRatio', () => {
  it('computes the WCAG ratio for hex colours', () => {
    expect(contrastRatio('#000000', '#ffffff')).toBeCloseTo(21, 5);
    expect(contrastRatio('#fff', '#fff')).toBeCloseTo(1, 5);
    // #777 on white is the classic 4.48:1.
    expect(contrastRatio('#777777', '#ffffff')).toBeCloseTo(4.48, 2);
    expect(contrastRatio('#ffffff', '#777777')).toBeCloseTo(4.48, 2);
    expect(contrastRatio('#000000ff', '#ffffffff')).toBeCloseTo(21, 5);
  });

  it('reads oklch, including percentages, none and alpha', () => {
    expect(contrastRatio('oklch(0 0 0)', 'oklch(1 0 0)')).toBeCloseTo(21, 1);
    expect(contrastRatio('oklch(0% 0 0)', 'oklch(100% 0 0 / 0.5)')).toBeCloseTo(21, 1);
    expect(contrastRatio('oklch(0 none none)', 'oklch(1 0 0)')).toBeCloseTo(21, 1);
    // oklch(0.5 0 0) is mid grey, #636363 in sRGB.
    expect(contrastRatio('oklch(0.5 0 0)', '#636363')).toBeCloseTo(1, 1);
    expect(contrastRatio('oklch(0.5 0 0)', '#ffffff')).toBeCloseTo(
      contrastRatio('#636363', '#ffffff')!,
      1,
    );
  });

  it('is null for a value that is neither hex nor oklch', () => {
    expect(contrastRatio('red', '#fff')).toBeNull();
    expect(contrastRatio('#fff', 'rgb(0 0 0)')).toBeNull();
  });

  it('rejects malformed oklch values', () => {
    for (const bad of [
      'oklch(0 0)',
      'oklch(0 0 0 0)',
      'oklch()',
      'oklch(0 0 0 / )',
      'oklch(0 0 0 / 1 2)',
      'oklch(0 0/1 0)',
      'oklch(0 0 0',
      'oklch(0 0 0deg)',
      'OKLCH(0 0 0)',
    ]) {
      expect(contrastRatio(bad, '#fff'), bad).toBeNull();
    }
    expect(contrastRatio('oklch( 0 0 0/0.5 )', '#fff')).not.toBeNull();
    expect(contrastRatio('oklch(+.5 0. 1e0)', '#fff')).toBeNull();
    expect(contrastRatio('oklch(+.5 0. 10)', '#fff')).not.toBeNull();
  });

  it('clamps lightness to [0, 1] and chroma to at least 0, as CSS does', () => {
    expect(contrastRatio('oklch(0.55 -0.3 0)', '#000')).toBeCloseTo(
      contrastRatio('oklch(0.55 0 0)', '#000')!,
      5,
    );
    expect(contrastRatio('oklch(1.5 0 0)', '#000')).toBeCloseTo(
      contrastRatio('oklch(1 0 0)', '#000')!,
      5,
    );
    expect(contrastRatio('oklch(-0.5 0 0)', '#000')).toBeCloseTo(1, 5);
  });

  it('parses long hostile input in linear time', () => {
    const start = performance.now();
    for (const n of [50_000, 100_000]) {
      expect(contrastRatio(`oklch(${'1'.repeat(n)}x 0 0)`, '#fff')).toBeNull();
      expect(contrastRatio(`oklch(0 0 0 / ${'!/!'.repeat(n)}`, '#fff')).toBeNull();
      expect(contrastRatio(`oklch(${'1 '.repeat(n)})`, '#fff')).toBeNull();
      expect(contrastRatio(`oklch(1 1 1 / ${'1 '.repeat(n)}`, '#fff')).toBeNull();
    }
    // Quadratic backtracking would take tens of seconds here.
    expect(performance.now() - start).toBeLessThan(2000);
  });
});
