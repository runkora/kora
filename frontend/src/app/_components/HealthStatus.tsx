"use client";

/**
 * Displays the Go backend connection status.
 *
 * Fetches from the BFF /api/example/health route — never calls the Go
 * backend directly.
 */
import { useEffect, useState } from "react";
import type { HealthResponse } from "@/shared/api";

type State =
  | { status: "loading" }
  | { status: "ok"; data: HealthResponse }
  | { status: "error" };

export function HealthStatus() {
  const [state, setState] = useState<State>({ status: "loading" });

  useEffect(() => {
    fetch("/api/example/health")
      .then(async (res) => {
        const body = await res.json();
        if (!res.ok || body.error) {
          setState({ status: "error" });
        } else {
          setState({ status: "ok", data: body.data as HealthResponse });
        }
      })
      .catch(() => setState({ status: "error" }));
  }, []);

  return (
    <div className="mt-4 rounded-lg border border-gray-800 bg-gray-900 px-6 py-4 text-sm">
      <p className="text-gray-500 mb-1 uppercase tracking-wider text-xs">
        Backend
      </p>
      {state.status === "loading" && (
        <p className="text-gray-400">Checking…</p>
      )}
      {state.status === "ok" && (
        <p className="text-green-400">
          ✓ {state.data.service} — {state.data.status}
        </p>
      )}
      {state.status === "error" && (
        <p className="text-red-400">
          ✗ Unreachable — is <code className="text-red-300">make dev</code> running?
        </p>
      )}
    </div>
  );
}
