import { describe, expect, it } from 'vitest';
import { deriveSlug } from './slug';

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
