import { Anchor, type AnchorProps } from '@runkora/hades';
import { ExternalUrl } from '@runkora/lib/routing';

type ExternalLinkProps = Omit<AnchorProps, 'href' | 'asChild' | 'children'> & {
  children: React.ReactNode;
  /**
   * The external URL to link to.
   *
   * Must be a value from the `EXTERNAL_URLS` registry in `external-urls.ts`.
   * Passing an unlisted URL is a TypeScript error. To link to a new external
   * resource, add it to `EXTERNAL_URLS` first.
   *
   * Always opens in a new tab with `rel="noopener noreferrer"` applied.
   *
   * @example url="https://docs.orbit.dev"
   */
  url: ExternalUrl;
};

/**
 * ExternalLink
 *
 * Renders a Hades `Anchor` pointing to an external URL.
 *
 * The `url` prop accepts only URLs registered in `external-urls.ts` — an
 * unlisted URL is a compile-time error. To link to a new external resource,
 * add it to the `EXTERNAL_URLS` array in `external-urls.ts` first.
 *
 * Always opens in a new tab with `target="_blank"` and
 * `rel="noopener noreferrer"` applied automatically.
 *
 * For internal navigation, use `AppLink` instead.
 *
 * @example
 *   <ExternalLink url="https://docs.orbit.dev">Documentation</ExternalLink>
 *
 * @example Compact inline reference
 *   <ExternalLink size="sm" url="https://openid.net/specs/openid-connect-core-1_0.html">
 *     OIDC spec
 *   </ExternalLink>
 */
export function ExternalLink({ url, children, ...props }: ExternalLinkProps) {
  return (
    <Anchor href={url} target="_blank" rel="noopener noreferrer" {...props}>
      {children}
    </Anchor>
  );
}
