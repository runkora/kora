const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Require `import "server-only"` in server-side modules to prevent accidental client bundle inclusion.',
    },
    messages: {
      missing:
        'Server-side modules must have `import "server-only"` as the first import ' +
        '(after any JSDoc block). Either the guard is absent or it is not the first ImportDeclaration.',
    },
  },
  create(context) {
    return {
      Program(node) {
        // Collect all import declarations in source order, then check that
        // "server-only" is the very first one (guardIdx === 0).
        // guardIdx === -1 → guard is absent entirely.
        // guardIdx  >  0 → guard exists but is not the first import.
        // Both cases violate the AGENTS.md position requirement.
        const imports = node.body.filter((n) => n.type === 'ImportDeclaration');
        const guardIdx = imports.findIndex((n) => n.source.value === 'server-only');
        if (guardIdx !== 0) {
          context.report({ node, messageId: 'missing' });
        }
      },
    };
  },
};

export default rule;
