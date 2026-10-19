/**
 * Builds `theme.schema.json` (JSON Schema draft 2020-12) from the manifest.
 * Pure.
 *
 * The value patterns come from `src/theme/formats.ts`, the same ones the
 * validator compiles, so the two cannot disagree on a value syntax.
 *
 * A producer can check a document against the schema before a deploy. The
 * validator stays the authority: it also trims the product name and applies
 * the font-slot rule. Neither one fetches an asset.
 */
import {
  ASSET_URL_PATTERN,
  KORA_VERSION_PATTERN,
  assetExtensionPattern,
  FORMAT_PATTERNS,
} from '../../src/theme/formats.ts';
import type { Manifest } from './manifest.ts';

const SCHEMA_DIALECT = 'https://json-schema.org/draft/2020-12/schema';

interface SchemaProperty {
  readonly type?: string;
  readonly [key: string]: unknown;
}

export interface ThemeSchema {
  readonly $schema: string;
  readonly title: string;
  readonly description: string;
  readonly type: 'object';
  readonly additionalProperties: false;
  readonly required: readonly string[];
  readonly properties: Readonly<Record<string, SchemaProperty>>;
  /** One string schema per value format, plus one for asset URLs. Tokens and slots refer to them. */
  readonly $defs: Readonly<Record<string, SchemaProperty>>;
}

/** The `$defs` name for an asset URL. */
const ASSET_URL_DEF = 'asset-url';

export function buildThemeSchema(manifest: Manifest): ThemeSchema {
  const ref = (def: string): SchemaProperty => ({ $ref: `#/$defs/${def}` });

  const $defs: Record<string, SchemaProperty> = {};
  for (const [format, pattern] of Object.entries(FORMAT_PATTERNS).sort(([a], [b]) =>
    a.localeCompare(b),
  )) {
    $defs[format] = { type: 'string', pattern };
  }
  $defs[ASSET_URL_DEF] = { type: 'string', pattern: ASSET_URL_PATTERN };

  const tokens: Record<string, SchemaProperty> = {};
  for (const token of manifest.tokens) {
    tokens[token.name] = {
      type: 'object',
      additionalProperties: false,
      properties: Object.fromEntries(token.modes.map((mode) => [mode, ref(token.format)])),
    };
  }

  const slots: Record<string, SchemaProperty> = {};
  for (const slot of manifest.slots) {
    // The URL form, plus the extensions this slot's path may end in.
    const url: SchemaProperty = {
      allOf: [ref(ASSET_URL_DEF), { type: 'string', pattern: assetExtensionPattern(slot.formats) }],
    };
    slots[slot.name] =
      slot.modes.length === 0
        ? url
        : {
            type: 'object',
            additionalProperties: false,
            properties: Object.fromEntries(slot.modes.map((mode) => [mode, url])),
          };
  }

  return {
    $schema: SCHEMA_DIALECT,
    title: 'Hades theme document',
    description:
      'A white-labelling theme, written against a kora release. An instance accepts a theme of its own major. Unknown tokens and slots are ignored by the instance; an unknown top-level key is refused.',
    type: 'object',
    additionalProperties: false,
    required: ['koraVersion'],
    properties: {
      koraVersion: { type: 'string', pattern: KORA_VERSION_PATTERN },
      productName: {
        type: 'string',
        minLength: manifest.productName.minLength,
        maxLength: manifest.productName.maxLength,
        pattern: manifest.productName.pattern,
      },
      tokens: { type: 'object', properties: tokens, additionalProperties: true },
      slots: { type: 'object', properties: slots, additionalProperties: true },
    },
    $defs,
  };
}

/** Stable JSON with a trailing newline, as checked in. */
export function serializeSchema(schema: ThemeSchema): string {
  return `${JSON.stringify(schema, null, 2)}\n`;
}
