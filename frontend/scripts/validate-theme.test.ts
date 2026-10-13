/**
 * Unit tests for the init container's validator entry point: the parsed-equal
 * document check and the bundle outcomes, against the fixtures the server
 * theme module uses.
 */
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { deepEqual, documentsEqual, validateBundle } from './validate-theme';

const FIXTURES = path.resolve(__dirname, '../src/server/theme/__fixtures__');
const VALID = path.join(FIXTURES, 'valid');
const MALFORMED = path.join(FIXTURES, 'malformed');

describe('documentsEqual', () => {
  it('ignores line endings, quoting, trailing whitespace and key order', () => {
    const bundle = 'hadesVersion: 1.0.0\nproductName: Acme\ntokens:\n  --a:\n    dark: "#000"\n';
    const rendered =
      'tokens:\r\n  "--a": {dark: \'#000\'}   \r\nproductName: "Acme"\r\nhadesVersion: "1.0.0"\r\n';
    expect(documentsEqual(bundle, rendered)).toBe(true);
  });

  it('sees a changed value, a missing key and an extra key', () => {
    const bundle = 'hadesVersion: 1.0.0\nproductName: Acme\n';
    expect(documentsEqual(bundle, 'hadesVersion: 1.0.0\nproductName: Acme Robots\n')).toBe(false);
    expect(documentsEqual(bundle, 'hadesVersion: 1.0.0\n')).toBe(false);
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
      /^theme ok: written for Hades 1\.0\.0, validated by Hades 1\.0\.0, hash [0-9a-f]{64}$/,
    );
    expect(run.lines).toContain(
      'ignored (unknown to this Hades): token --hades-raw-not-a-token, slot not-a-slot',
    );
    expect(run.lines).toContain('falls through to the default: favicon.light');
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
    expect(run.lines[0]).toContain('is invalid against Hades');
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
