/**
 * WCAG 2.x contrast for `#hex` and `oklch()` colours. Alpha is ignored, and an
 * out-of-gamut OKLCH colour is clipped per channel, so its ratio is approximate.
 */

/** Linear-light sRGB channels, each in [0, 1]. */
interface LinearRgb {
  readonly r: number;
  readonly g: number;
  readonly b: number;
}

const clamp01 = (n: number): number => Math.min(1, Math.max(0, n));

const srgbToLinear = (c: number): number =>
  c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);

function parseHex(value: string): LinearRgb | null {
  const m = /^#([0-9a-fA-F]{3,8})$/.exec(value.trim());
  if (!m) return null;
  let h = m[1]!;
  if (h.length === 3 || h.length === 4) h = [...h].map((c) => c + c).join('');
  if (h.length !== 6 && h.length !== 8) return null;
  const ch = (i: number) => srgbToLinear(parseInt(h.slice(i, i + 2), 16) / 255);
  return { r: ch(0), g: ch(2), b: ch(4) };
}

/** One OKLCH channel: a number, a percentage (of `percentBase`) or `none`. */
function channel(token: string, percentBase: number): number | null {
  if (token === 'none') return 0;
  const m = /^([+-]?(?:\d+(?:\.\d*)?|\.\d+))(%?)$/.exec(token);
  if (!m) return null;
  const n = Number(m[1]);
  return m[2] === '%' ? (n / 100) * percentBase : n;
}

function parseOklch(value: string): LinearRgb | null {
  // Split into tokens. A single regex of `\S+\s+\S+` groups backtracks quadratically.
  const v = value.trim();
  if (!v.startsWith('oklch(') || !v.endsWith(')')) return null;
  const inner = v.slice('oklch('.length, -1);
  const slash = inner.indexOf('/');
  const main = slash === -1 ? inner : inner.slice(0, slash);
  if (slash !== -1) {
    // The alpha is ignored, but it must be one token.
    const alpha = inner.slice(slash + 1).trim();
    if (alpha === '' || /\s/.test(alpha)) return null;
  }
  const parts = main.trim().split(/\s+/);
  if (parts.length !== 3) return null;
  const l0 = channel(parts[0]!, 1);
  const c0 = channel(parts[1]!, 0.4);
  const H = channel(parts[2]!, 360);
  if (l0 === null || c0 === null || H === null) return null;
  const L = clamp01(l0);
  const C = Math.max(0, c0);
  const hue = (H * Math.PI) / 180;
  const a = C * Math.cos(hue);
  const b = C * Math.sin(hue);
  const l = Math.pow(L + 0.3963377774 * a + 0.2158037573 * b, 3);
  const mm = Math.pow(L - 0.1055613458 * a - 0.0638541728 * b, 3);
  const s = Math.pow(L - 0.0894841775 * a - 1.291485548 * b, 3);
  return {
    r: clamp01(4.0767416621 * l - 3.3077115913 * mm + 0.2309699292 * s),
    g: clamp01(-1.2684380046 * l + 2.6097574011 * mm - 0.3413193965 * s),
    b: clamp01(-0.0041960863 * l - 0.7034186147 * mm + 1.707614701 * s),
  };
}

/** The colour as linear-light sRGB, or null when the value is neither a hex nor an `oklch()` colour. */
function parseColor(value: string): LinearRgb | null {
  return parseHex(value) ?? parseOklch(value);
}

/** WCAG relative luminance of a linear-light sRGB colour. */
function relativeLuminance({ r, g, b }: LinearRgb): number {
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** WCAG 2.x contrast ratio between two colour values, 1 to 21; null when either cannot be parsed. */
export function contrastRatio(a: string, b: string): number | null {
  const ca = parseColor(a);
  const cb = parseColor(b);
  if (!ca || !cb) return null;
  const la = relativeLuminance(ca);
  const lb = relativeLuminance(cb);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}
