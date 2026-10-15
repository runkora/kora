import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import Ajv2020 from 'ajv/dist/2020';
import { describe, expect, it } from 'vitest';

import {
  BUNDLE_PATH_PATTERN,
  FORMAT_PATTERNS,
  KORA_VERSION_PATTERN,
} from '../../src/theme/formats';
import { validateTheme } from '../../src/theme/validate';
import { buildManifest, SURFACE } from './manifest.ts';
import { buildThemeSchema, readSchemaSurface, serializeSchema } from './schema.ts';

const packageDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const tokensCss = fs.readFileSync(path.join(packageDir, 'src', 'tokens', 'tokens.css'), 'utf8');
const manifest = buildManifest(tokensCss, SURFACE);
const schema = buildThemeSchema(manifest);

describe('buildThemeSchema', () => {
  it('declares the document shape: four top-level keys, nothing else, koraVersion required', () => {
    expect(schema.$schema).toBe('https://json-schema.org/draft/2020-12/schema');
    expect(schema.additionalProperties).toBe(false);
    expect(schema.required).toEqual(['koraVersion']);
    expect(Object.keys(schema.properties)).toEqual([
      'koraVersion',
      'productName',
      'tokens',
      'slots',
    ]);
    expect(schema.properties.koraVersion).toEqual({
      type: 'string',
      pattern: KORA_VERSION_PATTERN,
    });
    expect(schema.properties.productName).toEqual({
      type: 'string',
      minLength: 1,
      maxLength: 40,
      pattern: manifest.productName.pattern,
    });
  });

  it('lists every manifest token and slot, references the formats once each, and leaves unknown entries open', () => {
    const tokens = schema.properties.tokens as {
      properties: Record<string, unknown>;
      additionalProperties: boolean;
    };
    expect(Object.keys(tokens.properties)).toEqual(manifest.tokens.map((t) => t.name));
    expect(tokens.additionalProperties).toBe(true);
    expect(tokens.properties['--hades-raw-shadow-sm']).toEqual({
      type: 'object',
      additionalProperties: false,
      properties: {
        dark: { $ref: '#/$defs/css-shadow-list' },
        light: { $ref: '#/$defs/css-shadow-list' },
      },
    });
    const slots = schema.properties.slots as {
      properties: Record<string, unknown>;
      additionalProperties: boolean;
    };
    expect(Object.keys(slots.properties)).toEqual(manifest.slots.map((s) => s.name));
    expect(slots.additionalProperties).toBe(true);
    expect(slots.properties['font.sans']).toEqual({ $ref: '#/$defs/bundle-path' });
    expect(slots.properties.logo).toEqual({
      type: 'object',
      additionalProperties: false,
      properties: { dark: { $ref: '#/$defs/bundle-path' }, light: { $ref: '#/$defs/bundle-path' } },
    });
    for (const [format, pattern] of Object.entries(FORMAT_PATTERNS)) {
      expect(schema.$defs[format]).toEqual({ type: 'string', pattern });
    }
    expect(schema.$defs['bundle-path']).toEqual({ type: 'string', pattern: BUNDLE_PATH_PATTERN });
  });

  it('writes the font-family-name pattern with combining marks allowed', () => {
    const source = (schema.$defs['font-family-name'] as { pattern: string }).pattern;
    expect(new RegExp(source, 'u').test('Cafe\u0301, Source Sans 3')).toBe(true);
  });

  it('serializes with a trailing newline and reads back its surface', () => {
    const text = serializeSchema(schema);
    expect(text.endsWith('}\n')).toBe(true);
    expect(readSchemaSurface(text)).toEqual({
      properties: {
        koraVersion: 'string',
        productName: 'string',
        tokens: 'object',
        slots: 'object',
      },
    });
    expect(() => readSchemaSurface('{"type":"object"}')).toThrow(/properties/);
  });

  it('matches the checked-in theme.schema.json', () => {
    const checkedIn = fs.readFileSync(path.join(packageDir, 'theme.schema.json'), 'utf8');
    expect(checkedIn).toBe(serializeSchema(buildThemeSchema(buildManifest(tokensCss, SURFACE))));
  });
});

describe('the schema and the validator agree', () => {
  const ajv = new Ajv2020({ allErrors: true, strict: true });
  const check = ajv.compile(schema);
  const noAssets = async () => null;

  async function both(doc: unknown): Promise<{ schema: boolean; validator: boolean }> {
    const result = await validateTheme(JSON.stringify(doc), {
      manifest,
      koraVersion: '1.0.0',
      readAsset: noAssets,
    });
    return { schema: check(doc), validator: result.ok };
  }

  it('on a valid sample document', async () => {
    const doc = {
      koraVersion: '1.0.0',
      productName: "Acme & Co.'s Agents",
      tokens: {
        '--hades-raw-black': { dark: '#0d0f11', light: 'oklch(97% 0.01 90)' },
        '--hades-raw-shadow-sm': { dark: '0 2px 8px oklch(0% 0 0 / 0.25)' },
        '--hades-raw-radius-md': { light: '6px' },
        '--hades-raw-font-mono': { dark: 'Cascadia Code, monospace' },
      },
    };
    expect(await both(doc)).toEqual({ schema: true, validator: true });
  });

  it('on an unknown token, which both let through', async () => {
    expect(
      await both({
        koraVersion: '1.0.0',
        tokens: { '--hades-raw-future': { dark: 'anything' } },
      }),
    ).toEqual({
      schema: true,
      validator: true,
    });
  });

  it('on each fault both can see', async () => {
    const faults: unknown[] = [
      { koraVersion: '1.0.0', fonts: {} },
      { productName: 'Acme' },
      { koraVersion: '1', tokens: {} },
      { koraVersion: '1.0', tokens: {} },
      { koraVersion: '1.0.0', productName: '<b>x</b>' },
      { koraVersion: '1.0.0', productName: 'a'.repeat(41) },
      { koraVersion: '1.0.0', tokens: { '--hades-raw-black': { dark: 'red' } } },
      { koraVersion: '1.0.0', tokens: { '--hades-raw-black': { dim: '#000' } } },
      { koraVersion: '1.0.0', tokens: { '--hades-raw-black': '#000' } },
      { koraVersion: '1.0.0', tokens: { '--hades-raw-radius-md': { dark: '6' } } },
      { koraVersion: '1.0.0', tokens: { '--hades-raw-shadow-sm': { dark: '0 0 0 #000; }' } } },
      { koraVersion: '1.0.0', slots: { logo: { dark: '../x.svg' } } },
      { koraVersion: '1.0.0', slots: { 'font.sans': { dark: 'x.woff2' } } },
      { koraVersion: '1.0.0', slots: { logo: 'x.svg' } },
    ];
    for (const doc of faults) {
      expect(await both(doc), JSON.stringify(doc)).toEqual({ schema: false, validator: false });
    }
  });
});
