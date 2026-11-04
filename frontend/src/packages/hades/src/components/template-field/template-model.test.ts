import { describe, it, expect } from 'vitest';
import {
  deleteBackward,
  deleteForward,
  deleteRange,
  domPositionToModelOffset,
  exampleMap,
  filterCatalog,
  groupCatalog,
  groupNameFor,
  insertAt,
  modelOffsetToDom,
  normalizeTemplate,
  parseTemplate,
  readModelFromDom,
  renderPreview,
  replaceSelection,
  retokenize,
  serialize,
  serializeToken,
  snapOffset,
  tokenSpans,
  type TemplateCatalogEntry,
} from './template-model';

const TPL = 'Review {{ pr.title }} on {{ repo.full_name }}';

describe('parseTemplate / serialize', () => {
  it('splits literal text and tokens', () => {
    expect(parseTemplate(TPL)).toEqual([
      { kind: 'text', text: 'Review ' },
      { kind: 'token', token: 'pr.title' },
      { kind: 'text', text: ' on ' },
      { kind: 'token', token: 'repo.full_name' },
    ]);
  });

  it('returns no parts for an empty string', () => {
    expect(parseTemplate('')).toEqual([]);
    expect(serialize([])).toBe('');
  });

  it('emits no empty text parts between adjacent tokens', () => {
    expect(parseTemplate('{{ a }}{{ b }}')).toEqual([
      { kind: 'token', token: 'a' },
      { kind: 'token', token: 'b' },
    ]);
  });

  it('treats a template with no tokens as one text part', () => {
    expect(parseTemplate('just words')).toEqual([{ kind: 'text', text: 'just words' }]);
  });

  it('ignores malformed placeholders', () => {
    expect(parseTemplate('{{ not-a-token }}')).toEqual([
      { kind: 'text', text: '{{ not-a-token }}' },
    ]);
    expect(parseTemplate('{{ unclosed')).toEqual([{ kind: 'text', text: '{{ unclosed' }]);
  });

  it('round-trips canonical templates', () => {
    for (const s of [TPL, '', '{{ a }}', 'a{{ b }}', '{{ a }}b', '{{ a }}{{ b }}', 'no tokens']) {
      expect(serialize(parseTemplate(s))).toBe(s);
    }
  });

  it('serializes a token with canonical padding', () => {
    expect(serializeToken('pr.title')).toBe('{{ pr.title }}');
  });

  it('reports token spans', () => {
    expect(tokenSpans(TPL)).toEqual([
      { start: 7, end: 21, token: 'pr.title' },
      { start: 25, end: 45, token: 'repo.full_name' },
    ]);
  });
});

describe('normalizeTemplate / retokenize', () => {
  it('canonicalises padding', () => {
    expect(retokenize('{{pr.title}}')).toBe('{{ pr.title }}');
    expect(retokenize('{{   pr.title   }}')).toBe('{{ pr.title }}');
  });

  it('leaves canonical input untouched', () => {
    expect(retokenize(TPL)).toBe(TPL);
  });

  it('leaves malformed placeholders as literal text', () => {
    expect(retokenize('{{ not-a-token }}')).toBe('{{ not-a-token }}');
  });

  it('maps a caret in the literal prefix unchanged', () => {
    expect(normalizeTemplate('ab{{x}}', 1)).toEqual({ value: 'ab{{ x }}', caret: 1 });
  });

  it('pushes a caret inside a rewritten token to its trailing edge', () => {
    // Caret sits just after "{{x" — the user has only just closed the braces.
    expect(normalizeTemplate('{{x}}', 5)).toEqual({ value: '{{ x }}', caret: 7 });
    expect(normalizeTemplate('{{x}}', 3)).toEqual({ value: '{{ x }}', caret: 7 });
  });

  it('maps a caret after a rewritten token by the length delta', () => {
    expect(normalizeTemplate('{{x}}tail', 7)).toEqual({ value: '{{ x }}tail', caret: 9 });
  });

  it('maps a caret at the leading edge of a token to the same edge', () => {
    expect(normalizeTemplate('ab{{x}}', 2)).toEqual({ value: 'ab{{ x }}', caret: 2 });
  });

  it('defaults the caret to the end when none is given', () => {
    expect(normalizeTemplate('{{x}}')).toEqual({ value: '{{ x }}', caret: 7 });
  });

  it('clamps an out-of-range caret', () => {
    expect(normalizeTemplate('abc', 99).caret).toBe(3);
    expect(normalizeTemplate('abc', -5).caret).toBe(0);
  });
});

