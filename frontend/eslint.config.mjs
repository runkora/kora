import path from 'node:path';
import { defineConfig, globalIgnores } from 'eslint/config';
import nextVitals from 'eslint-config-next/core-web-vitals';
import nextTs from 'eslint-config-next/typescript';
import prettier from 'eslint-config-prettier';

// ---------------------------------------------------------------------------
// Local plugin: server/client boundary enforcement + Terra design-system rules
//
// Rules enforce the three-zone directory contract (ADR-0020 Decision 4) and
// the Terra design-system usage contract:
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
//
//   3. no-terra-classname — blocks `className` props on components imported
//      from @orbit/terra outside the Terra package. Terra components are
//      pre-styled through the token system; ad-hoc Tailwind classes on top
//      break the design-system contract. Use a wrapper element for layout
//      needs, or add a documented variant inside Terra.
//
//   4. no-raw-html-text-tags — blocks raw <p> and <h1>–<h6> in JSX outside
//      the Terra package. All text must go through Terra typography
//      primitives (Heading, Text, Code) so colour and scale stay in the
//      token system.
//
//   5. no-raw-div — warns on raw <div> elements in JSX outside the Terra
//      package. Prefer Terra layout components (<Stack> for flex layouts,
//      <Grid> for grid layouts). Suppress with eslint-disable-next-line when
//      a plain div is genuinely the right tool (e.g. a single-child sizing
//      wrapper).
//
//   6. require-kebab-filename — all JS and TS source files under src/ must use
//      kebab-case filenames (including the Terra package and any future
//      packages). PascalCase filenames (e.g. HealthStatus.tsx) cause
//      case-sensitivity mismatches between macOS (case-insensitive) and Linux
//      (case-sensitive) file systems, and create an inconsistent naming surface.
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

/**
 * Terra components that intentionally accept `className` for structural sizing.
 *
 * `Skeleton` is a shape-agnostic placeholder whose dimensions must be supplied
 * by the call site — it has no inherent size (it fills any role from a
 * word-length text skeleton to a full card skeleton).
 *
 * `Separator` is a structural divider whose cross-axis dimension (height when
 * orientation="vertical", width when orientation="horizontal") must be set by
 * the parent layout context, not baked into the component itself.
 *
 * Both components document this expectation in their JSDoc. All other Terra
 * components must express layout needs through variant props or wrapper elements.
 */
const CLASSNAME_EXEMPT = new Set(['Skeleton', 'Separator']);

/**
 * Convert a PascalCase or camelCase string to kebab-case, used to generate
 * rename suggestions in `require-kebab-filename` error messages.
 *
 * @param {string} str
 * @returns {string}
 */
