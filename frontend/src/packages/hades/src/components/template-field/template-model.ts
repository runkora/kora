/** @tier 1 — Zero Orbit domain knowledge. The pure string-and-caret model behind TemplateField. */

/**
 * Template model — the pure, DOM-adjacent core of TemplateField.
 *
 * The model is always the raw template string, e.g.
 *
 *   "Review {{ pr.title }} on {{ repo.full_name }}"
 *
 * Everything the editor does is expressed as a transformation of that string
 * plus a caret offset measured against it. The contentEditable DOM is a pure
 * render of the model, never a second source of truth, so there is nothing to
 * drift. The functions here are deliberately free of React and free of any
 * component state: they are the part that can be tested exhaustively.
 *
 * Offsets are always offsets into the **serialized** string. A token occupies
 * the full width of its serialization (`{{ pr.title }}` is 14 offsets) but the
 * caret may only rest on its two boundaries — never inside it. `snapOffset`
 * enforces that invariant, and the edit functions apply it to the offsets they
 * accept, so callers can pass a raw browser offset without checking first.
 * (`tokenEndingAt`/`tokenStartingAt` are exact-boundary queries and do not
 * snap.)
 */

/**
 * The token grammar this editor recognises: a dotted path of word characters
 * inside double braces, optionally padded (`{{ pr.title }}` and `{{pr.title}}`
 * are the same token).
 *
 * Exported because a consumer's own renderer has to agree with this grammar
 * for a template written here to mean the same thing when it is rendered. Match
 * against this source rather than restating the pattern, and the two cannot
 * drift.
 */
export const TOKEN_PATTERN_SOURCE = '\\{\\{\\s*([a-zA-Z0-9_.]+)\\s*\\}\\}';

/** A fresh global regex. Never share one: `lastIndex` is mutable state. */
function tokenRegex(): RegExp {
  return new RegExp(TOKEN_PATTERN_SOURCE, 'g');
}

// ── Types ─────────────────────────────────────────────────────────────────────

/** A run of literal text between tokens. */
export interface TemplateTextPart {
  kind: 'text';
  text: string;
}

/** A `{{ token }}` placeholder, stored without its braces or padding. */
export interface TemplateTokenPart {
  kind: 'token';
  token: string;
}

export type TemplatePart = TemplateTextPart | TemplateTokenPart;

/** The location of one token inside a serialized template string. */
export interface TokenSpan {
  /** Offset of the opening brace. */
  start: number;
  /** Offset one past the closing brace. */
  end: number;
  /** The dotted token, e.g. `pr.title`. */
  token: string;
}

/** A string edit paired with where the caret ends up in the new string. */
export interface TemplateEdit {
  value: string;
  caret: number;
}

/** A DOM caret position: a container node and an offset inside it. */
export interface DomPosition {
  node: Node;
  offset: number;
}

// ── Serialization ─────────────────────────────────────────────────────────────

/** Canonical serialization of a single token: `{{ token }}`. */
export function serializeToken(token: string): string {
  return `{{ ${token} }}`;
}

/** Locate every token in a template string, in document order. */
export function tokenSpans(value: string): TokenSpan[] {
  const spans: TokenSpan[] = [];
  for (const match of value.matchAll(tokenRegex())) {
    const start = match.index ?? 0;
    spans.push({ start, end: start + match[0].length, token: match[1] ?? '' });
  }
  return spans;
}

/**
 * Split a template string into literal and token parts. Empty literal runs are
 * never emitted, so `parseTemplate('{{ a }}{{ b }}')` is exactly two parts.
 */
export function parseTemplate(value: string): TemplatePart[] {
  const parts: TemplatePart[] = [];
  let last = 0;
  for (const span of tokenSpans(value)) {
    if (span.start > last) parts.push({ kind: 'text', text: value.slice(last, span.start) });
    parts.push({ kind: 'token', token: span.token });
    last = span.end;
  }
  if (last < value.length) parts.push({ kind: 'text', text: value.slice(last) });
  return parts;
}

/** Rebuild a template string from parts. Inverse of `parseTemplate`. */
export function serialize(parts: readonly TemplatePart[]): string {
  return parts.map((p) => (p.kind === 'text' ? p.text : serializeToken(p.token))).join('');
}

