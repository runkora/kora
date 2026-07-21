const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        "Ban direct lucide-react imports in product code — use Terra's Icon component instead.",
    },
    messages: {
      noDirectLucide:
        "Import Lucide icons via Terra's Icon component (<Icon name=\"...\" />) — not directly from 'lucide-react'. " +
        'Add new icons to the Terra registry in src/packages/terra/src/components/icon/registry.ts. ' +
        'See frontend/docs/golden-paths/terra-design-system.md',
    },
    schema: [],
  },
  create(context) {
    return {
      ImportDeclaration(node) {
        if (node.source.value === 'lucide-react') {
          context.report({ node, messageId: 'noDirectLucide' });
        }
      },
    };
  },
};

export default rule;
