import type { NextConfig } from 'next';

const nextConfig: NextConfig = {
  // Treat the @orbit/terra workspace package as local source so that
  // Next.js/Turbopack resolves its symlink for both JS and CSS imports.
  transpilePackages: ['@orbit/terra'],
  // Emit standalone output for containerised deployments
  output: process.env.NEXT_OUTPUT === 'standalone' ? 'standalone' : undefined,
};

export default nextConfig;
