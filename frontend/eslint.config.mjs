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
//      break the design-system contract. Use <Stack asChild> or <Grid asChild>
//      for layout needs, or add a documented variant inside Terra.
//
//   4. no-raw-html-text-tags — blocks raw <p> and <h1>–<h6> in JSX outside
//      the Terra package. All text must go through Terra typography
//      primitives (Heading, Text, Code) so colour and scale stay in the
//      token system.
//
//   5. no-raw-div — errors on raw <div> elements in JSX outside the Terra
//      package. Every layout need is met by Terra primitives. Suppress with
//      an inline disable comment only when a plain <div> is genuinely the
//      only option (extremely rare), and include a comment explaining why.
//
//   6. no-raw-layout-elements — errors when any of the following raw HTML
//      semantic elements carry a display-mode layout class (flex, inline-flex,
//      grid, inline-grid, block, inline-block), which promotes them into layout
//      containers that Terra's Stack and Grid components are designed to replace:
//
//        Checked: <section> <aside> <nav> <header> <footer> <article> <main>
//
//        <section className="flex …">   →  <Stack asChild><section>…</section></Stack>
//        <aside className="grid …">     →  <Grid asChild><aside>…</aside></Grid>
//
//   7. no-raw-inline-elements — errors on raw inline HTML elements in product
//      JSX. Terra source is exempt: it is the implementation layer and must
//      be able to render these elements to expose them as components. There
//      are no legitimate uses of raw inline elements in product code:
//
//        <span>          →  <Text asChild> from @orbit/terra (inline text wrapping)
//        <strong>, <b>   →  <Text weight="strong"> or weight="heavy" from @orbit/terra
//        <em>, <i>       →  add an italic variant to Terra Text first
//        <mark>, <abbr>, <s>, <u>, <sub>, <sup>
//                        →  no Terra equivalent yet — add one to src/packages/terra/src/components/
//
//   8. require-kebab-filename — all JS and TS source files under src/ must use
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
    //   1. Use <Stack asChild> or <Grid asChild> — layout classes go on the
    //      child element (e.g. <section>, <aside>), not on the Terra component.
    //   2. If a genuine variant gap exists, add a documented variant inside
    //      the Terra package.
    //
    // Never route around this with raw HTML wrappers — <div>, <span>,
    // <section> etc. used as layout containers are caught by no-raw-div and
    // no-raw-layout-elements.
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
            'For layout: use `<Stack asChild>` or `<Grid asChild>` and put layout classes on the child element, not on the Terra component. ' +
            'For a style variant: add a documented variant inside the Terra package. ' +
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
    // The rule targets typographic tags only. All structural and layout work
    // must go through Terra layout primitives (Stack, Grid, Section, PageLayout).
    // Raw <div> is caught by `no-raw-div`. Raw <span> with display-mode layout
    // classes is caught by `no-raw-layout-elements`. Semantic elements (<section>,
    // <aside>, <nav>, etc.) are composed via the `asChild` pattern on Stack or
    // Grid — never by applying flex/grid Tailwind classes directly to them.
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
            'Raw <{{tag}}> is not allowed. ' +
            'Use <Text> (for p) or <Heading variant="…"> (for h1–h6) from @orbit/terra. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
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
    // Errors when a raw <div> element is used in JSX outside the Terra package.
    // Terra provides first-class layout primitives that must be used instead:
    //
    //   <div className="flex …">  →  <Stack>  (direction, gap, align, justify, wrap)
    //   <div className="grid …">  →  <Grid>   (cols, gap, gapX, gapY)
    //
    // For semantic HTML elements, compose via asChild — layout classes land on
    // the element itself, not on a wrapping div:
    //
    //   <Stack asChild gap="snug"><section>…</section></Stack>
    //   <Stack asChild direction="horizontal"><nav>…</nav></Stack>
    //
    // In the rare case a plain <div> is genuinely unavoidable (e.g. a
    // third-party component that requires a div as its direct child), suppress
    // with eslint-disable-next-line and a one-line explanation of why.
    //
    // Terra's own source is excluded: it uses raw elements intentionally as
    // the canonical layout implementation.
    // -------------------------------------------------------------------------
    'no-raw-div': {
      meta: {
        type: 'problem',
        docs: {
          description:
            'Disallow raw <div> elements in JSX outside the Terra package. ' +
            'Use Terra layout components: <Stack> for flex layouts, <Grid> for grid layouts.',
        },
        messages: {
          noRawDiv:
            'Raw <div> is not allowed. Use <Stack> (flex), <Grid> (grid), or <Section> from @orbit/terra. ' +
            'For semantic HTML, use <Stack asChild><section>…</section></Stack>. ' +
            'If a <div> is genuinely unavoidable, suppress with eslint-disable-next-line and a comment explaining why. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
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
    // no-raw-layout-elements
    //
    // Errors when a raw semantic HTML element is given a display-mode Tailwind
    // class (flex, inline-flex, grid, inline-grid, block, inline-block) that
    // promotes it into a layout container — the role Terra's Stack and Grid fill.
    //
    // Checked elements: <section>, <aside>, <nav>, <header>, <footer>,
    //                   <article>, <main>
    //
    // <span> is intentionally NOT in this list. It is caught unconditionally
    // and more specifically by no-raw-inline-elements. This rule owns only
    // semantic container elements being misused as layout wrappers.
    //
    // Allowed alternatives:
    //
    //   <section className="flex …">  →  <Stack asChild><section>…</section></Stack>
    //   <aside className="grid …">    →  <Grid asChild><aside>…</aside></Grid>
    //
    // See frontend/docs/golden-paths/terra-design-system.md
    // Terra's own source is excluded as a canonical implementation site.
    // -------------------------------------------------------------------------
    'no-raw-layout-elements': {
      meta: {
        type: 'problem',
        docs: {
          description:
            'Disallow raw semantic HTML elements (section, aside, nav, header, footer, article, main) ' +
            'used as layout containers via display-mode Tailwind classes. ' +
            'Use <Stack asChild> or <Grid asChild> from @orbit/terra wrapping the semantic element instead. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
        },
        messages: {
          noRawLayoutElement:
            'Raw <{{tag}} className="{{cls}} …"> uses <{{tag}}> as a layout container. ' +
            'Use <Stack asChild><{{tag}}>…</{{tag}}></Stack> (flex) or ' +
            '<Grid asChild><{{tag}}>…</{{tag}}></Grid> (grid) from @orbit/terra. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
        },
        schema: [],
      },
      create(context) {
        const LAYOUT_CLASSES = new Set([
          'flex',
          'inline-flex',
          'grid',
          'inline-grid',
          'block',
          'inline-block',
        ]);
        // <span> is intentionally omitted — no-raw-inline-elements owns it unconditionally.
        const CHECKED_ELEMENTS = new Set([
          'section',
          'aside',
          'nav',
          'header',
          'footer',
          'article',
          'main',
        ]);
        return {
          JSXOpeningElement(node) {
            if (node.name.type !== 'JSXIdentifier') return;
            const tag = node.name.name;
            if (!CHECKED_ELEMENTS.has(tag)) return;
            const classNameAttr = node.attributes.find(
              (attr) =>
                attr.type === 'JSXAttribute' &&
                attr.name.type === 'JSXIdentifier' &&
                attr.name.name === 'className',
            );
            if (!classNameAttr) return;
            const value = classNameAttr.value;
            // Only inspect static string literals — template literals and
            // expressions are too dynamic to evaluate reliably at lint time.
            if (!value || value.type !== 'Literal' || typeof value.value !== 'string') return;
            const classes = value.value.split(/\s+/);
            const hit = classes.find((cls) => LAYOUT_CLASSES.has(cls));
            if (hit) {
              context.report({ node, messageId: 'noRawLayoutElement', data: { cls: hit, tag } });
            }
          },
        };
      },
    },
    // -------------------------------------------------------------------------
    // no-raw-inline-elements
    //
    // Errors on raw inline HTML elements in product JSX. Terra source is
    // exempt — it is the implementation layer and must be able to render
    // these elements to expose them as components to product code. Every
    // use in product code either has a Terra
    // equivalent already, or signals that a Terra component is missing and
    // should be added rather than routing around the design system.
    //
    //   <span>              →  <Text asChild> for inline text wrapping
    //   <strong> / <b>      →  <Text weight="heavy">
    //   <em> / <i>          →  Terra component missing — add one
    //   <mark> / <abbr>     →  Terra component missing — add one
    //   <s> / <u>           →  Terra component missing — add one
    //   <sub> / <sup>       →  Terra component missing — add one
    //
    // The rule fires unconditionally in product code — className presence is
    // irrelevant. There is no escape hatch in product code; if an inline
    // element seems necessary, restructure the layout or add a Terra component.
    // Terra source is exempt (see the config block below for rationale).
    // -------------------------------------------------------------------------
    'no-raw-inline-elements': {
      meta: {
        type: 'problem',
        docs: {
          description:
            'Disallow raw inline HTML elements in JSX outside the Terra package. ' +
            'Use Terra typography components or add a new Terra component if one is missing.',
        },
        messages: {
          span:
            'Raw <span> is not allowed. Use <Text> or <Text asChild> from @orbit/terra. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
          formatting:
            'Raw <{{tag}}> is not allowed. ' +
            'Use <Text weight="heavy"> from @orbit/terra for <strong> and <b>. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
          missing:
            'Raw <{{tag}}> has no Terra equivalent yet. ' +
            'Add a Terra atom in src/packages/terra/src/components/ first, then use it here. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
        },
        schema: [],
      },
      create(context) {
        const FORMATTING = new Set(['strong', 'b']);
        const MISSING = new Set(['em', 'i', 'mark', 'abbr', 's', 'u', 'sub', 'sup']);
        return {
          JSXOpeningElement(node) {
            if (node.name.type !== 'JSXIdentifier') return;
            const tag = node.name.name;
            if (tag === 'span') {
              context.report({ node, messageId: 'span' });
            } else if (FORMATTING.has(tag)) {
              context.report({ node, messageId: 'formatting', data: { tag } });
            } else if (MISSING.has(tag)) {
              context.report({ node, messageId: 'missing', data: { tag } });
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
  // If a layout need can't be met by the component API, use <Stack asChild> or
  // <Grid asChild> and put layout classes on the child element — not on the
  // Terra component. If a genuine variant gap exists, add a variant inside Terra.
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

  // ---- Raw <div> and layout-span enforcement (Terra source exempted) ---------
  // Outside the Terra package, raw div and semantic layout elements must be
  // replaced with Terra layout primitives:
  //
  //   no-raw-div: all <div> elements
  //     <div className="flex …">        →  <Stack>
  //     <div className="grid …">        →  <Grid>
  //     <Stack asChild><section>…       →  semantic element composed via asChild
  //
  //   no-raw-layout-elements: semantic elements used as layout containers
  //     <section className="flex …">    →  <Stack asChild><section>…</section></Stack>
  //     <aside className="grid …">      →  <Grid asChild><aside>…</aside></Grid>
  //
  // Terra's own source is excluded: it implements the canonical primitives
  // and uses raw elements at the foundation level.
  //
  // Suppression with eslint-disable-next-line is allowed for no-raw-div only
  // when a raw element is genuinely unavoidable (e.g. a third-party component
  // constraint); the disable comment must include a one-line explanation.
  {
    plugins: { local: localPlugin },
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
      'local/no-raw-div': 'error',
      'local/no-raw-layout-elements': 'error',
    },
  },

  // ---- Raw inline element enforcement (Terra source exempted) ----------------
  // Raw inline HTML elements are banned in product code. Terra is exempt:
  // it is the implementation layer and must be able to render raw elements
  // in order to expose them as components to the rest of the app. Banning
  // them inside Terra would make it impossible to ever add <mark>, <abbr>,
  // <em>, etc. as Terra components — defeating the whole purpose of the rule.
  //
  //   <span>              →  <Text> or <Text asChild>
  //   <strong> / <b>      →  <Text weight="heavy">
  //   <em> / <i>          →  no Terra equivalent yet — add one
  //   <mark> / <abbr>     →  no Terra equivalent yet — add one
  //   <s> / <u>           →  no Terra equivalent yet — add one
  //   <sub> / <sup>       →  no Terra equivalent yet — add one
  //
  // There is no escape hatch in product code. If an inline element seems
  // necessary, either restructure the layout or add a new Terra component.
  // Do not add eslint-disable comments for this rule in product code.
  {
    plugins: { local: localPlugin },
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
      'local/no-raw-inline-elements': 'error',
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
