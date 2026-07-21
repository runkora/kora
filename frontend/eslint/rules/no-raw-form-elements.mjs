const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Disallow raw <form>, <label>, <input>, and <output> elements. Use Terra form components.',
    },
    messages: {
      noRawForm:
        'Raw <form> is not allowed. Use <Form> from @orbit/terra. ' +
        'See frontend/docs/golden-paths/terra-design-system.md',
      noRawLabel:
        'Raw <label> is not allowed. Use <Label> from @orbit/terra for text labels, ' +
        'or <Checkbox> for labeled checkbox inputs. ' +
        'See frontend/docs/golden-paths/terra-design-system.md',
      noRawInput:
        'Raw <input> is not allowed. Use <Input> from @orbit/terra for text inputs, ' +
        'or <Checkbox> for checkbox inputs. ' +
        'See frontend/docs/golden-paths/terra-design-system.md',
      noRawOutput:
        'Raw <output> is not allowed. Use <Output> from @orbit/terra. ' +
        'See frontend/docs/golden-paths/terra-design-system.md',
    },
    schema: [],
  },
  create(context) {
    return {
      JSXOpeningElement(node) {
        if (node.name.type !== 'JSXIdentifier') return;
        const tag = node.name.name;
        if (tag === 'form') context.report({ node, messageId: 'noRawForm' });
        else if (tag === 'label') context.report({ node, messageId: 'noRawLabel' });
        else if (tag === 'input') context.report({ node, messageId: 'noRawInput' });
        else if (tag === 'output') context.report({ node, messageId: 'noRawOutput' });
      },
    };
  },
};

export default rule;
