/**
 * Orbit — root landing page.
 *
 * Temporary placeholder until the real dashboard is built.
 * The HealthStatus component fetches via the BFF /api/example/health route
 * so the browser never communicates with the Go backend directly.
 */
import { HealthStatus } from "./_components/HealthStatus";

export default function RootPage() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center bg-gray-950 text-white">
      <div className="flex flex-col items-center gap-6 text-center">
        {/* Wordmark */}
        <h1 className="text-5xl font-bold tracking-tight">Orbit</h1>
        <p className="max-w-md text-gray-400 text-lg">
          Control plane for autonomous AI agents.
        </p>

        {/* Backend connection status */}
        <HealthStatus />
      </div>
    </main>
  );
}
