import { describe, expect, it } from 'vitest';
import { deriveSlug, slugInput } from './slug';

describe('deriveSlug', () => {
  it.each([
    ['Go services', 'go-services'],
    ['  Payments / Review ', 'payments-review'],
    ['node22', 'node22'],
    ['---', ''],
    ['Café ML', 'caf-ml'],
  ])('derives %j as %j', (name, slug) => {
    expect(deriveSlug(name)).toBe(slug);
  });
});

describe('slugInput', () => {
  it('keeps a hyphen as it is typed', () => {
    expect(slugInput('go-')).toBe('go-');
    expect(slugInput('Go Services')).toBe('go-services');
    expect(slugInput('a__b')).toBe('a-b');
  });
});
