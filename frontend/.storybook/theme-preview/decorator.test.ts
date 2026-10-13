import { describe, expect, it, vi } from 'vitest';
import {
  STYLE_ATTRIBUTE,
  applyThemeStyle,
  handleThemeMessage,
  type ThemeMessageDeps,
} from './decorator';
import type { ValidationResult } from '@clear-route/hades/theme';

const report = { ignored: [{ kind: 'token' as const, name: '--x' }], fallThrough: ['logo.dark'] };
const okResult = { ok: true, theme: { slots: {} }, report } as unknown as ValidationResult;

function deps(over: Partial<ThemeMessageDeps> = {}): ThemeMessageDeps {
  return {
    validate: vi.fn(async () => okResult),
    generateCss: vi.fn(() => 'html{--a:1}'),
    fetchAsset: vi.fn(async () => new Uint8Array([1])),
    applyStyle: vi.fn(),
    ...over,
  };
}

const msg = (data: unknown) => ({ data });
const request = {
  type: 'kora-theme-preview',
  version: 1,
  document: 'doc',
  assets: { 'a.svg': 'https://c/a.svg' },
};

describe('handleThemeMessage', () => {
  it('ignores messages that are not ours', async () => {
    const d = deps();
    expect(await handleThemeMessage(msg({ type: 'other' }), d)).toBeNull();
    expect(await handleThemeMessage(msg('x'), d)).toBeNull();
    expect(d.applyStyle).not.toHaveBeenCalled();
  });

  it('rejects an unknown version with an error reply and no injection', async () => {
    const d = deps();
    const reply = await handleThemeMessage(msg({ ...request, version: 9 }), d);
    expect(reply?.ok).toBe(false);
    expect(reply?.errors?.[0]?.path).toBe('version');
    expect(d.applyStyle).not.toHaveBeenCalled();
  });

  it('injects CSS and reports on a valid theme', async () => {
    const d = deps();
    const reply = await handleThemeMessage(msg(request), d);
    expect(reply).toEqual({
      type: 'kora-theme-preview-result',
      version: 1,
      ok: true,
      report,
    });
    expect(d.applyStyle).toHaveBeenCalledWith('html{--a:1}');
  });

  it('reads assets through the supplied map and returns null for a missing path', async () => {
    const d = deps({
      validate: vi.fn(async (_doc, read) => {
        expect(await read('a.svg')).toEqual(new Uint8Array([1]));
        expect(await read('missing.svg')).toBeNull();
        return okResult;
      }),
    });
    await handleThemeMessage(msg(request), d);
    expect(d.fetchAsset).toHaveBeenCalledTimes(1);
    expect(d.fetchAsset).toHaveBeenCalledWith('https://c/a.svg');
  });

  it('resolves bundle paths to the consumer URLs in the generated CSS', async () => {
    const generateCss = vi.fn((_theme, resolve: (p: string) => string) => resolve('a.svg'));
    const d = deps({ generateCss });
    await handleThemeMessage(msg(request), d);
    expect(d.applyStyle).toHaveBeenCalledWith('https://c/a.svg');
  });

  it('replies with the errors and does not inject when validation fails', async () => {
    const d = deps({
      validate: vi.fn(async () => ({
        ok: false,
        errors: [{ path: 'tokens.x', message: 'bad' }],
        report,
      })),
    });
    const reply = await handleThemeMessage(msg(request), d);
    expect(reply).toMatchObject({
      ok: false,
      errors: [{ path: 'tokens.x', message: 'bad' }],
      report,
    });
    expect(d.applyStyle).not.toHaveBeenCalled();
  });

  it('treats a failing asset fetch as a missing asset', async () => {
    const d = deps({
      fetchAsset: vi.fn(async () => {
        throw new Error('network');
      }),
      validate: vi.fn(async (_doc, read) => {
        expect(await read('a.svg')).toBeNull();
        return okResult;
      }),
    });
    expect((await handleThemeMessage(msg(request), d))?.ok).toBe(true);
  });

  it('answers a thrown failure with an error reply instead of throwing', async () => {
    const d = deps({
      validate: vi.fn(async () => {
        throw new Error('boom');
      }),
    });
    const reply = await handleThemeMessage(msg(request), d);
    expect(reply).toMatchObject({ ok: false, errors: [{ message: 'boom' }] });
  });

  it('clears the style on a clear message', async () => {
    const d = deps();
    const reply = await handleThemeMessage(
      msg({ type: 'kora-theme-preview', version: 1, clear: true }),
      d,
    );
    expect(reply?.ok).toBe(true);
    expect(d.applyStyle).toHaveBeenCalledWith(null);
    expect(d.validate).not.toHaveBeenCalled();
  });
});

describe('applyThemeStyle', () => {
  it('injects one style element, replaces it, and removes it on null', () => {
    const styles = () => document.head.querySelectorAll(`style[${STYLE_ATTRIBUTE}]`);
    applyThemeStyle('html{--a:1}');
    expect(styles()).toHaveLength(1);
    applyThemeStyle('html{--a:2}');
    expect(styles()).toHaveLength(1);
    expect(styles()[0]?.textContent).toBe('html{--a:2}');
    applyThemeStyle(null);
    expect(styles()).toHaveLength(0);
  });
});
