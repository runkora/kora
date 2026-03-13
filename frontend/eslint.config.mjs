import { defineConfig, globalIgnores } from 'eslint/config';
import nextVitals from 'eslint-config-next/core-web-vitals';
import nextTs from 'eslint-config-next/typescript';

// ---------------------------------------------------------------------------
// Local plugin: server/client boundary enforcement
//
// Two rules enforce the three-zone directory contract (ADR-0017 Decision 4):
//
//   1. require-server-only  — every file in src/server/** and every Route
//      Handler in src/app/api/**/route.ts must contain `import "server-only"`.
//      Forgetting this guard is a silent build failure: the module becomes
//      importable from client components with no compile-time error.
//
//   2. require-use-client-for-hooks — every file in src/app/** (excluding
//      Route Handlers) that imports React client hooks must carry a
//      "use client" directive. Prevents accidental Server Component/Client
//      Component boundary violations from being caught only at runtime.
// ---------------------------------------------------------------------------

/** React hooks that are only valid in Client Components. */
const CLIENT_HOOKS = new Set([
  'useState',
  'useEffect',
  'useContext',
  'useCallback',
  'useMemo',
  'useRef',
  'useReducer',
  'useLayoutEffect',
  'useImperativeHandle',
  'useDebugValue',
  'useDeferredValue',
  'useTransition',
  'useId',
  'useSyncExternalStore',
]);

const localPlugin = {
  rules: {
    // -------------------------------------------------------------------------
    // require-server-only
    // -------------------------------------------------------------------------
    'require-server-only': {
      meta: {
        type: 'problem',
        docs: {
          description:
            'Require `import "server-only"` in server-side modules to prevent accidental client bundle inclusion.',
        },
        messages: {
          missing:
            'Server-side modules must contain `import "server-only"`. ' +
            'Add it as the first import after any JSDoc block.',
        },
      },
      create(context) {
        return {
          Program(node) {
            const hasGuard = node.body.some(
              (n) => n.type === 'ImportDeclaration' && n.source.value === 'server-only',
            );
            if (!hasGuard) {
              context.report({ node, messageId: 'missing' });
            }
          },
        };
      },
    },

    // -------------------------------------------------------------------------
    // require-use-client-for-hooks
    // -------------------------------------------------------------------------
    'require-use-client-for-hooks': {
      meta: {
        type: 'problem',
        docs: {
          description:
            'Require a `"use client"` directive in files that import React client hooks.',
        },
        messages: {
          missing:
            'This file imports React client hook(s) ({{ hooks }}) but is missing a `"use client"` ' +
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

            // Find any `import { … } from "react"` declarations.
            const reactImport = node.body.find(
              (n) => n.type === 'ImportDeclaration' && n.source.value === 'react',
            );
            if (!reactImport) return;

            // Collect the client-only hooks being imported.
            const usedClientHooks = reactImport.specifiers
              .filter((s) => s.type === 'ImportSpecifier' && CLIENT_HOOKS.has(s.imported.name))
              .map((s) => s.imported.name);

            if (usedClientHooks.length > 0) {
              context.report({
                node,
                messageId: 'missing',
                data: { hooks: usedClientHooks.join(', ') },
              });
            }
          },
        };
      },
    },
  },
};

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,

  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    '.next/**',
    'out/**',
    'build/**',
    'next-env.d.ts',
  ]),

  // ---- server-only guard ---------------------------------------------------
  // Every file in src/server/ and every Route Handler must import "server-only".
  {
    plugins: { local: localPlugin },
    files: ['src/server/**/*.{ts,tsx}', 'src/app/api/**/route.ts'],
    rules: {
      'local/require-server-only': 'error',
    },
  },

  // ---- use client boundary -------------------------------------------------
  // App Router UI files that use React client hooks must declare "use client".
  {
    plugins: { local: localPlugin },
    files: ['src/app/**/*.{ts,tsx}'],
    ignores: ['src/app/api/**'],
    rules: {
      'local/require-use-client-for-hooks': 'error',
    },
  },
]);

export default eslintConfig;
