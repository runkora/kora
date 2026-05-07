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
//      Route Handlers) that imports React client hooks (from 'react') or
//      Next.js navigation hooks (from 'next/navigation') must carry a
//      "use client" directive. Prevents accidental Server Component/Client
//      Component boundary violations from being caught only at runtime.
//
//   3. no-terra-classname — blocks `className` props on components imported
//      from @orbit/terra outside the Terra package. Terra components are
//      pre-styled through the token system; ad-hoc Tailwind classes on top
//      break the design-system contract. Use <Stack asChild> or <Grid asChild>
//      for layout needs, or propose a new variant to a human engineer for approval.
//
//   4. no-raw-html-text-tags — blocks raw <p> and <h1>–<h6> in JSX outside
//      the Terra package. All text must go through Terra typography
//      primitives (Heading, Text, Code) so colour and scale stay in the
//      token system.
//
//   5. no-raw-layout-elements — errors on raw <div> elements (unconditionally),
//      <section> elements (unconditionally — use <Section> from @orbit/terra),
//      and on semantic HTML elements given a display-mode Tailwind class
//      (flex, inline-flex, grid, inline-grid, block, inline-block) that
//      promotes them into layout containers that Stack and Grid are designed
//      to replace. All checks live in the single rule.
//
//        Checked (unconditional): <div>, <section>
//        Checked (layout-class):  <aside> <nav> <header> <footer> <article> <main>
//
//        <div className="flex …">        →  <Stack>
//        <div className="grid …">        →  <Grid>
//        <section>                       →  <Section> from @orbit/terra
//        <aside className="grid …">      →  <Grid asChild><aside>…</aside></Grid>
//
//   6. no-raw-inline-elements — errors on raw inline HTML elements in product
//      JSX. Terra source is exempt: it is the implementation layer and must
//      be able to render these elements to expose them as components. There
//      are no legitimate uses of raw inline elements in product code:
//
//        <span>          →  <Text asChild> from @orbit/terra (inline text wrapping)
//        <strong>, <b>   →  <Text weight="strong"> or weight="heavy" from @orbit/terra
//        <code>          →  <Code> from @orbit/terra
//        <kbd>, <samp>, <var>  →  <Code> from @orbit/terra
//        <em>, <i>       →  no Terra equivalent yet — propose one to a human engineer for approval
//        <mark>, <abbr>, <s>, <u>, <sub>, <sup>
//                        →  no Terra equivalent yet — propose one to a human engineer for approval
//
//   7. no-direct-lucide-import — bans direct 'lucide-react' imports in product
//      code. All icon usage must go through Terra's <Icon> component, which
//      wraps Lucide internally and provides a strongly-typed curated registry.
//      Terra source is exempt — registry.ts must import from lucide-react.
//
//        import { X } from 'lucide-react'  →  <Icon name="close" />
//        import { Plus } from 'lucide-react'  →  <Icon name="plus" />
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
 * Next.js navigation hooks that are only valid in Client Components.
 * These are not caught by the react-import check above because they are
 * imported from 'next/navigation', not from 'react'.
 */
