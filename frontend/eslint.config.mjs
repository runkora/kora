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

  // ---- Hades is framework-agnostic ----------------------------------------
  // Hades components use <img> for app-supplied images, not next/image.
  {
    files: ['src/packages/hades/**/*.tsx'],
    rules: {
      '@next/next/no-img-element': 'off',
    },
  },

  // ---- server-only guard ---------------------------------------------------
  // Every file in src/server/ and every Route Handler must import "server-only".
  {
    files: ['src/server/**/*.{ts,tsx}', 'src/app/**/route.ts'],
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

  // ---- Hades className enforcement -----------------------------------------
  // Hades components are pre-styled through the token system. Applying ad-hoc
  // Tailwind classes via `className` on top of them breaks the design-system
  // contract and creates invisible coupling to internal implementation details.
  //
  // If a layout need can't be met by the component API, use <Stack asChild> or
  // <Grid asChild> and put layout classes on the child element — not on the
  // Hades component. If a genuine variant gap exists, propose it to a human engineer for approval.
  //
  // The rule tracks imports per-file so it only fires on actual Hades
  // components — never on similarly-named locals or third-party components.
  //
  // Hades' own source is excluded: `className` forwarding is fundamental to
  // how Hades components compose internally (cn(defaultClasses, className)).
  {
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/hades/**'],
    rules: {
      'local/no-hades-classname': 'error',
    },
  },

  // ---- Hades typography enforcement ----------------------------------------
  // Outside the Hades design-system package itself, raw HTML text-flow tags
  // (<p>, <h1>–<h6>) are forbidden in JSX. All text must be expressed through
  // Hades typography primitives so colour and scale stay in the token system.
  //
  // Hades' own source is excluded: it IS the canonical implementation site.
  // Story files (*.stories.tsx) inside Hades are also excluded automatically
  // because they live under src/packages/hades/.
  {
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/hades/**'],
    rules: {
      'local/no-raw-html-text-tags': 'error',
    },
  },

  // ---- Raw <div> and semantic layout element enforcement (Hades source exempted)
  // Outside the Hades package, raw <div> and semantic elements misused as layout
  // containers must use Hades layout primitives. Both checks live in the single
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
  // Hades' own source is excluded: it implements the canonical primitives
  // and uses raw elements at the foundation level.
  //
  // There is no escape hatch. Do not suppress violations with disable comments —
  // fix them by using the correct Hades primitive. If a genuine gap exists,
  // propose a new Hades component to a human engineer for approval.
  {
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/hades/**'],
    rules: {
      'local/no-raw-layout-elements': 'error',
    },
  },

  // ---- Raw inline element enforcement (Hades source exempted) ----------------
  // Raw inline HTML elements are banned in product code. Hades is exempt:
  // it is the implementation layer and must be able to render raw elements
  // in order to expose them as components to the rest of the app. Banning
  // them inside Hades would make it impossible to ever add <mark>, <abbr>,
  // <em>, etc. as Hades components — defeating the whole purpose of the rule.
  //
  //   <span>              →  <Text> or <Text asChild>
  //   <strong> / <b>      →  <Text weight="heavy">
  //   <em> / <i>          →  no Hades equivalent yet — propose to human engineer for approval
  //   <mark> / <abbr>     →  no Hades equivalent yet — propose to human engineer for approval
  //   <s> / <u>           →  no Hades equivalent yet — propose to human engineer for approval
  //   <sub> / <sup>       →  no Hades equivalent yet — propose to human engineer for approval
  //
  // There is no escape hatch in product code. If an inline element seems
  // necessary, either restructure the layout or propose a new Hades component
  // to a human engineer for approval.
  // Do not add eslint-disable comments for this rule in product code.
  {
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/hades/**'],
    rules: {
      'local/no-raw-inline-elements': 'error',
    },
  },

  // ---- Direct Lucide import ban (Hades source exempted) ---------------------
  // All icon usage must go through Hades' <Icon> component. Direct imports
  // from 'lucide-react' are banned in product code to ensure the curated
  // IconName registry is the single source of truth for available icons.
  // To add a new icon, propose the addition to a human engineer for approval,
  // then extend the registry in src/packages/hades/src/components/icon/registry.ts.
  // Hades source is exempt — registry.ts must import from lucide-react.
  {
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/hades/**'],
    rules: {
      'local/no-direct-lucide-import': 'error',
    },
  },

  // ---- Raw interactive element enforcement (Hades source exempted) -----------
  // Raw <button> elements must use Hades' <Button> component for consistent
  // focus styles, hover states, and accessibility attributes.
  {
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/hades/**'],
    rules: {
      'local/no-raw-interactive-elements': 'error',
    },
  },

  // ---- Raw list element enforcement (Hades source exempted) -----------------
  {
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/hades/**'],
    rules: {
      'local/no-raw-list-elements': 'error',
    },
  },

  // ---- Raw form element enforcement (Hades source exempted) -----------------
  {
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/hades/**'],
    rules: {
      'local/no-raw-form-elements': 'error',
    },
  },

  // ---- Hades disable-comment ban (product code only) ----------------------
  // Disable directive comments targeting any of the five Hades rules are
  // strictly forbidden in product code. Hades violations must be fixed by
  // using the correct Hades component (or proposing a new one for human
  // approval) — never by suppressing the lint error. Hades source is excluded
  // because it uses legitimate disable comments for unrelated rules (e.g.
  // react-hooks/rules-of-hooks in Storybook story render functions).
  {
    files: ['src/**/*.tsx'],
    ignores: ['src/packages/hades/**'],
    rules: {
      'local/no-hades-disable-comment': 'error',
    },
  },

  // ---- Inline clipboard.writeText ban (hades and shared components exempted) -
  // Direct navigator.clipboard.writeText() calls in product code are forbidden.
  // Use <CopyButton> from @orbit/components or a Tier 2 src/components/ wrapper.
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/packages/hades/**', 'src/components/**'],
    rules: {
      'local/no-inline-clipboard-write': 'error',
    },
  },

  // ---- Hardcoded colour ban -----------------------------------------------
  // All colour values — hex literals, oklch(), rgb(), hsl() — must reference a
  // Hades CSS variable so they respond to the active theme. Inline colour
  // literals are not themed and silently break in light mode (or any future
  // theme). Use var(--color-*) tokens from tokens.css, or derive from them via
  // color-mix(). Hades' own tokens.css and Storybook stories are excluded.
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/packages/hades/**', '**/*.stories.{ts,tsx}'],
    rules: {
      'local/no-hardcoded-color': 'error',
    },
  },

  // ---- Hades compound components in Server Components ----------------------
  // Hades' entrypoint carries "use client", so a Server Component importing a
  // composed component receives a client reference with none of the
  // Object.assign sub-components attached: <SidebarLayout.Main> renders as
  // undefined and React throws "Element type is invalid". Types do not catch
  // this and jsdom tests do not reproduce it — neither has an RSC boundary.
  {
    files: ['src/app/**/*.{ts,tsx}', 'src/components/**/*.{ts,tsx}'],
    rules: {
      'local/no-hades-compound-in-server-component': 'error',
    },
  },

  // ---- Product-name literal ban --------------------------------------------
  // The product name is a deployment value (white-labelling theme ADR, decision
  // 5): a theme can rename it, so copy that spells `kora` or `orbit` as a word
  // is a leak. Read it from the brand instead: useBrand().productName in client
  // components, getBrand() from @orbit/server/theme in server code.
  // Identifier-shaped literals pass (orbit.io, kora-base, ORBIT_API_KEY), as do
  // import sources, object keys, computed member keys and literal types.
  //
  // Mock data is excluded until it goes: the *-mock-data.ts modules beside the
  // dashboard pages and model-gateway/_data/mock.ts. Tests and stories are
  // excluded because they assert on the default brand.
  {
    files: ['src/app/**/*.{ts,tsx}', 'src/components/**/*.{ts,tsx}'],
    ignores: [
      '**/*.test.{ts,tsx}',
      '**/*.integration.test.{ts,tsx}',
      '**/*.stories.{ts,tsx}',
      '**/*-mock-data.ts',
      '**/_data/mock.ts',
    ],
    rules: {
      'local/no-product-name-literal': 'error',
    },
  },

  // ---- Kebab-case filename enforcement ------------------------------------
  // ALL JavaScript and TypeScript source files under src/ must use kebab-case
  // filenames — this includes the Hades package and any future packages added
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
