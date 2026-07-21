const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Use <CopyButton> from @orbit/components instead of navigator.clipboard.writeText() directly.',
    },
    messages: {
      noInlineClipboard:
        'Do not call navigator.clipboard.writeText() directly. ' +
        'Use <CopyButton> from @orbit/components, or a Tier 2 component in src/components/ that wraps it.',
    },
    schema: [],
  },
  create(context) {
    return {
      MemberExpression(node) {
        if (
          node.object?.type === 'MemberExpression' &&
          node.object.object?.name === 'navigator' &&
          node.object.property?.name === 'clipboard' &&
          node.property?.name === 'writeText'
        ) {
          context.report({ node, messageId: 'noInlineClipboard' });
        }
      },
    };
  },
};

export default rule;
