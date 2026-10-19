/**
 * Reports the whole word `kora` or `orbit` in strings, template chunks and JSX text, so copy
 * reads the name from the brand. There is no inline escape hatch.
 */

// The whole word; the adjacency check below tells an identifier from prose.
const CANDIDATE = /\b(?:kora|orbit)\b/gi;
const JOINER = /[\w\-/]/;

/** Whether the neighbour makes the word an identifier: `-`, `_`, `/`, or `.` before a word char. */
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
        'the brand: `useBrand().productName` (from @runkora/theme/brand-provider) in client ' +
        'components, `getBrand()` (from @runkora/server/theme) in server code. Identifier-shaped ' +
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
