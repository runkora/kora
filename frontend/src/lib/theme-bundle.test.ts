/**
 * The bounded bundle reads: the theme.yaml size cap, and an asset read that
 * stays inside the bundle through symlinks, directories and odd paths.
 */
import { mkdir, mkdtemp, rm, symlink, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  MAX_THEME_YAML_BYTES,
  ThemeYamlTooLargeError,
  mixedIdentity,
  readBundleAsset,
  readThemeDocument,
} from './theme-bundle';

let tmp: string;
let bundle: string;
let outside: string;

beforeEach(async () => {
  tmp = await mkdtemp(path.join(os.tmpdir(), 'theme-io-'));
  bundle = path.join(tmp, 'bundle');
  outside = path.join(tmp, 'outside');
  await mkdir(path.join(bundle, 'assets', 'sub'), { recursive: true });
  await mkdir(outside);
  await writeFile(path.join(bundle, 'assets', 'ok.svg'), '<svg/>');
  await writeFile(path.join(bundle, 'assets', 'sub', 'nested.svg'), '<svg/>');
  await writeFile(path.join(outside, 'secret.svg'), 'secret');
});
afterEach(async () => {
  await rm(tmp, { recursive: true, force: true });
});

describe('readBundleAsset', () => {
  it('reads a file, a nested file and one reached through an in-bundle symlink', async () => {
    await symlink('ok.svg', path.join(bundle, 'assets', 'alias.svg'));
    for (const name of ['ok.svg', 'sub/nested.svg', 'alias.svg']) {
      const bytes = await readBundleAsset(bundle, name, 100);
      expect(new TextDecoder().decode(bytes!)).toBe('<svg/>');
    }
  });

  it('returns null for a file symlink that points outside the bundle', async () => {
    await symlink(path.join(outside, 'secret.svg'), path.join(bundle, 'assets', 'leak.svg'));
    expect(await readBundleAsset(bundle, 'leak.svg', 100)).toBeNull();
  });

  it('returns null for a file reached through a directory symlink that points outside', async () => {
    await symlink(outside, path.join(bundle, 'assets', 'linked'));
    expect(await readBundleAsset(bundle, 'linked/secret.svg', 100)).toBeNull();
  });

  it('returns null when the bundle root itself is reached by a link but the file escapes', async () => {
    const link = path.join(tmp, 'root-link');
    await symlink(bundle, link);
    await symlink(path.join(outside, 'secret.svg'), path.join(bundle, 'assets', 'leak.svg'));
    expect(await readBundleAsset(link, 'leak.svg', 100)).toBeNull();
    expect(new TextDecoder().decode((await readBundleAsset(link, 'ok.svg', 100))!)).toBe('<svg/>');
  });

  it('returns null for a missing file, a directory and a path with a NUL', async () => {
    expect(await readBundleAsset(bundle, 'nope.svg', 100)).toBeNull();
    expect(await readBundleAsset(bundle, 'sub', 100)).toBeNull();
    expect(await readBundleAsset(bundle, 'bad\0.svg', 100)).toBeNull();
    expect(await readBundleAsset(bundle, 'ok.svg/x', 100)).toBeNull();
  });

  it('reads at most maxBytes + 1 bytes', async () => {
    await writeFile(path.join(bundle, 'assets', 'big.svg'), 'x'.repeat(50));
    expect((await readBundleAsset(bundle, 'big.svg', 10))!.length).toBe(11);
  });
});

describe('readThemeDocument', () => {
  it('reads a document at the cap and refuses one byte over', async () => {
    const file = path.join(bundle, 'theme.yaml');
    await writeFile(file, '#'.repeat(MAX_THEME_YAML_BYTES));
    expect((await readThemeDocument(file)).length).toBe(MAX_THEME_YAML_BYTES);
    await writeFile(file, '#'.repeat(MAX_THEME_YAML_BYTES + 1));
    await expect(readThemeDocument(file)).rejects.toBeInstanceOf(ThemeYamlTooLargeError);
  });
});

describe('mixedIdentity', () => {
  const manifest = {
    slots: [
      { kind: 'image', modes: ['dark', 'light'] },
      { kind: 'image', modes: ['dark', 'light'] },
      { kind: 'font', modes: [] },
    ],
  };
  const ALL = ['productName', 'logo.dark', 'logo.light', 'favicon.dark', 'favicon.light'];

  it('is empty when the theme sets none or all of the identity values', () => {
    expect(mixedIdentity({ fallThrough: ALL }, manifest)).toEqual([]);
    expect(mixedIdentity({ fallThrough: [] }, manifest)).toEqual([]);
  });

  it('lists the fall-through values when the theme sets some', () => {
    expect(mixedIdentity({ fallThrough: ['logo.light', 'favicon.light'] }, manifest)).toEqual([
      'logo.light',
      'favicon.light',
    ]);
    expect(mixedIdentity({ fallThrough: ALL.slice(1) }, manifest)).toEqual(ALL.slice(1));
  });

  it("prefers the validator's own field when the report has it", () => {
    expect(mixedIdentity({ fallThrough: ALL, mixedIdentity: ['x'] }, manifest)).toEqual(['x']);
    expect(mixedIdentity({ fallThrough: ['logo.dark'], mixedIdentity: [] }, manifest)).toEqual([]);
  });
});
