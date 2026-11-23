import { describe, expect, it } from 'vitest';
import {
  attributionDraftOf,
  attributionFrom,
  attributionHint,
  EMPTY_ATTRIBUTION,
  EMPTY_GENERIC_DRAFT,
  fixedEventType,
  genericConfigFrom,
  genericDraftHint,
  pastesSecret,
  verificationMethodOf,
  type AttributionDraft,
  type GenericSourceDraft,
} from './generic-source';

const HMAC: GenericSourceDraft = {
  ...EMPTY_GENERIC_DRAFT,
  method: 'hmac_sha256',
  header: ' X-Signature ',
  prefix: 'sha256=',
  encoding: 'base64',
};

describe('genericConfigFrom', () => {
  it('sends the header, prefix and encoding for HMAC only', () => {
    expect(genericConfigFrom(HMAC)).toEqual({
      verification: {
        method: 'hmac_sha256',
        header: 'X-Signature',
        prefix: 'sha256=',
        encoding: 'base64',
      },
    });
    expect(genericConfigFrom({ ...HMAC, method: 'bearer' })).toEqual({
      verification: { method: 'bearer' },
    });
  });

  it('omits an empty prefix and sends the event type location', () => {
    expect(
      genericConfigFrom({ ...HMAC, prefix: '', eventLocation: 'path', eventKey: ' data.type ' }),
    ).toEqual({
      verification: { method: 'hmac_sha256', header: 'X-Signature', encoding: 'base64' },
      event_type: { path: 'data.type' },
    });
  });
});

describe('genericDraftHint', () => {
  it('asks for a method, then a valid header', () => {
    expect(genericDraftHint(EMPTY_GENERIC_DRAFT, '')).toBe('Choose how deliveries are verified.');
    expect(genericDraftHint({ ...HMAC, header: 'X Sig' }, '')).toBe(
      'The signature header must be a valid HTTP header name.',
    );
    expect(genericDraftHint(HMAC, '')).toBeUndefined();
  });

  it('refuses a prefix over 32 bytes', () => {
    expect(genericDraftHint({ ...HMAC, prefix: 'x'.repeat(33) }, '')).toBe(
      'The signature prefix can be at most 32 bytes.',
    );
  });

  it('asks for a pasted secret, and never for a bearer one', () => {
    const paste = { ...HMAC, secretChoice: 'paste' as const };
    expect(genericDraftHint(paste, ' ')).toBe("Paste the sender's signing secret.");
    expect(genericDraftHint(paste, 's')).toBeUndefined();
    expect(pastesSecret({ ...paste, method: 'bearer' })).toBe(false);
  });

  it('refuses a credential or signature header as the event type header', () => {
    const hint = 'The event type header cannot be a credential or signature header.';
    const at = (eventKey: string, draft = HMAC) =>
      genericDraftHint({ ...draft, eventLocation: 'header', eventKey }, '');
    expect(at('authorization')).toBe(hint);
    expect(at('Webhook-Signature')).toBe(hint);
    expect(at('WEBHOOK-ID', { ...HMAC, method: 'standard_webhooks' })).toBe(hint);
    expect(at('x-signature')).toBe(hint);
    expect(at('X-Signature', { ...HMAC, method: 'bearer' })).toBeUndefined();
    expect(at('X-Event-Key')).toBeUndefined();
  });

  it('checks the event type header and path', () => {
    expect(genericDraftHint({ ...HMAC, eventLocation: 'header', eventKey: '' }, '')).toBe(
      'Name a valid HTTP header for the event type.',
    );
    expect(genericDraftHint({ ...HMAC, eventLocation: 'path', eventKey: 'a.' }, '')).toMatch(
      /dotted path/,
    );
    expect(
      genericDraftHint({ ...HMAC, eventLocation: 'path', eventKey: 'data.type' }, ''),
    ).toBeUndefined();
  });
});

