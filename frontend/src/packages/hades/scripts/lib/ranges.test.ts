import { describe, expect, it } from 'vitest';

import { compareRanges } from './ranges.ts';

describe('compareRanges', () => {
  it('is the same for an identical range', () => {
    expect(compareRanges('>=19', '>=19')).toBe('same');
    expect(compareRanges('>=1 <3', '<3 >=1')).toBe('same');
  });

  it('widens when the new range accepts everything the old one did', () => {
    expect(compareRanges('>=19.2', '>=19')).toBe('widened');
    expect(compareRanges('^1.2.0', '>=1.2.0 <3')).toBe('widened');
    expect(compareRanges('^18', '^18 || ^19')).toBe('widened');
    expect(compareRanges('>=1', '*')).toBe('widened');
  });

  it('narrows when the new range drops versions the old one accepted', () => {
    expect(compareRanges('>=19', '>=19.2')).toBe('narrowed');
    expect(compareRanges('>=1', '>=1 <3')).toBe('narrowed');
    expect(compareRanges('^18 || ^19', '^19')).toBe('narrowed');
    expect(compareRanges('~1.2.3', '1.2.3')).toBe('narrowed');
  });

  it('judges a union by what it covers, merging alternatives that overlap or touch', () => {
    expect(compareRanges('>=1 <3', '>=1 <2 || >=2 <4')).toBe('widened');
    expect(compareRanges('>=1 <3', '>=1 <2 || >=2 <3')).toBe('same');
    expect(compareRanges('^1 || ^2', '>=1 <3')).toBe('same');
    expect(compareRanges('>=1', '>=1 <2 || >=2')).toBe('same');
    expect(compareRanges('>=1', '>=1 <2 || >=3')).toBe('narrowed');
    expect(compareRanges('>=1 <3', '>=1 <2 || >2 <4')).toBe('narrowed');
  });

  it('narrows when the ranges overlap without one containing the other', () => {
    expect(compareRanges('^1.0.0', '^2.0.0')).toBe('narrowed');
  });

  it('understands tilde, caret on zero versions, partials and exclusive bounds', () => {
    expect(compareRanges('~1.2', '>=1.2.0 <1.3.0')).toBe('same');
    expect(compareRanges('^0.2.3', '>=0.2.3 <0.3.0')).toBe('same');
    expect(compareRanges('19', '>=19.0.0 <20.0.0')).toBe('same');
    expect(compareRanges('>0.400', '>=0.401')).toBe('same');
    expect(compareRanges('<=19', '<20')).toBe('same');
  });

  it('treats a range it cannot parse as narrowed', () => {
    expect(compareRanges('>=1', 'latest')).toBe('narrowed');
    expect(compareRanges('1.0.0 - 2.0.0', '>=1')).toBe('narrowed');
  });
});
