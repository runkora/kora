const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Forbid dot-notation sub-components on Hades imports in Server Components. Hades is a ' +
        '`"use client"` entrypoint, so a Server Component receives a client reference that ' +
        'carries none of the composed sub-components; `X.Sub` is undefined at render.',
    },
    messages: {
      compound:
        '`{{ name }}.{{ property }}` is undefined in a Server Component: Hades is a ' +
        '`"use client"` entrypoint, so `{{ name }}` arrives as a client reference without its ' +
        'composed sub-components, and React throws "Element type is invalid". Import ' +
        '`{{ name }}{{ property }}` by name from `@orbit/hades`, or add `"use client"` to this file.',
    },
  },
  create(context) {
    // Hades components exposing sub-components via Object.assign. A component
    // added to that list without an entry here loses this rule's protection.
    const COMPOUND = new Set(['SidebarLayout']);
    const hadesNames = new Set();
    let isServerComponent = false;

    return {
      Program(node) {
        isServerComponent = !node.body.some(
          (n) => n.type === 'ExpressionStatement' && n.directive === 'use client',
        );
      },
      ImportDeclaration(node) {
        if (node.source.value !== '@orbit/hades') return;
        for (const spec of node.specifiers) {
          if (spec.type === 'ImportSpecifier' && COMPOUND.has(spec.imported.name)) {
            hadesNames.add(spec.local.name);
          }
        }
      },
      // <SidebarLayout.Main> parses as JSXMemberExpression, not MemberExpression.
      JSXMemberExpression(node) {
        if (!isServerComponent) return;
        if (node.object.type !== 'JSXIdentifier' || !hadesNames.has(node.object.name)) return;
        context.report({
          node,
          messageId: 'compound',
          data: { name: node.object.name, property: node.property.name },
        });
      },
      // Non-JSX reference, e.g. `const Main = SidebarLayout.Main`.
      MemberExpression(node) {
        if (!isServerComponent) return;
        if (node.object.type !== 'Identifier' || !hadesNames.has(node.object.name)) return;
        if (node.computed || node.property.type !== 'Identifier') return;
        // Sub-components are capitalised; other property access is not a render.
        if (!/^[A-Z]/.test(node.property.name)) return;
        context.report({
          node,
          messageId: 'compound',
          data: { name: node.object.name, property: node.property.name },
        });
      },
    };
  },
};

export default rule;
