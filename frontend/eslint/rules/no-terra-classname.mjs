const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Disallow `className` props on Terra components (@orbit/terra) outside the Terra package. ' +
        'Use a wrapper element for layout, or propose a new variant to a human engineer for approval.',
    },
    messages: {
      noClassName:
        '`className` is not allowed on Terra component <{{component}}>. ' +
        'For layout: use `<Stack asChild>` or `<Grid asChild>` and put layout classes on the child element, not on the Terra component. ' +
        'For a style variant: propose a new variant to a human engineer for approval — do not modify Terra unilaterally. ' +
        'Never route around this with raw HTML wrappers.',
    },
  },
  create(context) {
    // Track which local names in this file are imported from @orbit/terra.
    const terraComponents = new Set();

    return {
      ImportDeclaration(node) {
        if (node.source.value !== '@orbit/terra') return;
        for (const specifier of node.specifiers) {
          if (specifier.type === 'ImportSpecifier') {
            terraComponents.add(specifier.local.name);
          }
        }
      },

      JSXOpeningElement(node) {
        // Only care about named components (uppercase first letter).
        const componentName = node.name.type === 'JSXIdentifier' ? node.name.name : null;
        if (!componentName || !terraComponents.has(componentName)) return;
        const classNameAttr = node.attributes.find(
          (attr) =>
            attr.type === 'JSXAttribute' &&
            attr.name.type === 'JSXIdentifier' &&
            attr.name.name === 'className',
        );
        if (classNameAttr) {
          context.report({
            node: classNameAttr,
            messageId: 'noClassName',
            data: { component: componentName },
          });
        }
      },
    };
  },
};

export default rule;
