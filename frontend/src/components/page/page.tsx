import * as React from 'react';
import { PageLayout, Stack } from '@orbit/terra';

// ── Discriminated union ───────────────────────────────────────────────────────

/**
 * Centred full-screen page — no sidebar, no header.
 * Use for: landing, auth, onboarding, error pages.
 */
interface CenteredPage {
  variant: 'centered';
  children: React.ReactNode;
}

/**
 * Shell page — fixed sidebar (required) + optional header + scrollable main.
 * Use for: all dashboard / app pages.
 *
 * TypeScript enforces that `sidebar` is provided — a `shell` page without a
 * sidebar is a type error.
 */
interface ShellPage {
  variant: 'shell';
  sidebar: React.ReactNode;
  header?: React.ReactNode;
  children: React.ReactNode;
}

/**
 * Canvas page — no sidebar, optional header, full-bleed scrollable main.
 * Use for: AI chat, document editors, map/canvas views.
 */
interface CanvasPage {
  variant: 'canvas';
  header?: React.ReactNode;
  children: React.ReactNode;
}

export type PageProps = CenteredPage | ShellPage | CanvasPage;

// ── Component ─────────────────────────────────────────────────────────────────

/**
 * Page
 *
 * The single page-root entry point for all Orbit app pages. Every Next.js
 * page renders `<Page variant="…">` at its root — Terra primitives are never
 * used directly for page framing.
 *
 * Three variants are available via a discriminated union. TypeScript enforces
 * the correct props for each variant at compile time.
 *
 * ### `centered`
 * Full-screen, vertically and horizontally centred. No chrome.
 * For landing, auth, onboarding, and error pages.
 *
 * ```tsx
 * export default function LoginPage() {
 *   return (
 *     <Page variant="centered">
 *       <LoginForm />
 *     </Page>
 *   );
 * }
 * ```
 *
 * ### `shell`
 * Fixed sidebar (required by the type) + optional top header + scrollable main.
 * For all dashboard and app pages.
 *
 * ```tsx
 * export default function DashboardPage() {
 *   return (
 *     <Page variant="shell" sidebar={<AppSidebar />} header={<AppHeader />}>
 *       <Section heading="Overview">…</Section>
 *     </Page>
 *   );
 * }
 * ```
 *
 * ### `canvas`
 * No sidebar, optional header, full-bleed scrollable main.
 * For AI chat, document editors, and map/canvas views.
 *
 * ```tsx
 * export default function ChatPage() {
 *   return (
 *     <Page variant="canvas" header={<ChatHeader />}>
 *       <ChatCanvas />
 *     </Page>
 *   );
 * }
 * ```
 */
export function Page(props: PageProps) {
  switch (props.variant) {
    case 'centered':
      return (
        <Stack asChild direction="vertical" align="center" justify="center" minHeight="screen">
          <main>{props.children}</main>
        </Stack>
      );

    case 'shell':
      return (
        <PageLayout sidebar={props.sidebar} header={props.header}>
          {props.children}
        </PageLayout>
      );

    case 'canvas':
      return <PageLayout header={props.header}>{props.children}</PageLayout>;
  }
}
