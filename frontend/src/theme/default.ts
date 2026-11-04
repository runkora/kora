/**
 * The kora default theme's logo and product name. Hades holds no kora identity.
 * `getBrand()` (server) and `useBrand()` (client) return this overlaid by the deployment's theme.
 */
export interface IdentityTheme {
  readonly productName: string;
  /** Logo source per mode. */
  readonly logo: { readonly dark: string; readonly light: string };
  /** Favicon source per mode. Absent on the kora default, where `app/icon.svg` supplies it. */
  readonly favicon?: { readonly dark?: string; readonly light?: string };
}

export const defaultTheme: IdentityTheme = {
  productName: 'kora',
  logo: {
    dark: '/brand/runkora-logo-dark.svg',
    light: '/brand/runkora-logo-light.svg',
  },
};