describe('snapOffset', () => {
  it('leaves offsets outside tokens alone', () => {
    expect(snapOffset(TPL, 0)).toBe(0);
    expect(snapOffset(TPL, 7)).toBe(7);
    expect(snapOffset(TPL, 21)).toBe(21);
  });

  it('pushes an offset inside a token to the nearer edge', () => {
    expect(snapOffset(TPL, 8)).toBe(7);
    expect(snapOffset(TPL, 20)).toBe(21);
  });

  it('breaks a tie towards the leading edge', () => {
    // "{{ x }}" is 7 wide at offset 0; the midpoint 3.5 rounds down at 3.
    expect(snapOffset('{{ x }}', 3)).toBe(0);
    expect(snapOffset('{{ x }}', 4)).toBe(7);
  });

  it('clamps to the string bounds', () => {
    expect(snapOffset('abc', -1)).toBe(0);
    expect(snapOffset('abc', 99)).toBe(3);
  });
});

describe('insertAt', () => {
  it('inserts plain text and advances the caret', () => {
    expect(insertAt('ab', 1, 'X')).toEqual({ value: 'aXb', caret: 2 });
  });

  it('inserts a token at the end', () => {
    expect(insertAt('Hi ', 3, serializeToken('pr.title'))).toEqual({
      value: 'Hi {{ pr.title }}',
      caret: 17,
    });
  });

  it('never splits an existing token', () => {
    const { value } = insertAt('{{ a }}', 3, 'X');
    expect(value).toBe('X{{ a }}');
  });

  it('canonicalises text that completes a token', () => {
    expect(insertAt('{{a}', 4, '}')).toEqual({ value: '{{ a }}', caret: 7 });
  });

  it('inserts into an empty template', () => {
    expect(insertAt('', 0, '{{ a }}')).toEqual({ value: '{{ a }}', caret: 7 });
  });
});

describe('delete operations', () => {
  it('deletes a plain character backwards', () => {
    expect(deleteBackward('abc', 2)).toEqual({ value: 'ac', caret: 1 });
  });

  it('is a no-op backspacing at the start', () => {
    expect(deleteBackward('abc', 0)).toEqual({ value: 'abc', caret: 0 });
  });

  it('removes a whole token when backspacing at its trailing edge', () => {
    expect(deleteBackward('a{{ x }}b', 8)).toEqual({ value: 'ab', caret: 1 });
  });

  it('removes a whole token when forward-deleting at its leading edge', () => {
    expect(deleteForward('a{{ x }}b', 1)).toEqual({ value: 'ab', caret: 1 });
  });

  it('deletes a plain character forwards', () => {
    expect(deleteForward('abc', 1)).toEqual({ value: 'ac', caret: 1 });
  });

  it('is a no-op forward-deleting at the end', () => {
    expect(deleteForward('abc', 3)).toEqual({ value: 'abc', caret: 3 });
  });

  it('deletes an astral character whole', () => {
    expect(deleteBackward('a🙂', 3)).toEqual({ value: 'a', caret: 1 });
    expect(deleteForward('🙂a', 0)).toEqual({ value: 'a', caret: 0 });
  });

  it('deletes a range, snapping both ends out of tokens', () => {
    expect(deleteRange('a{{ x }}b', 2, 9)).toEqual({ value: 'a', caret: 1 });
  });

  it('accepts a reversed range', () => {
    expect(deleteRange('abcd', 3, 1)).toEqual({ value: 'ad', caret: 1 });
  });

  it('is a no-op for a collapsed range', () => {
    expect(deleteRange('abcd', 2, 2)).toEqual({ value: 'abcd', caret: 2 });
  });
});

describe('replaceSelection', () => {
  it('inserts at a collapsed selection', () => {
    expect(replaceSelection('ab', 1, 1, 'X')).toEqual({ value: 'aXb', caret: 2 });
  });

  it('replaces the selected range', () => {
    expect(replaceSelection('abcd', 1, 3, 'X')).toEqual({ value: 'aXd', caret: 2 });
  });

  it('accepts a reversed selection', () => {
    expect(replaceSelection('abcd', 3, 1, 'X')).toEqual({ value: 'aXd', caret: 2 });
  });

  it('removes a token the selection covers, snapping partial coverage out', () => {
    expect(replaceSelection('a{{ x }}b', 2, 9, 'X')).toEqual({ value: 'aX', caret: 2 });
  });

  it('canonicalises an insertion that completes a token', () => {
    expect(replaceSelection('{{a}', 4, 4, '}')).toEqual({ value: '{{ a }}', caret: 7 });
  });

  it('inserts a newline over a selection, as the Enter key does', () => {
    expect(replaceSelection('ab', 0, 2, '\n')).toEqual({ value: '\n', caret: 1 });
  });
});

