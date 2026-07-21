const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Disallow raw <ul>, <ol>, <li> elements. Use <List> and <ListItem> from @orbit/terra.',
    },
    messages: {
      noRawUl:
        'Raw <ul> is not allowed. Use <List> from @orbit/terra for semantic lists, ' +
        'or <Stack> if used as a pure layout container. ' +
        'See frontend/docs/golden-paths/terra-design-system.md',
      noRawOl:
        'Raw <ol> is not allowed. Use <List ordered> from @orbit/terra for ordered lists, ' +
        'or <Stack> if used as a pure layout container. ' +
        'See frontend/docs/golden-paths/terra-design-system.md',
      noRawLi:
        'Raw <li> is not allowed. Use <ListItem> from @orbit/terra. ' +
        'See frontend/docs/golden-paths/terra-design-system.md',
    },
    schema: [],
  },
  create(context) {
    return {
      JSXOpeningElement(node) {
        if (node.name.type !== 'JSXIdentifier') return;
        const tag = node.name.name;
        if (tag === 'ul') context.report({ node, messageId: 'noRawUl' });
        else if (tag === 'ol') context.report({ node, messageId: 'noRawOl' });
        else if (tag === 'li') context.report({ node, messageId: 'noRawLi' });
      },
    };
  },
};

export default rule;
