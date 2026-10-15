/**
 * Builds `theme.schema.json`, the JSON Schema (draft 2020-12) of the theme
 * document, from the manifest. The patterns are the ones the validator
 * compiles, imported from `src/theme/formats.ts`, so the schema and the
 * validator cannot disagree on a value syntax. Pure. See the white-labelling
 * theme ADR, Decision 1.
 *
 * The schema is the shape a producer can check a document against before a
 * deploy; the validator remains the authority: it also trims the product
 * name, reads the assets and applies the font-slot rule.
 */
import {
  BUNDLE_PATH_PATTERN,
  KORA_VERSION_PATTERN,
  FORMAT_PATTERNS,
} from '../../src/theme/formats.ts';
import type { Manifest } from './manifest.ts';

export const SCHEMA_DIALECT = 'https://json-schema.org/draft/2020-12/schema';

export interface SchemaProperty {
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
  /** One string schema per value format and one for bundle paths, referenced from every token and slot. */
  readonly $defs: Readonly<Record<string, SchemaProperty>>;
}

/** The `$defs` name for a bundle-relative path. */
export const BUNDLE_PATH_DEF = 'bundle-path';

export function buildThemeSchema(manifest: Manifest): ThemeSchema {
  const ref = (def: string): SchemaProperty => ({ $ref: `#/$defs/${def}` });

  const $defs: Record<string, SchemaProperty> = {};
  for (const [format, pattern] of Object.entries(FORMAT_PATTERNS).sort(([a], [b]) =>
    a.localeCompare(b),
  )) {
    $defs[format] = { type: 'string', pattern };
  }
  $defs[BUNDLE_PATH_DEF] = { type: 'string', pattern: BUNDLE_PATH_PATTERN };

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
    slots[slot.name] =
      slot.modes.length === 0
        ? ref(BUNDLE_PATH_DEF)
        : {
            type: 'object',
            additionalProperties: false,
            properties: Object.fromEntries(slot.modes.map((mode) => [mode, ref(BUNDLE_PATH_DEF)])),
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

/** The top-level properties of a schema, name to `type`, as the classifier compares them. */
export interface SchemaSurface {
  readonly properties: Readonly<Record<string, string | undefined>>;
}

export function readSchemaSurface(text: string): SchemaSurface {
  const parsed: unknown = JSON.parse(text);
  if (typeof parsed !== 'object' || parsed === null) throw new Error('schema is not an object');
  const properties = (parsed as { properties?: unknown }).properties;
  if (typeof properties !== 'object' || properties === null) {
    throw new Error('schema has no top-level "properties"');
  }
  return {
    properties: Object.fromEntries(
      Object.entries(properties as Record<string, SchemaProperty>).map(([name, p]) => [
        name,
        typeof p.type === 'string' ? p.type : undefined,
      ]),
    ),
  };
}
