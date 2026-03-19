/**
 * Storybook preview configuration for Terra.
 *
 * This file is the single entry point executed by Storybook before any story
 * renders. It is responsible for two things:
 *
 *   1. Loading Terra's design tokens so components have access to the full
 *      token system (colours, typography, spacing) during development.
 *
 *   2. Configuring global Storybook parameters — background presets, Controls
 *      addon matchers, and the Docs tab theme.
 *
 * @see https://storybook.js.org/docs/react/writing-stories/parameters
 */

import type { Preview } from '@storybook/react';

/**
 * Import Terra's token stylesheet directly via relative path.
 *
 * tokens.css is the root of the Terra CSS graph. It opens with
 * `@import 'tailwindcss'`, which means this single import brings in:
 *   - The full Tailwind v4 utility layer (processed by @tailwindcss/vite)
 *   - All raw OKLCH palette values (`--terra-raw-*`)
 *   - All semantic design tokens (`--color-*`, `--font-*`, `--radius-*`)
 *     mapped into Tailwind via `@theme inline`
 *
 * The path must be a relative filesystem path. Bare package specifiers
 * (e.g. `@orbit/terra/src/tokens/tokens.css`) do not work here because
 * Vite resolves CSS imports via the filesystem, not Node module resolution.
 */
import '../src/tokens/tokens.css';

const preview: Preview = {
  parameters: {
    /**
     * Background presets for the Storybook canvas toolbar.
     *
     * The three values mirror Terra's surface token hierarchy so stories can
     * be verified on the actual backgrounds they will appear on in production:
     *
     *   orbit   — `--color-background` (#070a0f)  — the root page background,
     *             the darkest surface. Default because most Orbit UI renders here.
     *
     *   surface — `--color-surface`    (#0d1117)  — card and panel backgrounds,
     *             one step lighter than orbit. Use for components that live
     *             inside cards or sidebars.
     *
     *   light   — #ffffff                         — forces a white background to
     *             catch contrast or theming issues that only appear on light
     *             surfaces. Not a Terra token — Terra is dark-first.
     *
     * Note: values are hardcoded hex strings, not CSS custom properties.
     * Storybook's background switcher runs outside the component render tree
     * and therefore cannot read `var(--color-background)` at runtime.
     * Keep these in sync with the actual token values in tokens.css if the
     * root surface colours ever change.
     */
    backgrounds: {
      default: 'orbit',
      values: [
        { name: 'orbit', value: '#070a0f' },
        { name: 'surface', value: '#0d1117' },
        { name: 'light', value: '#ffffff' },
      ],
    },

    /**
     * Controls addon matchers.
     *
     * Tells the Controls panel which prop names should receive a specialised
     * input widget rather than a plain text field:
     *
     *   color  — any prop whose name ends in `background` or `color` gets a
     *            colour-picker widget. Covers common React patterns like
     *            `backgroundColor`, `color`, `borderColor`.
     *
     *   date   — any prop whose name ends in `date` gets a date-picker widget.
     *            Covers props like `createdAt`, `expiryDate`.
     */
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /date$/i,
      },
    },

    /**
     * Docs tab theme.
     *
     * `undefined` instructs Storybook to use its built-in default theme for
     * the Docs tab rather than any custom theme object. In practice this
     * inherits the dark variant of Storybook's default Manager theme.
     *
     * If a Terra-branded docs theme is needed in future, replace `undefined`
     * with a `create()` result from `@storybook/theming`.
     */
    docs: {
      theme: undefined,
    },

    /**
     * Sidebar sort order.
     *
     * Storybook defaults to alphabetical ordering, which would place
     * `Atoms` before `Foundation`. The explicit order below pins
     * `Overview` first (the full Component Grid) so the single-page
     * system reference is always the top entry. `Foundation` (design
     * tokens) follows, then the component categories.
     * Any group not listed here falls back to alphabetical after the
     * declared entries.
     */
    options: {
      storySort: {
        order: ['Terra', ['Overview', 'Foundation', 'Atoms', 'Typography', 'Layout', 'Organisms']],
      },
    },
  },
};

export default preview;
