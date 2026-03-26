import type { NextConfig } from 'next';

const nextConfig: NextConfig = {
  // Treat the @orbit/terra workspace package as local source so that
  // Next.js/Turbopack resolves its symlink for both JS and CSS imports.
  transpilePackages: ['@orbit/terra'],
  // Emit standalone output for containerised deployments
  output: process.env.NEXT_OUTPUT === 'standalone' ? 'standalone' : undefined,
  // Forward bare ORBIT_OIDC_* vars to the browser bundle without requiring a
  // NEXT_PUBLIC_ prefix. Values are read from the server-side environment at
  // build time and statically inlined. This keeps the naming consistent with
  // the backend config and avoids a second set of duplicate env var names.
  env: {
    ORBIT_OIDC_ISSUER_URL: process.env.ORBIT_OIDC_ISSUER_URL ?? '',
    ORBIT_OIDC_CLIENT_ID: process.env.ORBIT_OIDC_CLIENT_ID ?? '',
  },
};

export default nextConfig;