/**
 * Rewrite every token to its canonical `{{ token }}` form, and map a caret
 * offset through the rewrite. This is what turns a `{{pr.title}}` the user
 * typed or pasted as plain text into something the renderer will draw as a
 * pill. A caret that lands inside a rewritten token is pushed to its trailing
 * edge, which is where a person expects to be after finishing typing one.
 */
export function normalizeTemplate(raw: string, caret?: number): TemplateEdit {
  const target = caret === undefined ? null : Math.max(0, Math.min(caret, raw.length));
  let out = '';
  let last = 0;
  let mapped = -1;

  for (const span of tokenSpans(raw)) {
    const literal = raw.slice(last, span.start);
    if (target !== null && mapped < 0 && target >= last && target <= span.start) {
      mapped = out.length + (target - last);
    }
    out += literal;
    out += serializeToken(span.token);
    if (target !== null && mapped < 0 && target > span.start && target <= span.end) {
      mapped = out.length;
    }
    last = span.end;
  }

  if (target !== null && mapped < 0 && target >= last) {
    mapped = out.length + (target - last);
  }
  out += raw.slice(last);

  return { value: out, caret: mapped < 0 ? out.length : Math.min(mapped, out.length) };
}

/** `normalizeTemplate` without the caret bookkeeping. */
export function retokenize(raw: string): string {
  return normalizeTemplate(raw).value;
}

// ── Offsets ───────────────────────────────────────────────────────────────────

/**
 * Clamp an offset into range and push it out of any token interior, to
 * whichever edge of that token is nearer (ties go to the leading edge).
 */
export function snapOffset(value: string, offset: number): number {
  const clamped = Math.max(0, Math.min(offset, value.length));
  for (const span of tokenSpans(value)) {
    if (clamped > span.start && clamped < span.end) {
      return clamped - span.start <= span.end - clamped ? span.start : span.end;
    }
  }
  return clamped;
}

/** The token whose serialization ends exactly at `offset`, if any. */
export function tokenEndingAt(value: string, offset: number): TokenSpan | null {
  return tokenSpans(value).find((s) => s.end === offset) ?? null;
}

/** The token whose serialization starts exactly at `offset`, if any. */
export function tokenStartingAt(value: string, offset: number): TokenSpan | null {
  return tokenSpans(value).find((s) => s.start === offset) ?? null;
}

// ── Edits ─────────────────────────────────────────────────────────────────────

/** Insert text at an offset, then re-canonicalise. */
export function insertAt(value: string, offset: number, insert: string): TemplateEdit {
  const at = snapOffset(value, offset);
  const raw = value.slice(0, at) + insert + value.slice(at);
  return normalizeTemplate(raw, at + insert.length);
}

/** Delete the range between two offsets, snapping both to token boundaries. */
export function deleteRange(value: string, start: number, end: number): TemplateEdit {
  const from = snapOffset(value, Math.min(start, end));
  const to = snapOffset(value, Math.max(start, end));
  if (from === to) return { value, caret: from };
  return normalizeTemplate(value.slice(0, from) + value.slice(to), from);
}

/**
 * Collapse a selection and insert text where it stood: the one edit behind
 * Enter, paste, and anything else that types over a selection. A collapsed
 * selection (start === end) is a plain insertion at that offset.
 */
export function replaceSelection(
  value: string,
  start: number,
  end: number,
  insert: string,
): TemplateEdit {
  const cleared = deleteRange(value, start, end);
  return insertAt(cleared.value, cleared.caret, insert);
}

/** Length of the code point ending at `offset`, so astral characters survive. */
function codePointLengthBefore(value: string, offset: number): number {
  if (offset >= 2) {
    const low = value.charCodeAt(offset - 1);
    const high = value.charCodeAt(offset - 2);
    if (low >= 0xdc00 && low <= 0xdfff && high >= 0xd800 && high <= 0xdbff) return 2;
  }
  return 1;
}

/** Length of the code point starting at `offset`. */
function codePointLengthAfter(value: string, offset: number): number {
  const high = value.charCodeAt(offset);
  if (high >= 0xd800 && high <= 0xdbff && offset + 1 < value.length) {
    const low = value.charCodeAt(offset + 1);
    if (low >= 0xdc00 && low <= 0xdfff) return 2;
  }
  return 1;
}

/**
 * Backspace. A token adjacent to the caret is removed whole: a pill is one
 * thing to a person, so it is one thing to the delete key.
 */
