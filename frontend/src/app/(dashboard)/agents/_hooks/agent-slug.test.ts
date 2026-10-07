import { describe, expect, it } from 'vitest';
import { deriveSlug } from '@orbit/lib/slug';
import { AGENT_SLUG_MAX_LENGTH, SLUG_RE, agentSlugInput } from './use-agent-builder';

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

describe('agentSlugInput', () => {
  // Each keystroke passes the field's previous value plus the typed character.
  const type = (text: string) =>
    [...text].reduce((value, char) => agentSlugInput(value + char), '');

  it('keeps a typed hyphen', () => {
    expect(type('my-')).toBe('my-');
    expect(type('my-agent')).toBe('my-agent');
  });

  it('turns other characters into hyphens', () => {
    expect(type('My Agent_2')).toBe('my-agent-2');
  });

  it('stops at the agent limit', () => {
    expect(type('a'.repeat(60))).toBe('a'.repeat(AGENT_SLUG_MAX_LENGTH));
  });
});
