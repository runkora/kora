/**
 * BFF route: GET /api/example/health
 *
 * Proxies the Go backend health check server-side so the browser never
 * communicates with the Go API directly. Auth headers and credentials can
 * be injected here before forwarding.
 */
import "server-only";
import { NextResponse } from "next/server";
import { orbitClient, ApiError } from "@/server/client";
import type { HealthResponse } from "@/shared/api";

export async function GET() {
  try {
    const data = await orbitClient.get<HealthResponse>("/example/health");
    return NextResponse.json({ data });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json(
        { error: { code: err.status, message: err.message } },
        { status: err.status },
      );
    }
    console.error("[api/example/health] unexpected error:", err);
    return NextResponse.json(
      { error: { code: 502, message: "Backend unreachable" } },
      { status: 502 },
    );
  }
}
