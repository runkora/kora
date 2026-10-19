/**
 * Reads the custom properties declared in the `:root`, `[data-theme='light']`
 * and `@theme inline` blocks of `tokens.css`. It is not a general CSS parser.
 */

export interface Declaration {
  readonly name: string;
  readonly value: string;
}

export interface Block {
  /** The selector or at-rule prelude, whitespace-collapsed. */
  readonly selector: string;
  readonly declarations: readonly Declaration[];
}

export const RAW_PREFIX = '--hades-raw-';

export function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '');
}

/** Top-level blocks only; nested braces (keyframes) are skipped whole. */
export function parseBlocks(css: string): Block[] {
  const src = stripComments(css);
  const blocks: Block[] = [];
  let i = 0;
  while (i < src.length) {
    const open = src.indexOf('{', i);
    if (open === -1) break;
    const selector = src.slice(i, open).split(';').pop()!.trim().replace(/\s+/g, ' ');
    let depth = 1;
    let j = open + 1;
    while (j < src.length && depth > 0) {
      if (src[j] === '{') depth += 1;
      else if (src[j] === '}') depth -= 1;
      j += 1;
    }
    const body = src.slice(open + 1, j - 1);
    blocks.push({ selector, declarations: body.includes('{') ? [] : parseDeclarations(body) });
    i = j;
  }
  return blocks;
}

/** Splits on `;` outside parentheses and quotes; keeps only `--*` properties. */
export function parseDeclarations(body: string): Declaration[] {
  const out: Declaration[] = [];
  let depth = 0;
  let quote: string | null = null;
  let start = 0;
  const push = (end: number) => {
    const text = body.slice(start, end).trim();
    start = end + 1;
    if (!text.startsWith('--')) return;
    const colon = text.indexOf(':');
    if (colon === -1) return;
    out.push({ name: text.slice(0, colon).trim(), value: text.slice(colon + 1).trim() });
  };
  for (let k = 0; k < body.length; k += 1) {
    const c = body[k]!;
    if (quote) {
      if (c === quote) quote = null;
    } else if (c === "'" || c === '"') quote = c;
    else if (c === '(') depth += 1;
    else if (c === ')') depth -= 1;
    else if (c === ';' && depth === 0) push(k);
  }
  push(body.length);
  return out;
}

export interface TokenSheet {
  /** Raw token names declared in any `:root` block, in declaration order. */
  readonly rawTokens: readonly string[];
  /** Raw tokens given a light value in the `[data-theme='light']` block. */
  readonly lightOverrides: readonly string[];
  /** `@theme inline` declarations, in declaration order. */
  readonly semantic: readonly Declaration[];
}

export function readTokenSheet(css: string): TokenSheet {
  const blocks = parseBlocks(css);
  const rawTokens: string[] = [];
  const lightOverrides: string[] = [];
  const semantic: Declaration[] = [];
  for (const block of blocks) {
    if (block.selector === ':root') {
      for (const d of block.declarations) if (d.name.startsWith(RAW_PREFIX)) rawTokens.push(d.name);
    } else if (block.selector === "[data-theme='light']") {
      for (const d of block.declarations)
        if (d.name.startsWith(RAW_PREFIX)) lightOverrides.push(d.name);
    } else if (block.selector === '@theme inline') {
      semantic.push(...block.declarations);
    }
  }
  return { rawTokens, lightOverrides, semantic };
}

/** Every `var(--name` reference in a value, in order, with duplicates kept. */
export function varReferences(value: string): string[] {
  const out: string[] = [];
  const re = /var\(\s*(--[A-Za-z0-9_-]+)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(value)) !== null) out.push(m[1]!);
  return out;
}

/** True when the value is exactly one `var(--name)` with no fallback. */
export function isDirectReference(value: string): boolean {
  return /^var\(\s*--[A-Za-z0-9_-]+\s*\)$/.test(value.trim());
}
