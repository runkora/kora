/**
 * Unit tests for the init container's validator entry point: the parsed-equal
 * document check and the bundle outcomes, against the fixtures the server
 * theme module uses.
 */
import { mkdir, mkdtemp, rm, symlink, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { deepEqual, documentsEqual, validateBundle } from './validate-theme';

const FIXTURES = path.resolve(__dirname, '../src/server/theme/__fixtures__');
const VALID = path.join(FIXTURES, 'valid');
const MALFORMED = path.join(FIXTURES, 'malformed');

describe('documentsEqual', () => {
  it('ignores line endings, quoting, trailing whitespace and key order', () => {
    const bundle = 'koraVersion: "1.0.0"\nproductName: Acme\ntokens:\n  --a:\n    dark: "#000"\n';
    const rendered =
      'tokens:\r\n  "--a": {dark: \'#000\'}   \r\nproductName: "Acme"\r\nkoraVersion: "1.0.0"\r\n';
    expect(documentsEqual(bundle, rendered)).toBe(true);
  });

  it('sees a changed value, a missing key and an extra key', () => {
    const bundle = 'koraVersion: "1.0.0"\nproductName: Acme\n';
    expect(documentsEqual(bundle, 'koraVersion: "1.0.0"\nproductName: Acme Robots\n')).toBe(false);
    expect(documentsEqual(bundle, 'koraVersion: "1.0.0"\n')).toBe(false);
    expect(documentsEqual(bundle, `${bundle}tokens: {}\n`)).toBe(false);
  });

  it('is a structural equality', () => {
    expect(deepEqual({ a: [1, { b: null }] }, { a: [1, { b: null }] })).toBe(true);
    expect(deepEqual({ a: [1, 2] }, { a: [2, 1] })).toBe(false);
    expect(deepEqual([], {})).toBe(false);
    expect(deepEqual(null, undefined)).toBe(false);
    expect(deepEqual('1', 1)).toBe(false);
  });
});

describe('validateBundle', () => {
  it('passes a valid bundle whose chart document parses equal, and prints the report', async () => {
    const run = await validateBundle(VALID, path.join(VALID, 'document.yaml'));
    expect(run.ok).toBe(true);
    expect(run.lines[0]).toMatch(
      /^theme ok: written for kora 1\.0\.0, validated by a kora development build \(version not compared: not a release build\), hash [0-9a-f]{64}$/,
    );
    expect(run.lines).toContain(
      'ignored (unknown to this Hades): token --hades-raw-not-a-token, slot not-a-slot',
    );
    expect(run.lines).toContain('falls through to the default: favicon.light');
    expect(run.lines.at(-1)).toBe(
      'warning: this theme mixes its own identity with kora defaults; still showing kora: favicon.light',
    );
  });

  it('fails a valid bundle whose chart document differs in substance', async () => {
    const run = await validateBundle(VALID, path.join(VALID, 'document-stale.yaml'));
    expect(run.ok).toBe(false);
    expect(run.lines[0]).toContain('does not match the bundle');
    expect(run.lines.join('\n')).toContain('not rebuilt');
  });

  it('fails a malformed bundle with every fault, before comparing documents', async () => {
    const run = await validateBundle(MALFORMED, path.join(MALFORMED, 'theme.yaml'));
    expect(run.ok).toBe(false);
    expect(run.lines[0]).toContain('is invalid against a kora development build');
    expect(run.lines).toContain('  brand: unknown top-level key');
    expect(run.lines).toContain(
      '  tokens.--hades-raw-100.dark: "red" is not a valid hex-or-oklch value',
    );
    expect(run.lines).toContain('  slots.logo.dark: "missing.svg" is not in the bundle');
  });

  it('fails when the bundle has no theme.yaml or the document is missing', async () => {
    const missing = await validateBundle(
      path.join(FIXTURES, 'nope'),
      path.join(VALID, 'theme.yaml'),
    );
    expect(missing.ok).toBe(false);
    expect(missing.lines[0]).toContain('no theme.yaml');

    const noDoc = await validateBundle(VALID, path.join(VALID, 'nope.yaml'));
    expect(noDoc.ok).toBe(false);
    expect(noDoc.lines[0]).toContain('no chart-supplied document');
  });

  it('fails when the chart document is not YAML', async () => {
    const run = await validateBundle(VALID, path.join(VALID, 'assets', 'logo-dark.svg'));
    expect(run.ok).toBe(false);
    expect(run.lines[0]).toMatch(/does not match|not valid YAML/);
  });
});

describe('validateBundle: hostile bundles', () => {
  let tmp: string;
  let bundle: string;
  let document: string;

  beforeEach(async () => {
    tmp = await mkdtemp(path.join(os.tmpdir(), 'validate-theme-'));
    bundle = path.join(tmp, 'bundle');
    document = path.join(tmp, 'document.yaml');
    await mkdir(path.join(bundle, 'assets', 'adir.svg'), { recursive: true });
    await mkdir(path.join(tmp, 'outside'));
    await writeFile(path.join(tmp, 'outside', 'out.svg'), '<svg/>');
  });
  afterEach(async () => {
    await rm(tmp, { recursive: true, force: true });
  });

  const logoDocument = (file: string) =>
    `koraVersion: "1.0.0"\nslots:\n  logo:\n    dark: ${file}\n`;

  async function run(text: string) {
    await writeFile(path.join(bundle, 'theme.yaml'), text);
    await writeFile(document, text);
    return validateBundle(bundle, document);
  }

  it('refuses a theme.yaml over the size cap', async () => {
    const run1 = await run('#'.repeat(70 * 1024));
    expect(run1.ok).toBe(false);
    expect(run1.lines[0]).toContain('65536 byte limit');
  });

  it('refuses a chart document over the size cap', async () => {
    await writeFile(path.join(bundle, 'theme.yaml'), 'koraVersion: "1.0.0"\n');
    await writeFile(document, '#'.repeat(70 * 1024));
    const result = await validateBundle(bundle, document);
    expect(result.ok).toBe(false);
    expect(result.lines[0]).toContain('65536 byte limit');
  });

  it('reports a directory asset path as not in the bundle', async () => {
    const result = await run(logoDocument('adir.svg'));
    expect(result.ok).toBe(false);
    expect(result.lines).toContain('  slots.logo.dark: "adir.svg" is not in the bundle');
  });

  it('does not follow a file or directory symlink out of the bundle', async () => {
    await symlink(path.join(tmp, 'outside', 'out.svg'), path.join(bundle, 'assets', 'leak.svg'));
    await symlink(path.join(tmp, 'outside'), path.join(bundle, 'assets', 'dirlink'));
    for (const file of ['leak.svg', 'dirlink/out.svg']) {
      const result = await run(logoDocument(file));
      expect(result.ok).toBe(false);
      expect(result.lines).toContain(`  slots.logo.dark: "${file}" is not in the bundle`);
    }
  });
});
