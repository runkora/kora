import { describe, expect, it } from 'vitest';
import { grantOwnerLink, isGrantEditable } from './grant-owner';

describe('grantOwnerLink', () => {
  it('links an agent grant to the agent named by its SPIFFE ID', () => {
    expect(
      grantOwnerLink({
        principal_type: 'AGENT',
        principal_id: 'spiffe://orbit.test/tenant/acme/agent/ci-investigator',
      }),
    ).toEqual({ href: '/agents/ci-investigator', label: 'Open agent' });
  });

  it.each([
    ['a parent segment', 'spiffe://td/tenant/acme/agent/..'],
    ['a current segment', 'spiffe://td/tenant/acme/agent/.'],
    ['a trailing slash', 'spiffe://td/tenant/acme/agent/'],
    ['no agent segment', 'spiffe://td/tenant/acme/service/ci-bot'],
    ['no path', 'spiffe://td'],
    ['an empty ID', ''],
  ])('gives an agent ID with %s no link', (_name, id) => {
    expect(grantOwnerLink({ principal_type: 'AGENT', principal_id: id })).toBeNull();
  });

  it('gives an agent grant with an undefined ID no link', () => {
    expect(
      grantOwnerLink({ principal_type: 'AGENT', principal_id: undefined as unknown as string }),
    ).toBeNull();
  });

  it.each([
    ['a?b', 'a%3Fb'],
    ['a#b', 'a%23b'],
    ['a%b', 'a%25b'],
  ])('encodes the slug %s in the link', (slug, encoded) => {
    expect(
      grantOwnerLink({
        principal_type: 'AGENT',
        principal_id: `spiffe://td/tenant/acme/agent/${slug}`,
      })?.href,
    ).toBe(`/agents/${encoded}`);
  });

  it('gives an agent grant keyed by a UUID no link', () => {
    expect(
      grantOwnerLink({
        principal_type: 'AGENT',
        principal_id: 'a3f1c9e2-5b7d-4c1a-9e60-2d8b4f7a1c35',
      }),
    ).toBeNull();
  });

  it.each(['trigger:9b1c', 'trigger-source:9b1c'])(
    'links the trigger grant %s to triggers',
    (id) => {
      expect(grantOwnerLink({ principal_type: 'SERVICE', principal_id: id })).toEqual({
        href: '/triggers',
        label: 'Open triggers',
      });
    },
  );

  it.each(['svc-triggers', 'trigger:', 'trigger:  ', 'trigger-source: ', ''])(
    'gives the SERVICE id "%s" no link',
    (id) => {
      expect(grantOwnerLink({ principal_type: 'SERVICE', principal_id: id })).toBeNull();
    },
  );

  it('links a PAT grant to the API keys screen', () => {
    expect(grantOwnerLink({ principal_type: 'PAT', principal_id: 'pat_5e0b72c4' })).toEqual({
      href: '/model-gateway/keys',
      label: 'Open API keys',
    });
  });

  it('gives a human grant no link', () => {
    expect(grantOwnerLink({ principal_type: 'HUMAN', principal_id: 'usr_1' })).toBeNull();
  });
});

describe('isGrantEditable', () => {
  it('allows only a human grant', () => {
    expect(isGrantEditable({ principal_type: 'HUMAN' })).toBe(true);
    expect(isGrantEditable({ principal_type: 'AGENT' })).toBe(false);
    expect(isGrantEditable({ principal_type: 'SERVICE' })).toBe(false);
    expect(isGrantEditable({ principal_type: 'PAT' })).toBe(false);
  });
});
