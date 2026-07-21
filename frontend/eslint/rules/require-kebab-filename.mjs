import path from 'node:path';
import { toKebab } from '../constants.mjs';

const rule = {
  meta: {
    type: 'problem',
    docs: {
      description: 'Require kebab-case filenames for all JS and TS source files under src/.',
    },
    messages: {
      notKebab: 'Filename "{{name}}" must be kebab-case. Rename it to "{{suggestion}}".',
    },
  },
  create(context) {
    return {
      Program(node) {
        const filename = context.filename ?? context.getFilename();
        const base = path.basename(filename);

        // Skip dot-files and Next.js dynamic route segments.
        if (base.startsWith('.') || base.startsWith('[')) return;

        // Isolate the stem (everything before the first dot), e.g.
        //   "health-status.tsx" → "health-status"
        //   "HealthStatus.tsx"  → "HealthStatus"
        const dotIdx = base.indexOf('.');
        const stem = dotIdx === -1 ? base : base.slice(0, dotIdx);
        const ext = dotIdx === -1 ? '' : base.slice(dotIdx);

        // Valid: lowercase letters and digits separated by single hyphens.
        if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(stem)) {
          context.report({
            node,
            messageId: 'notKebab',
            data: { name: base, suggestion: toKebab(stem) + ext },
          });
        }
      },
    };
  },
};

export default rule;
