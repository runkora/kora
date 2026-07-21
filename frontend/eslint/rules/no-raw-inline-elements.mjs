const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Disallow raw inline HTML elements in JSX outside the Terra package. ' +
        'Use Terra typography components. If one is missing, propose it to a human engineer for approval before modifying Terra.',
    },
    messages: {
      span:
        'Raw <span> is not allowed. Use <Text> or <Text asChild> from @orbit/terra. ' +
        'See frontend/docs/golden-paths/terra-design-system.md',
      formatting:
        'Raw <{{tag}}> is not allowed. ' +
        'Use <Text weight="heavy"> from @orbit/terra for <strong> and <b>. ' +
        'See frontend/docs/golden-paths/terra-design-system.md',
      code:
        'Raw <{{tag}}> is not allowed. ' +
        'Use <Code> from @orbit/terra for inline code, keyboard input, sample output, and variables. ' +
        'See frontend/docs/golden-paths/terra-design-system.md',
      missing:
        'Raw <{{tag}}> has no Terra equivalent yet. ' +
        'Propose a new Terra component to a human engineer for approval — do not modify Terra unilaterally. ' +
        'See frontend/docs/golden-paths/terra-design-system.md',
    },
    schema: [],
  },
  create(context) {
    const FORMATTING = new Set(['strong', 'b']);
    const CODE_ELEMENTS = new Set(['code', 'kbd', 'samp', 'var']);
    const MISSING = new Set(['em', 'i', 'mark', 'abbr', 's', 'u', 'sub', 'sup']);
    return {
      JSXOpeningElement(node) {
        if (node.name.type !== 'JSXIdentifier') return;
        const tag = node.name.name;
        if (tag === 'span') {
          context.report({ node, messageId: 'span' });
        } else if (FORMATTING.has(tag)) {
          context.report({ node, messageId: 'formatting', data: { tag } });
        } else if (CODE_ELEMENTS.has(tag)) {
          context.report({ node, messageId: 'code', data: { tag } });
        } else if (MISSING.has(tag)) {
          context.report({ node, messageId: 'missing', data: { tag } });
        }
      },
    };
  },
};

export default rule;
