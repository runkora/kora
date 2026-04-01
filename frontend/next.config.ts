import type { NextConfig } from 'next';

const nextConfig: NextConfig = {
  // Treat the @orbit/terra workspace package as local source so that
  // Next.js/Turbopack resolves its symlink for both JS and CSS imports.
  transpilePackages: ['@orbit/terra'],
  // Emit standalone output for containerised deployments
  output: process.env.NEXT_OUTPUT === 'standalone' ? 'standalone' : undefined,
  // ORBIT_OIDC_ISSUER_URL and ORBIT_OIDC_CLIENT_ID have been intentionally
  // removed. Tenant OIDC configuration is now resolved at runtime via the
  // Home Realm Discovery endpoint (ADR-0031) — there is no longer a single
  // build-time issuer or client ID to bake in.
};

export default nextConfig;
