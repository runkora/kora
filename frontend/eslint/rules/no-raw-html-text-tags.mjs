const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Disallow raw HTML text-flow tags in JSX outside the Terra package. ' +
        'Use Terra typography components (Heading, Text, Code) from @orbit/terra instead.',
    },
    messages: {
      noRawTag:
        'Raw <{{tag}}> is not allowed. ' +
        'Use <Text> (for p) or <Heading variant="…"> (for h1–h6) from @orbit/terra. ' +
        'See frontend/docs/golden-paths/terra-design-system.md',
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
