const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Disallow raw <button> elements in JSX outside the Terra package. ' +
        'Use <Button> from @orbit/terra for consistent focus styles, hover states, and accessibility.',
    },
    messages: {
      noRawButton:
        'Raw <button> is not allowed. ' +
        'Use <Button> from @orbit/terra. ' +
        'See frontend/docs/golden-paths/terra-design-system.md',
    },
    schema: [],
  },
  create(context) {
    return {
      JSXOpeningElement(node) {
        if (node.name.type !== 'JSXIdentifier') return;
        if (node.name.name === 'button') {
          context.report({ node, messageId: 'noRawButton' });
        }
      },
    };
  },
};

export default rule;
