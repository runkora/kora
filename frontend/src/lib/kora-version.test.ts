import { describe, expect, it } from 'vitest';
import { instanceKoraVersion } from './kora-version';

describe('instanceKoraVersion', () => {
  it.each([
    ['v0.8.0', '0.8.0'],
    ['0.8.0', '0.8.0'],
    ['v1.4.2-rc.3', '1.4.2-rc.3'],
    ['  v2.0.0 ', '2.0.0'],
  ])('reads the release in %j', (raw, expected) => {
    expect(instanceKoraVersion(raw)).toBe(expected);
  });

  it.each([undefined, '', 'dev', '3f9c2ab', 'main', 'v1.4', 'latest'])(
    'treats %j as a build that is not from a release',
    (raw) => {
      expect(instanceKoraVersion(raw)).toBeNull();
    },
  );
});
