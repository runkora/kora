import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // BFF: strip /api prefix when proxying to the Go backend is handled
  // in individual API route handlers, not via rewrites, so the BFF
  // can inject auth headers server-side before forwarding.

  // Emit standalone output for containerised deployments
  output: process.env.NEXT_OUTPUT === "standalone" ? "standalone" : undefined,
};

export default nextConfig;
