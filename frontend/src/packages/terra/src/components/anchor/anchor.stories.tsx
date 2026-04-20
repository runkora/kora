import type { Meta, StoryObj } from '@storybook/react';
import { Anchor } from './anchor';
import { Text } from '../typography/typography';

// ─── Meta ─────────────────────────────────────────────────────────────────────

const meta = {
  title: 'Terra/Atoms/Anchor',
  component: Anchor,
  parameters: {
    layout: 'padded',
  },
} satisfies Meta<typeof Anchor>;

export default meta;
type Story = StoryObj<typeof Anchor>;

// ─── Helpers ──────────────────────────────────────────────────────────────────

/**
 * Labelled row: prop name → when to use → rendered output.
 * The hint column is intentionally dim — it's guidance, not content.
 */
function Row({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-baseline gap-6 py-2 border-b border-border-subtle last:border-0">
      <div className="w-36 shrink-0 font-mono text-xs text-subtle">{label}</div>
      {hint && <div className="w-64 shrink-0 text-xs text-subtle italic">{hint}</div>}
      <div className="flex-1 min-w-0">{children}</div>
    </div>
  );
}

/** Section divider used to group related rows within a story. */
function SectionLabel({ title }: { title: string }) {
  return (
    <div className="mt-8 mb-3">
      <Text size="caption" color="subtle" mono transform="uppercase">
        {title}
      </Text>
    </div>
  );
}

// ─── Stories ──────────────────────────────────────────────────────────────────

/**
 * The three `size` variants control the rendered font-size.
 *
 * `inherit` is the default — Anchor deliberately carries no font-size of its
 * own so it can sit inside body copy without breaking the surrounding rhythm.
 * Use `sm` or `md` only when Anchor renders as a standalone element (e.g. a
 * secondary form link) that needs an explicit size independent of its container.
 */
export const Sizes: Story = {
  render: () => (
    <div>
      <SectionLabel title="Anchor — size" />
      <Row label="inherit" hint="Default — no font-size applied; inherits from parent">
        <Anchor href="#">Follows parent text size</Anchor>
      </Row>
      <Row label="md" hint="text-sm — for standalone links in body-copy contexts">
        <Anchor href="#" size="md">
          Read the documentation
        </Anchor>
      </Row>
      <Row label="sm" hint="text-xs — for secondary form links (e.g. Forgot password?)">
        <Anchor href="#" size="sm">
          Forgot password?
        </Anchor>
      </Row>
    </div>
  ),
};

/**
 * Anchor inside body copy via `size="inherit"`.
 *
 * The most common usage — drop an Anchor directly into a `Text` paragraph and
 * it adopts the surrounding type scale without any size override. The accent
 * colour and hover underline distinguish it from the surrounding text.
 */
export const InlineInText: Story = {
  name: 'Inline in Text',
  render: () => (
    <div className="max-w-lg space-y-4">
      <SectionLabel title="Anchor — inside Text (size=inherit)" />
      <Row label="body" hint="Default Text size">
        <Text>
          Orbit uses <Anchor href="#">OpenID Connect</Anchor> to federate agent identities across
          providers. See the <Anchor href="#">RFC 8628</Anchor> device flow for headless
          authentication.
        </Text>
      </Row>
      <Row label="supporting" hint="Smaller Text size — Anchor inherits automatically">
        <Text size="supporting" color="muted">
          Credentials are scoped and time-limited.{' '}
          <Anchor href="#">Learn how credential brokering works.</Anchor>
        </Text>
      </Row>
      <Row label="caption" hint="Smallest Text size — Anchor inherits automatically">
        <Text size="caption" color="subtle">
          By continuing you agree to the <Anchor href="#">Terms of Service</Anchor> and{' '}
          <Anchor href="#">Privacy Policy</Anchor>.
        </Text>
      </Row>
    </div>
  ),
};

/**
 * `asChild` merges Anchor styles onto a child element rather than rendering
 * its own `<a>` tag. The pattern is used by `AppLink` and `ExternalLink` to
 * avoid nesting `<a>` inside `<a>` when composing with a router link.
 *
 * In application code, prefer the typed app-level composites:
 *   - `AppLink`      — internal Next.js navigation
 *   - `ExternalLink` — external URLs with `rel="noopener noreferrer"`
 *
 * Use `asChild` directly only inside Terra or in exceptional edge cases.
 */
export const AsChild: Story = {
  name: 'asChild (composition)',
  render: () => (
    <div>
      <SectionLabel title="Anchor — asChild" />
      <Row label="asChild" hint="Anchor styles applied to child element — no nested <a>">
        {/* In a real app this would be a Next.js Link or router component */}
        <Anchor asChild size="md">
          <a href="#">Router link with Anchor styling</a>
        </Anchor>
      </Row>
      <Row label="asChild sm" hint="Secondary form link — router-aware">
        <Anchor asChild size="sm">
          <a href="#">Forgot password?</a>
        </Anchor>
      </Row>
    </div>
  ),
};

/**
 * A realistic form-card footer showing both sizes in context — the secondary
 * "Forgot password?" link at `sm` alongside a standard "Sign up" link at
 * `inherit` inside a `Text` paragraph.
 *
 * This is the canonical usage pattern that drove the `size` variant API.
 */
export const Composed: Story = {
  render: () => (
    <div className="max-w-sm space-y-6">
      {/* Login form footer — the motivating use case for size="sm" */}
      <div className="rounded-lg border border-border bg-surface p-6 space-y-4">
        <div className="space-y-1">
          <Text size="supporting" weight="emphasis">
            Email
          </Text>
          <div className="flex items-center justify-between">
            <Text size="caption" color="subtle">
              agent@orbit.internal
            </Text>
            <Anchor href="#" size="sm">
              Forgot password?
            </Anchor>
          </div>
        </div>
        <div className="pt-2 border-t border-border-subtle">
          <Text size="supporting" color="muted">
            Don&apos;t have an account? <Anchor href="#">Sign up</Anchor>
          </Text>
        </div>
      </div>

      {/* Documentation callout — inline links in body copy */}
      <div className="rounded-lg border border-border bg-surface p-6 space-y-2">
        <Text weight="emphasis">Credential brokering</Text>
        <Text size="supporting" color="muted">
          Agents never hold raw secrets. Orbit issues scoped, time-limited tokens via the{' '}
          <Anchor href="#">credential broker</Anchor> based on the requesting agent&apos;s{' '}
          <Anchor href="#">policy scope</Anchor>.
        </Text>
        <Text size="caption" color="subtle">
          <Anchor href="#" size="sm">
            Read the architecture guide →
          </Anchor>
        </Text>
      </div>
    </div>
  ),
};
