/**
 * The bounded read of a theme document: the theme.yaml size cap.
 */
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  MAX_THEME_YAML_BYTES,
  ThemeYamlTooLargeError,
  isMissing,
  readThemeDocument,
} from './theme-document';

let tmp: string;

beforeEach(async () => {
  tmp = await mkdtemp(path.join(os.tmpdir(), 'theme-document-'));
});

afterEach(async () => {
  await rm(tmp, { recursive: true, force: true });
});

describe('readThemeDocument', () => {
  it('reads a document at the cap and refuses one byte over', async () => {
    const file = path.join(tmp, 'theme.yaml');
    await writeFile(file, '#'.repeat(MAX_THEME_YAML_BYTES));
    expect((await readThemeDocument(file)).length).toBe(MAX_THEME_YAML_BYTES);
    await writeFile(file, '#'.repeat(MAX_THEME_YAML_BYTES + 1));
    await expect(readThemeDocument(file)).rejects.toBeInstanceOf(ThemeYamlTooLargeError);
  });

  it('throws an error isMissing recognises when the file is absent', async () => {
    const err = await readThemeDocument(path.join(tmp, 'nope.yaml')).catch((e: unknown) => e);
    expect(isMissing(err)).toBe(true);
  });
});
