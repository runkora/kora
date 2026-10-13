const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Disallow raw HTML text-flow tags in JSX outside the Hades package. ' +
        'Use Hades typography components (Heading, Text, Code) from @clear-route/hades instead.',
    },
    messages: {
      noRawTag:
        'Raw <{{tag}}> is not allowed. ' +
        'Use <Text> (for p) or <Heading variant="…"> (for h1–h6) from @clear-route/hades. ' +
        'See frontend/docs/reference/hades-design-system.md',
    },
  },
  create(context) {
    const BLOCKED = new Set(['p', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6']);
    return {
      JSXOpeningElement(node) {
        const name = node.name.type === 'JSXIdentifier' ? node.name.name : null;
        if (name && BLOCKED.has(name)) {
          context.report({ node, messageId: 'noRawTag', data: { tag: name } });
        }
      },
    };
  },
};

export default rule;
