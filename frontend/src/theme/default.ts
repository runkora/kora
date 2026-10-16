/**
 * The kora default theme's identity values.
 *
 * Hades contains no kora identity: the logo is an asset of this theme, served
 * from `public/brand/`, and the product name is a value. Every identity surface reads from here, so a loaded theme replaces
 * one object rather than finding call sites. The loader that overlays a
 * deployment's theme on these defaults is the white-labelling theme ADR's
 * decision 4; until it exists, this is the effective theme.
 */
export interface IdentityTheme {
  readonly productName: string;
  /** Logo source per mode. */
  readonly logo: { readonly dark: string; readonly light: string };
}

export const defaultTheme: IdentityTheme = {
  productName: 'kora',
  logo: {
    dark: '/brand/kora-logo-dark.svg',
    light: '/brand/kora-logo-light.svg',
  },
};
