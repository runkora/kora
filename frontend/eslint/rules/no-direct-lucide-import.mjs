const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        "Ban direct lucide-react imports in product code — use Hades' Icon component instead.",
    },
    messages: {
      noDirectLucide:
        "Import Lucide icons via Hades' Icon component (<Icon name=\"...\" />) — not directly from 'lucide-react'. " +
        'Add new icons to the Hades registry in src/packages/hades/src/components/icon/registry.ts. ' +
        'See frontend/docs/reference/hades-design-system.md',
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
