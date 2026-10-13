/**
 * no-product-name-literal
 *
 * The product name is a deployment value (white-labelling theme ADR, decision
 * 5): a theme can rename the product, so user-facing copy must read it from the
 * brand rather than spell it. This rule reports a string literal, a template
 * literal chunk or JSX text containing the whole word `kora` or `orbit`,
 * case-insensitively, when the match is not immediately adjacent to `-`, `_`,
 * `.` or `/` on either side. Identifier-shaped uses therefore pass (`orbit.io`,
 * `kora-base`, `orbit-system`, `@clear-route/hades`, `ORBIT_API_KEY`) and prose
 * fails ("Log out of kora?", `"orbit"` inside a config snippet). One
 * refinement to the ADR's wording: a `.` only joins when a word character sits
 * on its far side, so a sentence that ends in the product name is prose too.
 *
 * Literals that are never copy are skipped: import and export sources, object
 * property keys, computed member keys (`claims['orbit']`) and literal types.
 * There is no inline escape hatch; a hit that is not user-facing is either an
 * identifier (keep it adjacent to one of the four characters, or move it out of
 * the app tree) or a path-scoped `ignores` entry in eslint.config.mjs.
 */

// A candidate is the whole word `kora` or `orbit`; the adjacency check below
// decides whether it is an identifier or prose.
const CANDIDATE = /\b(?:kora|orbit)\b/gi;
const JOINER = /[\w\-/]/;

/**
 * Whether the character beside the word makes it part of an identifier: a
 * letter, digit, `_`, `-` or `/` always does (`kora-base`, `orbit/authz`,
 * `ORBIT_API_KEY`); a `.` does only when a word character sits on its other
 * side (`orbit.io`, `claims.orbit`), so the full stop that ends a sentence
 * ("provisioned in kora.") is still prose.
 */
function joinedThrough(text, index, direction) {
  const c = text[index];
  if (c === undefined) return false;
  if (JOINER.test(c)) return true;
  if (c === '.') {
    const beyond = text[index + direction];
    return beyond !== undefined && /\w/.test(beyond);
  }
  return false;
}

function firstMatch(text) {
  if (typeof text !== 'string' || text.length < 4) return null;
  CANDIDATE.lastIndex = 0;
  let m;
  while ((m = CANDIDATE.exec(text)) !== null) {
    const start = m.index;
    const end = start + m[0].length;
    if (!joinedThrough(text, start - 1, -1) && !joinedThrough(text, end, +1)) return m[0];
  }
  return null;
}

/** Literals that name things rather than say them. */
function isIdentifierPosition(node) {
  const parent = node.parent;
  if (!parent) return false;
  switch (parent.type) {
    case 'ImportDeclaration':
    case 'ExportNamedDeclaration':
    case 'ExportAllDeclaration':
    case 'ImportExpression':
    case 'TSLiteralType':
    case 'TSExternalModuleReference':
      return true;
    case 'Property':
      return parent.key === node;
    case 'MemberExpression':
      return parent.computed && parent.property === node;
    case 'ImportAttribute':
      return true;
    default:
      return false;
  }
}

const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Disallow the product name (`kora`, `orbit`) as a word in string literals, template ' +
        'literals and JSX text; user-facing copy reads it from the brand.',
    },
    messages: {
      literal:
        'Product name "{{word}}" in a literal. A theme can rename the product, so read it from ' +
        'the brand: `useBrand().productName` (from @orbit/theme/brand-provider) in client ' +
        'components, `getBrand()` (from @orbit/server/theme) in server code. Identifier-shaped ' +
        'uses such as `orbit.io` or `kora-base` are not flagged.',
    },
    schema: [],
  },
  create(context) {
    function report(node, word) {
      context.report({ node, messageId: 'literal', data: { word } });
    }
    return {
      Literal(node) {
        if (typeof node.value !== 'string') return;
        if (isIdentifierPosition(node)) return;
        const word = firstMatch(node.value);
        if (word) report(node, word);
      },
      TemplateElement(node) {
        const word = firstMatch(node.value.cooked ?? node.value.raw);
        if (word) report(node, word);
      },
      JSXText(node) {
        const word = firstMatch(node.value);
        if (word) report(node, word);
      },
    };
  },
};

export default rule;
