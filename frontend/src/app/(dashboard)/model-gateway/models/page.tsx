// RSC — runs exclusively on the server. No 'use client' directive.
//
// Responsibilities:
//   1. Validate the session (middleware already guards the route, but we
//      double-check here so TypeScript knows accessToken is present).
//   2. Fetch the model list from the inference gateway directly using the
//      session token — no round-trip through the BFF route handler.
//   3. Transform the raw API response into ModelRow[] via toModelRow.
//   4. Pass the data to ModelsClientShell synchronously — no loading flash.

import { getSession, isSessionValid } from '@orbit/server/session';
import { config } from '@orbit/server/config';
import type { ModelsListResponse } from '@orbit/shared/api';
import { toModelRow } from '../_types';
import { ModelsClientShell } from './_components/models-client-shell';

async function loadModels(accessToken: string) {
  try {
    const res = await fetch(`${config.inferenceGatewayUrl}/models`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
    });

    if (!res.ok) {
      console.error(
        `[loadModels] gateway responded with ${res.status} ${res.statusText}:`,
        await res.text().catch(() => '(could not read body)'),
      );
      return [];
    }

    const body: ModelsListResponse = await res.json();
    return (body.data ?? []).map(toModelRow);
  } catch (err) {
    // Network error (ECONNREFUSED, timeout) or JSON parse failure.
    // Log for server-side visibility; return empty list so the page still
    // renders — the client-side refresh() action will retry.
    console.error('[loadModels] failed to fetch models from gateway:', err);
    return [];
  }
}

export default async function ModelsPage() {
  const session = await getSession();

  // Middleware guards this route, so an invalid session here is a rare edge
  // case (e.g. cookie expired between middleware check and handler). Render
  // with empty initial data; the client will show an error on mount.
  const initialData = isSessionValid(session) ? await loadModels(session.accessToken) : [];

  return <ModelsClientShell initialData={initialData} />;
}