function toKebab(str) {
  return str
    .replace(/([a-z0-9])([A-Z])/g, '$1-$2')
    .replace(/([A-Z]+)([A-Z][a-z])/g, '$1-$2')
    .toLowerCase();
}

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
            'Server-side modules must have `import "server-only"` as the first import ' +
            '(after any JSDoc block). Either the guard is absent or it is not the first ImportDeclaration.',
        },
      },
      create(context) {
        return {
          Program(node) {
            // Collect all import declarations in source order, then check that
            // "server-only" is the very first one (guardIdx === 0).
            // guardIdx === -1 → guard is absent entirely.
            // guardIdx  >  0 → guard exists but is not the first import.
            // Both cases violate the AGENTS.md position requirement.
            const imports = node.body.filter((n) => n.type === 'ImportDeclaration');
            const guardIdx = imports.findIndex((n) => n.source.value === 'server-only');
            if (guardIdx !== 0) {
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

    // -------------------------------------------------------------------------
    // no-terra-classname
    //
    // Blocks `className` props on any component imported from @orbit/terra
    // outside the Terra package itself. Terra components are pre-styled through
    // the token system; ad-hoc Tailwind classes applied on top break the
    // design-system contract and create invisible coupling between product code
    // and internal implementation details.
    //
    // If a layout or style need cannot be met by the existing component API,
    // the correct path is:
    //
    //   1. Use a wrapper element (div, section, etc.) with the layout classes.
    //   2. If a genuine variant gap exists, propose it to the Terra team and
    //      add a documented variant to the component inside the Terra package.
    //
    // The rule is import-aware: it tracks which local names in each file come
    // from @orbit/terra, so it never fires on similarly-named non-Terra
    // components.
    // -------------------------------------------------------------------------
    'no-terra-classname': {
      meta: {
        type: 'problem',
        docs: {
          description:
            'Disallow `className` props on Terra components (@orbit/terra) outside the Terra package. ' +
            'Use a wrapper element for layout, or (with approval) add a variant to the Terra component.',
        },
        messages: {
          noClassName:
            '`className` is not allowed on Terra component <{{component}}>. ' +
            'Wrap it in a layout element instead, or (with approval) add a variant to the Terra component.',
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
            // Skeleton and Separator are intentionally shape-agnostic — callers
            // must size them via className. All other Terra components must use
            // variant props or wrapper elements instead.
            if (CLASSNAME_EXEMPT.has(componentName)) return;

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
    },

    // -------------------------------------------------------------------------
    // no-raw-html-text-tags
    //
    // Blocks direct use of <p> and <h1>–<h6> in JSX outside the Terra package.
    // These elements must be expressed through Terra typography primitives so
    // that colour, size, and weight always come from the token system:
    //
    //   <p>   →  <Text>                          (@orbit/terra)
    //   <h1>  →  <Heading variant="title">       (@orbit/terra)
    //   <h2>  →  <Heading variant="section">     (@orbit/terra)
    //   <h3>  →  <Heading variant="subsection">  (@orbit/terra)
    //   <h4>–<h6>  →  <Heading variant="label">  (@orbit/terra)
    //
    // The rule is intentionally narrow — it targets typographic tags only.
    // Structural/layout elements (<span>, <section>, <article>, <header>,
    // <footer>) are unrestricted. <div> is handled separately by the
    // `no-raw-div` rule which steers callers toward Terra layout components.
    // -------------------------------------------------------------------------
    'no-raw-html-text-tags': {
      meta: {
        type: 'problem',
        docs: {
          description:
            'Disallow raw HTML text-flow tags in JSX outside the Terra package. ' +
            'Use Terra typography components (Heading, Text, Code) from @orbit/terra instead.',
        },
        messages: {
          noRawTag:
            'Raw <{{tag}}> is not allowed outside the Terra package. ' +
            'Replace with a Terra typography component: ' +
            'Heading (for h1–h6) or Text (for p) from @orbit/terra.',
        },
      },
      create(context) {
        const BLOCKED = new Set(['p', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6']);
        return {
          JSXOpeningElement(node) {
            const name = node.name.type === 'JSXIdentifier' ? node.name.name : null;
            if (name && BLOCKED.has(name)) {
              context.report({ node, messageId: 'noRawTag', data: { tag: name } });
            }
          },
        };
      },
    },

    // -------------------------------------------------------------------------
    // no-raw-div
    //
    // Warns when a raw <div> element is used in JSX outside the Terra package.
    // Terra provides first-class layout primitives that should be preferred:
    //
    //   <div className="flex …">   →  <Stack>  (direction, gap, align, justify, wrap)
    //   <div className="grid …">   →  <Grid>   (cols, gap, gapX, gapY)
    //
    // This is a *warning* rather than an error because there are legitimate
    // cases where a plain <div> is the right tool — e.g. a single-child
    // sizing wrapper that provides zero layout value, or a semantic landmark
    // element. In those cases, suppress with an eslint-disable-next-line
    // comment and a short explanation so the intent is clear.
    //
    // Terra's own source is excluded: it uses raw elements intentionally as
    // the canonical layout implementation.
    // -------------------------------------------------------------------------
    'no-raw-div': {
      meta: {
        type: 'suggestion',
        docs: {
          description:
            'Warn on raw <div> elements in JSX outside the Terra package. ' +
            'Prefer Terra layout components: <Stack> for flex layouts or <Grid> for grid layouts.',
        },
        messages: {
          noRawDiv:
            'Avoid raw <div> elements. Prefer Terra layout components: ' +
            '<Stack> (flex) or <Grid> (grid) from @orbit/terra. ' +
            'If a <div> is genuinely necessary (e.g. a single-child sizing wrapper), ' +
            'suppress this warning with an eslint-disable-next-line comment.',
        },
        schema: [],
      },
      create(context) {
        return {
          JSXOpeningElement(node) {
            if (node.name.type === 'JSXIdentifier' && node.name.name === 'div') {
              context.report({ node, messageId: 'noRawDiv' });
            }
          },
        };
      },
    },

    // -------------------------------------------------------------------------
    // require-kebab-filename
    //
    // All JS and TS source files under src/ must use kebab-case filenames —
    // this includes the Terra package and any future packages added under
    // src/packages/. Consistent naming prevents case-sensitivity mismatches
    // between macOS (case-insensitive HFS+) and Linux CI (case-sensitive
    // ext4), and establishes a single convention across all source zones.
    //
    // Skipped automatically:
    //   - Dot-files (.prettierrc, etc.)
    //   - Next.js dynamic route segments ([id].tsx, [...slug].tsx)
    // -------------------------------------------------------------------------
    'require-kebab-filename': {
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
    },
  },
};

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  prettier, // disables ESLint formatting rules that conflict with Prettier

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

  // ---- Terra className enforcement -----------------------------------------
  // Terra components are pre-styled through the token system. Applying ad-hoc
  // Tailwind classes via `className` on top of them breaks the design-system
  // contract and creates invisible coupling to internal implementation details.
  //
  // If a layout need can't be met by the component API, use a wrapper element.
  // If a genuine variant gap exists, add a documented variant inside Terra.
  //
  // The rule tracks imports per-file so it only fires on actual Terra
  // components — never on similarly-named locals or third-party components.
  //
  // Terra's own source is excluded: `className` forwarding is fundamental to
  // how Terra components compose internally (cn(defaultClasses, className)).
  {
    plugins: { local: localPlugin },
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
      'local/no-terra-classname': 'error',
    },
  },

  // ---- Terra typography enforcement ----------------------------------------
  // Outside the Terra design-system package itself, raw HTML text-flow tags
  // (<p>, <h1>–<h6>) are forbidden in JSX. All text must be expressed through
  // Terra typography primitives so colour and scale stay in the token system.
  //
  // Terra's own source is excluded: it IS the canonical implementation site.
  // Story files (*.stories.tsx) inside Terra are also excluded automatically
  // because they live under src/packages/terra/.
  {
    plugins: { local: localPlugin },
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
      'local/no-raw-html-text-tags': 'error',
    },
  },

  // ---- Raw <div> usage warning ---------------------------------------------
  // Outside the Terra package, raw <div> elements should be replaced with
  // Terra layout primitives (<Stack> for flex, <Grid> for grid). This is a
  // *warning* rather than an error because there are valid edge-cases (e.g.
  // a single-child sizing wrapper with no layout value). Suppress the warning
  // with eslint-disable-next-line and a brief comment when needed.
  //
  // Terra's own source is excluded: it uses raw elements as the canonical
  // layout implementation and intentionally does not go through itself.
  {
    plugins: { local: localPlugin },
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
      'local/no-raw-div': 'warn',
    },
  },

  // ---- Kebab-case filename enforcement ------------------------------------
  // ALL JavaScript and TypeScript source files under src/ must use kebab-case
  // filenames — this includes the Terra package and any future packages added
  // under src/packages/. Consistent naming prevents case-sensitivity mismatches
  // between macOS (case-insensitive) and Linux CI (case-sensitive) and gives
  // every source zone a single predictable convention.
  {
    plugins: { local: localPlugin },
    files: ['src/**/*.{js,jsx,ts,tsx}'],
    rules: {
      'local/require-kebab-filename': 'error',
    },
  },
]);

export default eslintConfig;
