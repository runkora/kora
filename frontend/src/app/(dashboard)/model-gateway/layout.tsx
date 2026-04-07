// Shared layout for all model-gateway pages.
// Provides consistent page padding — no data fetching, no providers.

export default function ModelGatewayLayout({ children }: { children: React.ReactNode }) {
  return <main className="px-6 py-8">{children}</main>;
}
