const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Disallow raw <button> elements in JSX outside the Hades package. ' +
        'Use <Button> from @runkora/hades for consistent focus styles, hover states, and accessibility.',
    },
    messages: {
      noRawButton:
        'Raw <button> is not allowed. ' +
        'Use <Button> from @runkora/hades. ' +
        'See frontend/docs/reference/hades-design-system.md',
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
