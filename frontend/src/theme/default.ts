/**
 * The kora default theme's identity values.
 *
 * Hades contains no kora identity: the logo is an asset of this theme, served
 * from `public/brand/`, and the product name is a value. Every identity
 * surface reads the effective brand, which is this object overlaid by the
 * deployment's theme. Server code calls `getBrand()` from `@orbit/server/theme`.
 * Client components call `useBrand()` from `@orbit/theme/brand-provider`.
 */
export interface IdentityTheme {
  readonly productName: string;
  /** Logo source per mode. */
  readonly logo: { readonly dark: string; readonly light: string };
  /**
   * Favicon source per mode. Absent on the kora default, where Next's
   * `app/icon.svg` supplies it. A theme's favicon slot replaces it for the
   * modes the theme sets.
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