const CLIENT_NEXT_NAV_HOOKS = new Set([
  'useRouter',
  'usePathname',
  'useSearchParams',
  'useParams',
  'useSelectedLayoutSegment',
  'useSelectedLayoutSegments',
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
    //   2. If a genuine variant gap exists, propose it to a human engineer
    //      for approval before making any changes to the Terra package.
    //
    // Never route around this with raw HTML wrappers — <div>, <span>,
    // <section> etc. used as layout containers are caught by
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
            'Use a wrapper element for layout, or propose a new variant to a human engineer for approval.',
        },
        messages: {
          noClassName:
            '`className` is not allowed on Terra component <{{component}}>. ' +
            'For layout: use `<Stack asChild>` or `<Grid asChild>` and put layout classes on the child element, not on the Terra component. ' +
            'For a style variant: propose a new variant to a human engineer for approval — do not modify Terra unilaterally. ' +
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
    // Raw <div> is caught by `no-raw-layout-elements`. Raw <span> with
    // display-mode layout classes is also caught by `no-raw-layout-elements`.
    // Semantic elements (<section>,
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
    // no-raw-layout-elements
    //
    // Two checks in one rule:
    //
    //   1. <div> — fires unconditionally. Raw <div> is always a layout primitive
    //      that must be replaced by Stack or Grid.
    //
    //   2. <section> — fires unconditionally. Terra provides a Section component
    //      that renders a semantic <section> with proper heading/description
    //      wiring. All raw <section> elements must use the Terra wrapper.
    //
    //   3. Semantic container elements (nav, aside, header, footer, article,
    //      main) — fire only when a display-mode Tailwind class (flex,
    //      inline-flex, grid, inline-grid, block, inline-block) promotes them
    //      into layout containers that Terra's Stack/Grid fill.
    //
    // Checked unconditionally: <div>, <section>
    // Checked with layout class: <aside>, <nav>, <header>, <footer>,
    //                            <article>, <main>
    //
    // <span> is intentionally NOT in this list. It is caught unconditionally
    // and more specifically by no-raw-inline-elements. This rule owns only
    // structural container elements.
    //
    // Allowed alternatives:
    //
    //   <div className="flex …">      →  <Stack>
    //   <div className="grid …">      →  <Grid>
    //   <section>                     →  <Section> from @orbit/terra
    //   <aside className="flex …">    →  <Stack asChild><aside>…</aside></Stack>
    //
    // See frontend/docs/golden-paths/terra-design-system.md
    // Terra's own source is excluded as a canonical implementation site.
    // -------------------------------------------------------------------------
    'no-raw-layout-elements': {
      meta: {
        type: 'problem',
        docs: {
          description:
            'Disallow raw <div>, <section>, <header>, <article>, <nav>, <aside>, and <footer> ' +
            'elements in product JSX. Use <Stack>, <Grid>, <Section>, <Callout>, <Navigation>, ' +
            '<SidePanel>, <TableToolbarShell>, or PageLayout slots from @orbit/terra instead. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
        },
        messages: {
          noRawDiv:
            'Raw <div> is not allowed. Use <Stack> (flex), <Grid> (grid), or <Section> from @orbit/terra. ' +
            'Do not suppress this error with a disable comment. If no Terra component covers this need, write a proposal for human engineer approval before modifying Terra. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
          noRawSection:
            'Raw <section> is not allowed. Use <Section> from @orbit/terra. ' +
            'For layout-only grouping, use <Stack>. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
          noRawHeader:
            'Raw <header> is not allowed. Use <TableToolbarShell> for toolbar containers, ' +
            '<SidePanelHeader> for panel headers, or <Stack> for generic grouping (from @orbit/terra). ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
          noRawArticle:
            'Raw <article> is not allowed. Use <Card> for content cards, <Stack> for grouping, ' +
            'or <SidePanelBody> for panel body sections (from @orbit/terra). ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
          noRawNav:
            'Raw <nav> is not allowed. Use <Navigation> for sidebar nav landmarks (from @orbit/terra). ' +
            'For breadcrumb trails, use <Stack aria-label="Breadcrumb">. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
          noRawAside:
            'Raw <aside> is not allowed. Use <Callout> for informational callout boxes, ' +
            '<SidePanel> for slide-over panels, or <Stack> for generic grouping (from @orbit/terra). ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
          noRawFooter:
            'Raw <footer> is not allowed. Use the PageLayout sidebarFooter prop for sidebar footers, ' +
            '<SidePanelFooter> for panel footers, or <Stack> for generic grouping (from @orbit/terra). ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
          noRawLayoutElement:
            'Raw <{{tag}} className="{{cls}} …"> uses <{{tag}}> as a layout container. ' +
            'Use the appropriate Terra component instead. ' +
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
        // Only <main> remains in the conditional set — it has no Terra equivalent yet.
        const SEMANTIC_ELEMENTS = new Set(['main']);
        return {
          JSXOpeningElement(node) {
            if (node.name.type !== 'JSXIdentifier') return;
            const tag = node.name.name;
            // <div> fires unconditionally — no className check needed.
            if (tag === 'div') {
              context.report({ node, messageId: 'noRawDiv' });
              return;
            }
            // <section> fires unconditionally — Terra provides a Section wrapper.
            if (tag === 'section') {
              context.report({ node, messageId: 'noRawSection' });
              return;
            }
            // The following five fire unconditionally — Terra provides compliant
            // alternatives for all product-code use cases.
            if (tag === 'header') {
              context.report({ node, messageId: 'noRawHeader' });
              return;
            }
            if (tag === 'article') {
              context.report({ node, messageId: 'noRawArticle' });
              return;
            }
            if (tag === 'nav') {
              context.report({ node, messageId: 'noRawNav' });
              return;
            }
            if (tag === 'aside') {
              context.report({ node, messageId: 'noRawAside' });
              return;
            }
            if (tag === 'footer') {
              context.report({ node, messageId: 'noRawFooter' });
              return;
            }
            if (!SEMANTIC_ELEMENTS.has(tag)) return;
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
    // no-raw-interactive-elements
    //
    // Blocks raw <button> elements in product JSX. All interactive elements
    // must use Terra's <Button> component to ensure consistent focus styles,
    // hover states, and accessibility attributes across the application.
    //
    //   <button>  →  <Button> from @orbit/terra
    //
    // The rule fires unconditionally. If a specific Button variant does not
    // cover a layout need, propose a new variant to a human engineer for
    // approval — do not use raw HTML.
    // Terra source is exempt (it IS the Button implementation).
    // -------------------------------------------------------------------------
    'no-raw-interactive-elements': {
      meta: {
        type: 'problem',
        docs: {
          description:
            'Disallow raw <button> elements in JSX outside the Terra package. ' +
            'Use <Button> from @orbit/terra for consistent focus styles, hover states, and accessibility.',
        },
        messages: {
          noRawButton:
            'Raw <button> is not allowed. ' +
            'Use <Button> from @orbit/terra. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
        },
        schema: [],
      },
      create(context) {
        return {
          JSXOpeningElement(node) {
            if (node.name.type !== 'JSXIdentifier') return;
            if (node.name.name === 'button') {
              context.report({ node, messageId: 'noRawButton' });
            }
          },
        };
      },
    },

    // -------------------------------------------------------------------------
    // no-raw-list-elements
    //
    // Bans raw <ul>, <ol>, and <li> in product JSX. Semantic list elements
    // were used as layout workarounds after <div>/<section> were forbidden.
    // Terra now provides <List> and <ListItem> as replacements.
    //
    //   <ul>  →  <List> from @orbit/terra (or <Stack> for pure layout containers)
    //   <ol>  →  <List ordered> from @orbit/terra
    //   <li>  →  <ListItem> from @orbit/terra
    //
    // All three fire unconditionally. Terra source is exempt.
    // -------------------------------------------------------------------------
    'no-raw-list-elements': {
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
    },

    // -------------------------------------------------------------------------
    // no-raw-form-elements
    //
    // Bans raw <form>, <label>, <input>, and <output> in product JSX. These
    // were observed as workarounds — particularly <label>/<input type="checkbox">
    // to avoid no-raw-inline-elements after <span> was forbidden.
    //
    //   <form>                   →  <Form> from @orbit/terra
    //   <label>                  →  <Label> from @orbit/terra, or <Checkbox> for checkboxes
    //   <input type="checkbox">  →  <Checkbox> from @orbit/terra
    //   <input type="text/...">  →  <Input> from @orbit/terra
    //   <output>                 →  <Output> from @orbit/terra
    //
    // All fire unconditionally. Terra source is exempt.
    // -------------------------------------------------------------------------
    'no-raw-form-elements': {
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
    },

    // -------------------------------------------------------------------------
    // no-raw-inline-elements
    //
    // Errors on raw inline HTML elements in product JSX. Terra source is
    // exempt — it is the implementation layer and must be able to render
    // these elements to expose them as components to product code. Every
    // use in product code either has a Terra
    // equivalent already, or signals that a Terra component is missing and
    // must be proposed to a human engineer for approval before being added.
    //
    //   <span>              →  <Text asChild> for inline text wrapping
    //   <strong> / <b>      →  <Text weight="heavy">
    //   <em> / <i>          →  Terra component missing — propose to human engineer for approval
    //   <mark> / <abbr>     →  Terra component missing — propose to human engineer for approval
    //   <s> / <u>           →  Terra component missing — propose to human engineer for approval
    //   <sub> / <sup>       →  Terra component missing — propose to human engineer for approval
    //   <code>              →  <Code> from @orbit/terra
    //   <kbd> / <samp> / <var> → <Code> from @orbit/terra (keyboard input, sample output, variables)
    //
    // The rule fires unconditionally in product code — className presence is
    // irrelevant. There is no escape hatch in product code; if an inline
    // element seems necessary, restructure the layout or propose a Terra component
    // to a human engineer for approval.
    // Terra source is exempt (see the config block below for rationale).
    // -------------------------------------------------------------------------
    'no-raw-inline-elements': {
      meta: {
        type: 'problem',
        docs: {
          description:
            'Disallow raw inline HTML elements in JSX outside the Terra package. ' +
            'Use Terra typography components. If one is missing, propose it to a human engineer for approval before modifying Terra.',
        },
        messages: {
          span:
            'Raw <span> is not allowed. Use <Text> or <Text asChild> from @orbit/terra. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
          formatting:
            'Raw <{{tag}}> is not allowed. ' +
            'Use <Text weight="heavy"> from @orbit/terra for <strong> and <b>. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
          code:
            'Raw <{{tag}}> is not allowed. ' +
            'Use <Code> from @orbit/terra for inline code, keyboard input, sample output, and variables. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
          missing:
            'Raw <{{tag}}> has no Terra equivalent yet. ' +
            'Propose a new Terra component to a human engineer for approval — do not modify Terra unilaterally. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
        },
        schema: [],
      },
      create(context) {
        const FORMATTING = new Set(['strong', 'b']);
        const CODE_ELEMENTS = new Set(['code', 'kbd', 'samp', 'var']);
        const MISSING = new Set(['em', 'i', 'mark', 'abbr', 's', 'u', 'sub', 'sup']);
        return {
          JSXOpeningElement(node) {
            if (node.name.type !== 'JSXIdentifier') return;
            const tag = node.name.name;
            if (tag === 'span') {
              context.report({ node, messageId: 'span' });
            } else if (FORMATTING.has(tag)) {
              context.report({ node, messageId: 'formatting', data: { tag } });
            } else if (CODE_ELEMENTS.has(tag)) {
              context.report({ node, messageId: 'code', data: { tag } });
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

    // -------------------------------------------------------------------------
    // no-terra-disable-comment
    //
    // Bans eslint-disable directive comments (eslint-disable, eslint-disable-next-line,
    // eslint-disable-line) that target any of the Terra design-system rules in
    // product code. The design system must be adopted by using the correct Terra
    // component — not by routing around lint with disable comments.
    //
    // If a genuine gap exists in the Terra component library, write a short
    // proposal and wait for approval from a human engineer before modifying Terra.
    //
    // This rule applies to both JSX block comments ({/* … */}) and JS line
    // comments (// …) because both forms appear in the AST comment list.
    // -------------------------------------------------------------------------
    'no-terra-disable-comment': {
      meta: {
        type: 'problem',
        docs: {
          description:
            'Disallow eslint-disable directive comments that suppress Terra design-system rules. ' +
            'Fix the violation by using the correct Terra component instead of suppressing the lint error.',
        },
        messages: {
          noDisableComment:
            'Suppressing Terra rule "{{rule}}" with a disable directive is forbidden. ' +
            'Fix the violation by using the correct Terra component. ' +
            'If no Terra component covers your use case, propose one to a human engineer for approval — do not modify Terra unilaterally. ' +
            'See frontend/docs/golden-paths/terra-design-system.md',
        },
        schema: [],
      },
      create(context) {
        const TERRA_RULES = new Set([
          'local/no-raw-layout-elements',
          'local/no-raw-inline-elements',
          'local/no-raw-html-text-tags',
          'local/no-terra-classname',
          'local/no-direct-lucide-import',
          'local/no-raw-interactive-elements',
          'local/no-raw-list-elements',
          'local/no-raw-form-elements',
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
                if (TERRA_RULES.has(rule)) {
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
    },

    // -------------------------------------------------------------------------
    // no-direct-lucide-import
    //
    // Lucide React ships ~1,600 icons. Allowing direct imports would make it
    // impossible to maintain a curated, strongly-typed icon vocabulary, enforce
    // consistent sizing tokens, or provide a container inbuilt. All icons must
    // go through the Terra Icon component, which wraps Lucide internally.
    //
    // To add a new icon:
    //   1. Verify it exists in lucide-react (check node_modules/lucide-react).
    //   2. Add a kebab-case entry to ICON_REGISTRY in
    //      src/packages/terra/src/components/icon/registry.ts.
    //   3. Add the name to the IconName union type in the same file.
    //   4. Use <Icon name="your-icon" /> in product code.
    //
    // Terra source (src/packages/terra/**) is exempt — the registry itself
    // must import from lucide-react.
    // -------------------------------------------------------------------------
    'no-direct-lucide-import': {
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
    ignores: ['**/*.test.{ts,tsx}', '**/*.integration.test.{ts,tsx}'],
    rules: {
      'local/require-server-only': 'error',
    },
  },

  // ---- use client boundary -------------------------------------------------
  // App Router UI files that use React client hooks must declare "use client".
  {
    plugins: { local: localPlugin },
    files: ['src/app/**/*.{ts,tsx}'],
    ignores: ['src/app/api/**', '**/*.test.{ts,tsx}', '**/*.integration.test.{ts,tsx}'],
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
  // Terra component. If a genuine variant gap exists, propose it to a human engineer for approval.
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

  // ---- Raw <div> and semantic layout element enforcement (Terra source exempted)
  // Outside the Terra package, raw <div> and semantic elements misused as layout
  // containers must use Terra layout primitives. Both checks live in the single
  // no-raw-layout-elements rule:
  //
  //   <div> (unconditional):
  //     <div className="flex …">        →  <Stack>
  //     <div className="grid …">        →  <Grid>
  //     <Stack asChild><section>…       →  semantic element composed via asChild
  //
  //   Semantic elements (layout-class triggered):
  //     <section className="flex …">    →  <Stack asChild><section>…</section></Stack>
  //     <aside className="grid …">      →  <Grid asChild><aside>…</aside></Grid>
  //
  // Terra's own source is excluded: it implements the canonical primitives
  // and uses raw elements at the foundation level.
  //
  // There is no escape hatch. Do not suppress violations with disable comments —
  // fix them by using the correct Terra primitive. If a genuine gap exists,
  // propose a new Terra component to a human engineer for approval.
  {
    plugins: { local: localPlugin },
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
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
  //   <em> / <i>          →  no Terra equivalent yet — propose to human engineer for approval
  //   <mark> / <abbr>     →  no Terra equivalent yet — propose to human engineer for approval
  //   <s> / <u>           →  no Terra equivalent yet — propose to human engineer for approval
  //   <sub> / <sup>       →  no Terra equivalent yet — propose to human engineer for approval
  //
  // There is no escape hatch in product code. If an inline element seems
  // necessary, either restructure the layout or propose a new Terra component
  // to a human engineer for approval.
  // Do not add eslint-disable comments for this rule in product code.
  {
    plugins: { local: localPlugin },
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
      'local/no-raw-inline-elements': 'error',
    },
  },

  // ---- Direct Lucide import ban (Terra source exempted) ---------------------
  // All icon usage must go through Terra's <Icon> component. Direct imports
  // from 'lucide-react' are banned in product code to ensure the curated
  // IconName registry is the single source of truth for available icons.
  // To add a new icon, propose the addition to a human engineer for approval,
  // then extend the registry in src/packages/terra/src/components/icon/registry.ts.
  // Terra source is exempt — registry.ts must import from lucide-react.
  {
    plugins: { local: localPlugin },
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
      'local/no-direct-lucide-import': 'error',
    },
  },

  // ---- Raw interactive element enforcement (Terra source exempted) -----------
  // Raw <button> elements must use Terra's <Button> component for consistent
  // focus styles, hover states, and accessibility attributes.
  {
    plugins: { local: localPlugin },
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
      'local/no-raw-interactive-elements': 'error',
    },
  },

  // ---- Raw list element enforcement (Terra source exempted) -----------------
  {
    plugins: { local: localPlugin },
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
      'local/no-raw-list-elements': 'error',
    },
  },

  // ---- Raw form element enforcement (Terra source exempted) -----------------
  {
    plugins: { local: localPlugin },
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
      'local/no-raw-form-elements': 'error',
    },
  },

  // ---- Terra disable-comment ban (product code only) ----------------------
  // Disable directive comments targeting any of the five Terra rules are
  // strictly forbidden in product code. Terra violations must be fixed by
  // using the correct Terra component (or proposing a new one for human
  // approval) — never by suppressing the lint error. Terra source is excluded
  // because it uses legitimate disable comments for unrelated rules (e.g.
  // react-hooks/rules-of-hooks in Storybook story render functions).
  {
    plugins: { local: localPlugin },
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
      'local/no-terra-disable-comment': 'error',
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
