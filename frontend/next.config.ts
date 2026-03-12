import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Emit standalone output for containerised deployments
  output: process.env.NEXT_OUTPUT === "standalone" ? "standalone" : undefined,
};

export default nextConfig;
