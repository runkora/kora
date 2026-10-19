/**
 * The kora default theme's identity values.
 *
 * Every identity surface reads the logo and product name from here, so a
 * deployment's theme can replace one object. Hades holds no kora identity.
 * The logo files are served from `public/brand/`.
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
