const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Disallow raw inline HTML elements in JSX outside the Hades package. ' +
        'Use Hades typography components. If one is missing, propose it to a human engineer for approval before modifying Hades.',
    },
    messages: {
      span:
        'Raw <span> is not allowed. Use <Text> or <Text asChild> from @runkora/hades. ' +
        'See frontend/docs/reference/hades-design-system.md',
      formatting:
        'Raw <{{tag}}> is not allowed. ' +
        'Use <Text weight="heavy"> from @runkora/hades for <strong> and <b>. ' +
        'See frontend/docs/reference/hades-design-system.md',
      code:
        'Raw <{{tag}}> is not allowed. ' +
        'Use <Code> from @runkora/hades for inline code, keyboard input, sample output, and variables. ' +
        'See frontend/docs/reference/hades-design-system.md',
      missing:
        'Raw <{{tag}}> has no Hades equivalent yet. ' +
        'Propose a new Hades component to a human engineer for approval — do not modify Hades unilaterally. ' +
        'See frontend/docs/reference/hades-design-system.md',
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
