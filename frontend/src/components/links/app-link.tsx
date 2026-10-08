import NextLink from 'next/link';
import { Anchor, type AnchorProps } from '@orbit/hades';
import { type AppRoute } from '@orbit/lib/routing/routes';

type AppLinkProps = Omit<AnchorProps, 'href' | 'asChild' | 'children'> & {
  children: React.ReactNode;
  /**
   * The internal route to navigate to.
   *
   * Must be a value from the `APP_ROUTES` registry in `routes.ts`.
   * Passing an unlisted path is a TypeScript error.
   *
   * @example to="/chat"
   */
  to: AppRoute;
};

/**
 * AppLink
 *
 * Composes Hades `Anchor` (styling) and Next.js `Link` (client-side routing)
 * for internal navigation.
 *
 * The `to` prop accepts only routes registered in `routes.ts` — an unrecognised
 * path is a compile-time error. To navigate to a new route, add it to the
 * `APP_ROUTES` array in `routes.ts` first.
 *
 * For external links, use `ExternalLink` instead.
 *
 * @example
 *   <AppLink to="/chat">Chat</AppLink>
 *
 * @example Compact secondary form link
 *   <AppLink size="sm" to="/forgot-password">Forgot password?</AppLink>
 */
export function AppLink({ to, children, ...props }: AppLinkProps) {
  return (
    <Anchor asChild {...props}>
      <NextLink href={to}>{children}</NextLink>
    </Anchor>
  );
}