describe('renderPreview', () => {
  const examples = new Map([
    ['pr.title', 'Fix login retry'],
    ['repo.full_name', 'runkora/kora'],
  ]);

  it('substitutes sample values', () => {
    expect(renderPreview(TPL, examples)).toBe('Review Fix login retry on runkora/kora');
  });

  it('renders unknown fields as the empty string', () => {
    expect(renderPreview('a {{ nope }} b', examples)).toBe('a  b');
  });

  it('passes a template with no tokens through', () => {
    expect(renderPreview('plain', examples)).toBe('plain');
  });
});

// ── DOM bridge ────────────────────────────────────────────────────────────────

/** Build the editor DOM the component renders, without React. */
function buildEditor(parts: Array<string | { token: string }>): HTMLElement {
  const root = document.createElement('div');
  for (const part of parts) {
    if (typeof part === 'string') {
      root.appendChild(document.createTextNode(part));
    } else {
      const pill = document.createElement('span');
      pill.setAttribute('data-slot', 'template-token');
      pill.setAttribute('data-token', part.token);
      pill.setAttribute('contenteditable', 'false');
      pill.textContent = part.token;
      root.appendChild(pill);
    }
  }
  return root;
}

describe('readModelFromDom', () => {
  it('reads text and pills back to a template string', () => {
    const root = buildEditor([
      'Review ',
      { token: 'pr.title' },
      ' on ',
      { token: 'repo.full_name' },
    ]);
    expect(readModelFromDom(root)).toBe(TPL);
  });

  it('reads an empty editor as an empty string', () => {
    expect(readModelFromDom(buildEditor([]))).toBe('');
  });

  it('reads a browser-inserted <br> as a newline', () => {
    const root = buildEditor(['a']);
    root.appendChild(document.createElement('br'));
    root.appendChild(document.createTextNode('b'));
    expect(readModelFromDom(root)).toBe('a\nb');
  });

  it('reads nested markup by flattening its text', () => {
    const root = buildEditor(['a']);
    const wrapper = document.createElement('span');
    wrapper.textContent = 'bc';
    root.appendChild(wrapper);
    expect(readModelFromDom(root)).toBe('abc');
  });
});

describe('domPositionToModelOffset', () => {
  const root = buildEditor(['Review ', { token: 'pr.title' }, ' on ']);
  const [text0, pill, text1] = Array.from(root.childNodes) as [ChildNode, ChildNode, ChildNode];

  it('maps an offset inside a leading text node', () => {
    expect(domPositionToModelOffset(root, text0, 0)).toBe(0);
    expect(domPositionToModelOffset(root, text0, 7)).toBe(7);
  });

  it('maps a container position before a pill', () => {
    expect(domPositionToModelOffset(root, root, 1)).toBe(7);
  });

  it('maps a container position after a pill', () => {
    expect(domPositionToModelOffset(root, root, 2)).toBe(21);
  });

  it('maps an offset inside the trailing text node', () => {
    expect(domPositionToModelOffset(root, text1, 0)).toBe(21);
    expect(domPositionToModelOffset(root, text1, 4)).toBe(25);
  });

  it('snaps a position at the pill start to its leading edge', () => {
    expect(domPositionToModelOffset(root, pill, 0)).toBe(7);
  });

  it('snaps a position inside the pill to its trailing edge', () => {
    expect(domPositionToModelOffset(root, pill, 1)).toBe(21);
    expect(domPositionToModelOffset(root, pill.firstChild, 3)).toBe(21);
  });

  it('maps the container start and end', () => {
    expect(domPositionToModelOffset(root, root, 0)).toBe(0);
    expect(domPositionToModelOffset(root, root, 3)).toBe(25);
  });

  it('clamps an over-long text offset', () => {
    expect(domPositionToModelOffset(root, text0, 99)).toBe(7);
  });

  it('returns 0 for a null node', () => {
    expect(domPositionToModelOffset(root, null, 0)).toBe(0);
  });

  it('handles an empty editor', () => {
    const empty = buildEditor([]);
    expect(domPositionToModelOffset(empty, empty, 0)).toBe(0);
  });
});

