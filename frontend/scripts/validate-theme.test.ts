/**
 * Unit tests for the init container's validator entry point, against the
 * fixture documents the server theme module uses.
 */
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { main, validateDocument } from './validate-theme';

const FIXTURES = path.resolve(__dirname, '../src/server/theme/__fixtures__');
const VALID = path.join(FIXTURES, 'valid', 'theme.yaml');
const MALFORMED = path.join(FIXTURES, 'malformed', 'theme.yaml');

describe('validateDocument', () => {
  it('passes a valid document and prints the report', async () => {
    const run = await validateDocument(VALID);
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

  it('fails a malformed document with every fault', async () => {
    const run = await validateDocument(MALFORMED);
    expect(run.ok).toBe(false);
    expect(run.lines[0]).toContain('is invalid against a kora development build');
    expect(run.lines).toContain('  brand: unknown top-level key');
    expect(run.lines).toContain(
      '  tokens.--hades-raw-100.dark: "red" is not a valid hex-or-oklch value',
    );
    expect(run.lines.join('\n')).toContain(
      'slots.logo.dark: "http://assets.acme.example/logo-dark.svg" is not an https:// URL',
    );
  });

  it('never fetches a slot URL: an unreachable host does not fail the document', async () => {
    const run = await validateDocument(VALID);
    expect(run.ok).toBe(true);
  });

  it('fails when the document is missing', async () => {
    const run = await validateDocument(path.join(FIXTURES, 'valid', 'nope.yaml'));
    expect(run.ok).toBe(false);
    expect(run.lines[0]).toContain('no theme document');
  });
});

describe('main', () => {
  it('takes exactly one argument, the document', async () => {
    expect(await main([])).toBe(2);
    expect(await main([VALID, VALID])).toBe(2);
  });
});
