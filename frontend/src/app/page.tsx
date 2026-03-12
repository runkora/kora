/**
 * Orbit — root landing page.
 *
 * Temporary placeholder until the real dashboard is built.
 * Exercises the BFF health endpoint on the server side so we can confirm
 * the Next.js → Go backend connection is wired correctly.
 */
import { orbitClient, ApiError } from "@/server/client";
import type { HealthResponse } from "@/shared/api";

async function getBackendHealth(): Promise<HealthResponse | null> {
  try {
    return await orbitClient.get<HealthResponse>("/example/health");
  } catch (err) {
    if (err instanceof ApiError) {
      console.error("[page] health check failed:", err.status, err.message);
    } else {
      console.error("[page] unexpected health check error:", err);
    }
    return null;
  }
}

export default async function RootPage() {
  const health = await getBackendHealth();

  return (
    <main className="flex min-h-screen flex-col items-center justify-center bg-gray-950 text-white">
      <div className="flex flex-col items-center gap-6 text-center">
        {/* Wordmark */}
        <h1 className="text-5xl font-bold tracking-tight">Orbit</h1>
        <p className="max-w-md text-gray-400 text-lg">
          Control plane for autonomous AI agents.
        </p>

        {/* Backend connection status */}
        <div className="mt-4 rounded-lg border border-gray-800 bg-gray-900 px-6 py-4 text-sm">
          <p className="text-gray-500 mb-1 uppercase tracking-wider text-xs">
            Backend
          </p>
          {health ? (
            <p className="text-green-400">
              ✓ {health.service} — {health.status}
            </p>
          ) : (
            <p className="text-red-400">
              ✗ Unreachable — is <code className="text-red-300">make dev</code> running?
            </p>
          )}
        </div>
      </div>
    </main>
  );
}