describe('modelOffsetToDom', () => {
  const root = buildEditor(['Review ', { token: 'pr.title' }, ' on ']);
  const [text0, , text1] = Array.from(root.childNodes);

  it('maps an offset inside the leading text node', () => {
    expect(modelOffsetToDom(root, 3)).toEqual({ node: text0, offset: 3 });
  });

  it('maps the boundary before a pill into the preceding text node', () => {
    // Both {text0, 7} and {root, 1} are the same caret; preferring the text
    // node keeps typing continuous with the text already there.
    expect(modelOffsetToDom(root, 7)).toEqual({ node: text0, offset: 7 });
  });

  it('maps the boundary after a pill to a container position', () => {
    // {root, 2} sits between the pill and the following text node — the same
    // caret as {text1, 0}, expressed outside the atomic pill.
    expect(modelOffsetToDom(root, 21)).toEqual({ node: root, offset: 2 });
  });

  it('never lands inside a pill: an interior offset snaps to an edge', () => {
    const pillOnly = buildEditor([{ token: 'x' }]);
    expect(modelOffsetToDom(pillOnly, 2)).toEqual({ node: pillOnly, offset: 0 });
    expect(modelOffsetToDom(pillOnly, 6)).toEqual({ node: pillOnly, offset: 1 });
  });

  it('maps offset 0 of a pill-first editor to the container start', () => {
    const pillFirst = buildEditor([{ token: 'x' }, 'tail']);
    expect(modelOffsetToDom(pillFirst, 0)).toEqual({ node: pillFirst, offset: 0 });
  });

  it('maps the end of the template', () => {
    expect(modelOffsetToDom(root, 25)).toEqual({ node: text1, offset: 4 });
  });

  it('maps past the end to the container end', () => {
    expect(modelOffsetToDom(root, 999)).toEqual({ node: root, offset: 3 });
  });

  it('handles an empty editor', () => {
    const empty = buildEditor([]);
    expect(modelOffsetToDom(empty, 0)).toEqual({ node: empty, offset: 0 });
  });

  it('round-trips every offset of a template through the DOM and back', () => {
    for (let offset = 0; offset <= TPL.length; offset += 1) {
      const full = buildEditor([
        'Review ',
        { token: 'pr.title' },
        ' on ',
        { token: 'repo.full_name' },
      ]);
      const position = modelOffsetToDom(full, offset);
      const back = domPositionToModelOffset(full, position.node, position.offset);
      expect(back).toBe(snapOffset(TPL, offset));
    }
  });
});

// ── Catalog ───────────────────────────────────────────────────────────────────

const CATALOG: TemplateCatalogEntry[] = [
  { token: 'pr.title', description: 'Title', example: 'Fix login retry', group: 'Pull request' },
  { token: 'pr.number', description: 'Number', example: '412', group: 'Pull request' },
  { token: 'repo.full_name', description: 'Owner and name', example: 'runkora/kora' },
  { token: 'group.labels.<name>', description: 'Any label', pattern: true },
];

describe('catalog helpers', () => {
  it('takes an explicit group, else the token prefix', () => {
    expect(groupNameFor(CATALOG[0]!)).toBe('Pull request');
    expect(groupNameFor(CATALOG[2]!)).toBe('Repo');
  });

  it('groups in first-seen order', () => {
    expect(groupCatalog(CATALOG).map((g) => g.name)).toEqual(['Pull request', 'Repo', 'Group']);
    expect(groupCatalog(CATALOG)[0]!.entries).toHaveLength(2);
  });

  it('filters on token and description, case-insensitively', () => {
    expect(filterCatalog(CATALOG, 'NUM').map((e) => e.token)).toEqual(['pr.number']);
    expect(filterCatalog(CATALOG, 'owner').map((e) => e.token)).toEqual(['repo.full_name']);
  });

  it('returns everything for an empty query', () => {
    expect(filterCatalog(CATALOG, '   ')).toHaveLength(4);
  });

  it('excludes pattern entries from the example map', () => {
    expect([...exampleMap(CATALOG).keys()]).toEqual(['pr.title', 'pr.number', 'repo.full_name']);
  });
});