export function deleteBackward(value: string, offset: number): TemplateEdit {
  const at = snapOffset(value, offset);
  if (at === 0) return { value, caret: 0 };
  const span = tokenEndingAt(value, at);
  if (span) return deleteRange(value, span.start, span.end);
  return deleteRange(value, at - codePointLengthBefore(value, at), at);
}

/** Forward delete, with the same all-or-nothing treatment of tokens. */
export function deleteForward(value: string, offset: number): TemplateEdit {
  const at = snapOffset(value, offset);
  if (at >= value.length) return { value, caret: at };
  const span = tokenStartingAt(value, at);
  if (span) return deleteRange(value, span.start, span.end);
  return deleteRange(value, at, at + codePointLengthAfter(value, at));
}

// ── Catalog ───────────────────────────────────────────────────────────────────

/** One insertable (or merely documented) field a template may reference. */
export interface TemplateCatalogEntry {
  /** The dotted token, without braces, e.g. `pr.title`. */
  token: string;
  /** One line describing what the field resolves to. */
  description: string;
  /** A sample value, used by the preview panel, e.g. `Fix login retry`. */
  example?: string;
  /**
   * Marks a dynamic family such as `group.labels.<name>`: listed so a person
   * knows it exists, but not insertable verbatim, because the literal token is
   * not a real field.
   */
  pattern?: boolean;
  /** Heading to file this entry under. Defaults to the token's first segment. */
  group?: string;
}

/** A catalog group in display order, with its entries in catalog order. */
export interface TemplateCatalogGroup {
  name: string;
  entries: TemplateCatalogEntry[];
}

