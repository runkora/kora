import 'server-only';

import { loadErrorMessage } from '@runkora/lib/load-error';
import { ApiError } from './api-error';

/** One section of a page load. A failed section carries a sentence for the page. */
export type Loaded<T> = { ok: true; data: T } | { ok: false; status: number; message: string };

/** A failed section load, with a 403 read as no access. */
export function loadFailure(err: unknown, subject: string): Loaded<never> {
  const status = err instanceof ApiError ? err.status : 500;
  return { ok: false, status, message: loadErrorMessage(status, subject) };
}
