/**
 * The kora default theme's identity values.
 *
 * Hades contains no kora identity (contract boundary rule 3): the logo is an
 * asset of this theme, served from `public/brand/`, and the product name is a
 * value. Every identity surface reads from here through the effective brand,
 * which is this object overlaid by the deployment's theme: `getBrand()` from
 * `@orbit/server/theme` in server code, `useBrand()` from
 * `@orbit/theme/brand-provider` in client components. See the white-labelling
 * theme ADR, decisions 4 and 5.
 */
export interface IdentityTheme {
  readonly productName: string;
  /** Logo source per mode. */
  readonly logo: { readonly dark: string; readonly light: string };
  /**
   * Favicon source per mode. Absent on the kora default, where Next's
   * `app/icon.svg` file convention supplies it; a theme that sets the favicon
   * slot replaces that for the modes it sets.
   */
  readonly favicon?: { readonly dark?: string; readonly light?: string };
}

export const defaultTheme: IdentityTheme = {
  productName: 'kora',
  logo: {
    dark: '/brand/kora-logo-dark.svg',
    light: '/brand/kora-logo-light.svg',
  },
};