/** Title-case a token prefix for use as a fallback group heading. */
function humanizeSegment(segment: string): string {
  const words = segment.replace(/[_-]+/g, ' ').trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** The heading an entry files under: its `group`, else its token prefix. */
export function groupNameFor(entry: TemplateCatalogEntry): string {
  if (entry.group) return entry.group;
  const [head] = entry.token.split('.');
  return humanizeSegment(head || entry.token);
}

/** Bucket a catalog into groups, preserving first-seen group order. */
export function groupCatalog(catalog: readonly TemplateCatalogEntry[]): TemplateCatalogGroup[] {
  const groups: TemplateCatalogGroup[] = [];
  const index = new Map<string, TemplateCatalogGroup>();
  for (const entry of catalog) {
    const name = groupNameFor(entry);
    let group = index.get(name);
    if (!group) {
      group = { name, entries: [] };
      index.set(name, group);
      groups.push(group);
    }
    group.entries.push(entry);
  }
  return groups;
}

/** Case-insensitive substring match over the token and its description. */
export function filterCatalog(
  catalog: readonly TemplateCatalogEntry[],
  query: string,
): TemplateCatalogEntry[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return [...catalog];
  return catalog.filter(
    (e) => e.token.toLowerCase().includes(needle) || e.description.toLowerCase().includes(needle),
  );
}

/** The sample values a catalog offers, keyed by token. */
export function exampleMap(catalog: readonly TemplateCatalogEntry[]): Map<string, string> {
  const map = new Map<string, string>();
  for (const entry of catalog) {
    if (entry.example !== undefined && !entry.pattern) map.set(entry.token, entry.example);
  }
  return map;
}

// ── Preview ───────────────────────────────────────────────────────────────────

/**
 * Render a template with each token replaced by its sample value. Tokens with
 * no sample, and tokens absent from the map entirely, render as the empty
 * string: an unresolved token contributes nothing rather than leaking its own
 * braces into the output.
 */
export function renderPreview(value: string, examples: ReadonlyMap<string, string>): string {
  return parseTemplate(value)
    .map((part) => (part.kind === 'text' ? part.text : (examples.get(part.token) ?? '')))
    .join('');
}

// ── DOM bridge ────────────────────────────────────────────────────────────────

/** The `data-slot` marking an atomic token pill in the rendered DOM. */
export const TOKEN_SLOT = 'template-token';

/** Is this node one of our atomic, non-editable token pills? */
export function isTokenPill(node: Node | null | undefined): node is HTMLElement {
  return (
    !!node && node.nodeType === 1 && (node as HTMLElement).getAttribute('data-slot') === TOKEN_SLOT
  );
}

function isTextNode(node: Node): node is Text {
  return node.nodeType === 3;
}

/**
 * Zero-width space used as a caret anchor. A contenteditable=false pill with
 * nothing after it is unreachable by arrow keys: the browser has no caret
 * position to give. The editor therefore keeps an anchor text node around
 * edge pills; it is invisible, holds the caret, and never enters the model.
 */
export const CARET_ANCHOR = '\u200B';

/** Strips caret anchors: DOM text may carry them, the model never does. */
function stripAnchors(text: string): string {
  return text.split(CARET_ANCHOR).join('');
}

/**
 * The model text a DOM subtree stands for. Text nodes contribute verbatim,
 * pills contribute their canonical serialization, and a `<br>` contributes a
 * newline (browsers insert them even when we intercept Enter).
 */
export function modelTextOf(node: Node): string {
  if (isTextNode(node)) return stripAnchors(node.nodeValue ?? '');
  if (isTokenPill(node)) return serializeToken(node.getAttribute('data-token') ?? '');
  if (node.nodeName === 'BR') return '\n';
  let out = '';
  node.childNodes.forEach((child) => {
    out += modelTextOf(child);
  });
  return out;
}

/** Read the editor DOM back to a template string. */
export function readModelFromDom(root: Node): string {
  let out = '';
  root.childNodes.forEach((child) => {
    out += modelTextOf(child);
  });
  return out;
}

/** Model length of everything before `node`, walking up to `root`. */
function modelOffsetBefore(root: Node, node: Node): number {
  let acc = 0;
  let cursor: Node | null = node;
  while (cursor && cursor !== root) {
    let sibling = cursor.previousSibling;
    while (sibling) {
      acc += modelTextOf(sibling).length;
      sibling = sibling.previousSibling;
    }
    cursor = cursor.parentNode;
  }
  return acc;
}

/** The pill containing `node`, if `node` sits at or inside one. */
function enclosingPill(root: Node, node: Node): HTMLElement | null {
  let cursor: Node | null = node;
  while (cursor && cursor !== root) {
    if (isTokenPill(cursor)) return cursor;
    cursor = cursor.parentNode;
  }
  return null;
}

/**
 * Map a DOM caret position to a model offset. Positions at or inside a pill
 * snap to one of its edges, because the caret is never allowed to live inside
 * a token.
 */
export function domPositionToModelOffset(root: Node, node: Node | null, offset: number): number {
  if (!node) return 0;

  const pill = enclosingPill(root, node);
  if (pill) {
    const base = modelOffsetBefore(root, pill);
    const atStart = node === pill && offset === 0;
    return atStart ? base : base + modelTextOf(pill).length;
  }

  if (isTextNode(node)) {
    // Count only model characters before the raw offset: an anchor occupies a
    // DOM position but zero model width.
    const raw = node.nodeValue ?? '';
    const before = stripAnchors(raw.slice(0, Math.max(0, Math.min(offset, raw.length))));
    return modelOffsetBefore(root, node) + before.length;
  }

  let acc = modelOffsetBefore(root, node);
  const children = node.childNodes;
  const upto = Math.max(0, Math.min(offset, children.length));
  for (const child of Array.from(children).slice(0, upto)) acc += modelTextOf(child).length;
  return acc;
}

/**
 * Map a model offset back to a DOM caret position. Boundaries next to a pill
 * resolve to a position in the pill's parent (before or after it), never into
 * the pill's own text, so the caret can never be trapped inside one.
 */
export function modelOffsetToDom(root: Node, offset: number): DomPosition {
  const target = Math.max(0, offset);
  let acc = 0;
  const children = Array.from(root.childNodes);

  for (const [i, child] of children.entries()) {
    const len = modelTextOf(child).length;

    if (isTextNode(child)) {
      if (target <= acc + len) return { node: child, offset: Math.max(0, target - acc) };
    } else if (isTokenPill(child) || child.nodeName === 'BR') {
      if (target <= acc) return { node: root, offset: i };
      if (target < acc + len) {
        return { node: root, offset: target - acc <= len / 2 ? i : i + 1 };
      }
      if (target === acc + len) return { node: root, offset: i + 1 };
    } else {
      if (target < acc + len) return modelOffsetToDom(child, target - acc);
      if (target === acc + len) return { node: root, offset: i + 1 };
    }

    acc += len;
  }

  return { node: root, offset: children.length };
}
