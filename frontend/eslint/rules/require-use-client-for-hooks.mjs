import { CLIENT_HOOKS, CLIENT_NEXT_NAV_HOOKS } from '../constants.mjs';

const rule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Require a `"use client"` directive in files that import React or Next.js client hooks.',
    },
    messages: {
      missing:
        'This file imports client-only hook(s) ({{ hooks }}) but is missing a `"use client"` ' +
        'directive. Add `"use client"` as the first line of the file.',
    },
  },
  create(context) {
    return {
      Program(node) {
        // If "use client" is already declared, nothing to enforce.
        const hasUseClient = node.body.some(
          (n) => n.type === 'ExpressionStatement' && n.directive === 'use client',
        );
        if (hasUseClient) return;

        const allClientHooks = [];

        // Check `import { … } from 'react'` for React client hooks.
        const reactImport = node.body.find(
          (n) => n.type === 'ImportDeclaration' && n.source.value === 'react',
        );
        if (reactImport) {
          const hooks = reactImport.specifiers
            .filter((s) => s.type === 'ImportSpecifier' && CLIENT_HOOKS.has(s.imported.name))
            .map((s) => s.imported.name);
          allClientHooks.push(...hooks);
        }

        // Check `import { … } from 'next/navigation'` for Next.js client hooks.
        const nextNavImport = node.body.find(
          (n) => n.type === 'ImportDeclaration' && n.source.value === 'next/navigation',
        );
        if (nextNavImport) {
          const hooks = nextNavImport.specifiers
            .filter(
              (s) => s.type === 'ImportSpecifier' && CLIENT_NEXT_NAV_HOOKS.has(s.imported.name),
            )
            .map((s) => s.imported.name);
          allClientHooks.push(...hooks);
        }

        if (allClientHooks.length > 0) {
          context.report({
            node,
            messageId: 'missing',
            data: { hooks: allClientHooks.join(', ') },
          });
        }
      },
    };
  },
};

export default rule;
