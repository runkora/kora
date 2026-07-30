import { defineConfig, globalIgnores } from 'eslint/config';
import nextVitals from 'eslint-config-next/core-web-vitals';
import nextTs from 'eslint-config-next/typescript';
import prettier from 'eslint-config-prettier';
import localPlugin from './eslint/index.mjs';

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

  // Register the local plugin globally so individual config objects below
  // only need to list rule names — no repeated plugins: { local } per block.
  { plugins: { local: localPlugin } },

  // ---- server-only guard ---------------------------------------------------
  // Every file in src/server/ and every Route Handler must import "server-only".
  {
    files: ['src/server/**/*.{ts,tsx}', 'src/app/api/**/route.ts'],
    ignores: ['**/*.test.{ts,tsx}', '**/*.integration.test.{ts,tsx}'],
    rules: {
      'local/require-server-only': 'error',
    },
  },

  // ---- use client boundary -------------------------------------------------
  // App Router UI files that use React client hooks must declare "use client".
  {
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
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
      'local/no-raw-interactive-elements': 'error',
    },
  },

  // ---- Raw list element enforcement (Terra source exempted) -----------------
  {
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
      'local/no-raw-list-elements': 'error',
    },
  },

  // ---- Raw form element enforcement (Terra source exempted) -----------------
  {
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
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/terra/**'],
    rules: {
      'local/no-terra-disable-comment': 'error',
    },
  },

  // ---- Inline clipboard.writeText ban (terra and shared components exempted) -
  // Direct navigator.clipboard.writeText() calls in product code are forbidden.
  // Use <CopyButton> from @orbit/components or a Tier 2 src/components/ wrapper.
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/packages/terra/**', 'src/components/**'],
    rules: {
      'local/no-inline-clipboard-write': 'error',
    },
  },

  // ---- Hardcoded colour ban -----------------------------------------------
  // All colour values — hex literals, oklch(), rgb(), hsl() — must reference a
  // Terra CSS variable so they respond to the active theme. Inline colour
  // literals are not themed and silently break in light mode (or any future
  // theme). Use var(--color-*) tokens from tokens.css, or derive from them via
  // color-mix(). Terra's own tokens.css and Storybook stories are excluded.
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/packages/terra/**', '**/*.stories.{ts,tsx}'],
    rules: {
      'local/no-hardcoded-color': 'error',
    },
  },

  // ---- Kebab-case filename enforcement ------------------------------------
  // ALL JavaScript and TypeScript source files under src/ must use kebab-case
  // filenames — this includes the Terra package and any future packages added
  // under src/packages/. Consistent naming prevents case-sensitivity mismatches
  // between macOS (case-insensitive) and Linux CI (case-sensitive) and gives
  // every source zone a single predictable convention.
  {
    files: ['src/**/*.{js,jsx,ts,tsx}'],
    rules: {
      'local/require-kebab-filename': 'error',
    },
  },
]);

export default eslintConfig;