describe('reading a stored source', () => {
  it('reads the method of a generic source only', () => {
    const config = { verification: { method: 'standard_webhooks' } };
    expect(verificationMethodOf('generic', config)).toBe('standard_webhooks');
    expect(verificationMethodOf('github', config)).toBeUndefined();
    expect(verificationMethodOf('generic', { verification: { method: 'rsa' } })).toBeUndefined();
  });

  it('fixes the event type webhook when the source has no event type location', () => {
    expect(fixedEventType('generic', { verification: { method: 'bearer' } })).toBe('webhook');
    expect(fixedEventType('generic', { event_type: { header: 'X-Event' } })).toBeUndefined();
    expect(fixedEventType('github', undefined)).toBeUndefined();
  });
});

describe('attribution', () => {
  const FULL: AttributionDraft = {
    links: [{ label: ' #{{ data.number }} ', url: 'https://t.example.com/{{data.id}}' }],
    actor: { name: 'Tracker', kind: 'system', url: '' },
    object: { label: '{{ data.title }}', url: 'https://t.example.com/{{ data.id }}' },
    verbs: [{ eventType: 'issue.created', verb: 'created' }],
  };

  it('writes the block into the config on create, trimmed', () => {
    expect(genericConfigFrom({ ...HMAC, method: 'bearer', attribution: FULL })).toEqual({
      verification: { method: 'bearer' },
      attribution: {
        links: [{ label: '#{{ data.number }}', url: 'https://t.example.com/{{data.id}}' }],
        actor: { name: 'Tracker', kind: 'system' },
        object: { label: '{{ data.title }}', url: 'https://t.example.com/{{ data.id }}' },
        verbs: { 'issue.created': 'created' },
      },
    });
  });

  it('omits an empty block', () => {
    expect(genericConfigFrom({ ...HMAC, method: 'bearer' })).not.toHaveProperty('attribution');
    expect(attributionFrom(EMPTY_ATTRIBUTION)).toEqual({});
  });

  it('reads a stored block back into the form', () => {
    const config = { attribution: attributionFrom(FULL) };
    expect(attributionFrom(attributionDraftOf(config))).toEqual(attributionFrom(FULL));
    expect(attributionDraftOf({ verification: {} })).toEqual(EMPTY_ATTRIBUTION);
  });

  it('accepts a complete block, and blocks the wizard on a broken one', () => {
    expect(attributionHint(FULL)).toBeUndefined();
    expect(attributionHint(EMPTY_ATTRIBUTION)).toBeUndefined();
    const broken = { ...FULL, actor: { ...FULL.actor, kind: '' as const } };
    expect(genericDraftHint({ ...HMAC, attribution: broken }, '')).toBe(
      'Choose what kind of actor it is.',
    );
  });

  it.each<[string, Partial<AttributionDraft>]>([
    [
      'A source can have at most 10 links.',
      { links: Array.from({ length: 11 }, () => ({ label: 'a', url: 'https://x' })) },
    ],
    ['Give every link a label and a URL.', { links: [{ label: 'a', url: ' ' }] }],
    ['Name the actor, or clear its URL.', { actor: { name: '', kind: '', url: 'https://x' } }],
    ['Name the actor, or clear its kind.', { actor: { name: '', kind: 'bot', url: '' } }],
    ['Label the object, or clear its URL.', { object: { label: '', url: 'https://x' } }],
    ['Set the actor name and the object label together.', { object: { label: '', url: '' } }],
    ['Give every verb an event type and a verb.', { verbs: [{ eventType: 'a', verb: '' }] }],
    [
      'Map each event type to one verb.',
      {
        verbs: [
          { eventType: 'a', verb: 'x' },
          { eventType: ' a', verb: 'y' },
        ],
      },
    ],
    [
      'Write each placeholder as a dotted payload path, such as {{ data.id }}.',
      { links: [{ label: '{{ data..id }}', url: 'https://x' }] },
    ],
    [
      'Write each placeholder as a dotted payload path, such as {{ data.id }}.',
      { object: { label: '{{ data-title }}', url: '' } },
    ],
    [
      'A URL template can be at most 2,048 bytes.',
      { links: [{ label: 'a', url: 'https://x/' + 'é'.repeat(1100) }] },
    ],
    [
      'A label, name or verb can be at most 300 characters.',
      { actor: { name: 'n'.repeat(301), kind: 'bot', url: '' } },
    ],
  ])('refuses: %s', (hint, patch) => {
    expect(attributionHint({ ...FULL, ...patch })).toBe(hint);
  });
});
