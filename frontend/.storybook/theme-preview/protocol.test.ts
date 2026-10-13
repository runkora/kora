import { describe, expect, it } from 'vitest';
import {
  PROTOCOL_VERSION,
  RESULT_TYPE,
  isThemeMessage,
  isThemeMessageEnvelope,
  rejectionFor,
} from './protocol';

const request = {
  type: 'kora-theme-preview',
  version: 1,
  document: 'a: b',
  assets: { 'x.svg': 'u' },
};

describe('isThemeMessage', () => {
  it('accepts a request and a clear message', () => {
    expect(isThemeMessage(request)).toBe(true);
    expect(isThemeMessage({ type: 'kora-theme-preview', version: 1, clear: true })).toBe(true);
  });

  it.each([
    ['null', null],
    ['a string', 'kora-theme-preview'],
    ['another type', { ...request, type: 'other' }],
    ['no document', { ...request, document: undefined }],
    ['assets not a map', { ...request, assets: [] }],
    ['a non-string URL', { ...request, assets: { 'x.svg': 1 } }],
    ['an unknown version', { ...request, version: 2 }],
  ])('rejects %s', (_name, value) => {
    expect(isThemeMessage(value)).toBe(false);
  });
});

describe('rejectionFor', () => {
  it('answers an unknown version with an error naming the version', () => {
    const reply = rejectionFor({ ...request, version: 2 });
    expect(isThemeMessageEnvelope({ ...request, version: 2 })).toBe(true);
    expect(reply).toMatchObject({ type: RESULT_TYPE, version: PROTOCOL_VERSION, ok: false });
    expect(reply.errors?.[0]).toMatchObject({ path: 'version' });
    expect(reply.errors?.[0]?.message).toContain('unsupported protocol version 2');
  });

  it('answers a malformed version 1 message with an error', () => {
    const reply = rejectionFor({ type: 'kora-theme-preview', version: 1 });
    expect(reply.ok).toBe(false);
    expect(reply.errors?.[0]?.message).toContain('malformed');
  });
});
