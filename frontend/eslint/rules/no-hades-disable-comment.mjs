const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Disallow eslint-disable directive comments that suppress Hades design-system rules. ' +
        'Fix the violation by using the correct Hades component instead of suppressing the lint error.',
    },
    messages: {
      noDisableComment:
        'Suppressing Hades rule "{{rule}}" with a disable directive is forbidden. ' +
        'Fix the violation by using the correct Hades component. ' +
        'If no Hades component covers your use case, propose one to a human engineer for approval — do not modify Hades unilaterally. ' +
        'See frontend/docs/reference/hades-design-system.md',
    },
    schema: [],
  },
  create(context) {
    const HADES_RULES = new Set([
      'local/no-raw-layout-elements',
      'local/no-raw-inline-elements',
      'local/no-raw-html-text-tags',
      'local/no-hades-classname',
      'local/no-direct-lucide-import',
      'local/no-raw-interactive-elements',
      'local/no-raw-list-elements',
      'local/no-raw-form-elements',
      'local/no-hardcoded-color',
    ]);

    return {
      Program() {
        const sourceCode = context.sourceCode ?? context.getSourceCode();
        for (const comment of sourceCode.getAllComments()) {
          const raw = comment.value.trim();
          // Match any of the three eslint-disable directive forms.
          const match = raw.match(/^eslint-disable(?:-next-line|-line)?(?:\s+([\s\S]*))?$/);
          if (!match) continue;
          const rulesPart = match[1] ?? '';
          // Strip a trailing inline reason (-- reason text) then split on commas.
          const rules = rulesPart
            .replace(/--.*$/, '')
            .split(',')
            .map((r) => r.trim())
            .filter(Boolean);
          for (const rule of rules) {
            if (HADES_RULES.has(rule)) {
              context.report({
                loc: comment.loc,
                messageId: 'noDisableComment',
                data: { rule },
              });
            }
          }
        }
      },
    };
  },
};

export default rule;
