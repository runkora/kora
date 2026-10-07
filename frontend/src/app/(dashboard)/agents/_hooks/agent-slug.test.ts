import { describe, expect, it } from 'vitest';
import { deriveSlug } from '@orbit/lib/slug';
import { AGENT_SLUG_MAX_LENGTH, SLUG_RE } from './use-agent-builder';

describe('SLUG_RE', () => {
  it.each([
    ['sales-agent', true],
    ['agent2-v3', true],
    ['a', true],
    ['a'.repeat(52), true],
    ['', false],
    ['l5-closed none', false],
    ['Sales-Agent', false],
    ['sales_agent', false],
    ['-sales', false],
    ['sales-', false],
    ['1sales', false],
    ['a'.repeat(53), false],
  ])('%j is %s', (slug, valid) => {
    expect(SLUG_RE.test(slug)).toBe(valid);
  });
});

describe('agent slug derived from a name', () => {
  it('fits the rule for a name longer than the limit', () => {
    const slug = deriveSlug(
      `Quarterly revenue reconciliation and forecasting assistant`,
      AGENT_SLUG_MAX_LENGTH,
    );
    expect(slug.length).toBeLessThanOrEqual(AGENT_SLUG_MAX_LENGTH);
    expect(SLUG_RE.test(slug)).toBe(true);
  });
});
