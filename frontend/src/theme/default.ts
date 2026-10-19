/**
 * The kora default theme's logo and product name, read by every identity surface.
 * Hades holds no kora identity.
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
