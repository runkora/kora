/**
 * The BFF's unified upstream error — server-side only.
 *
 * Lives in its own module, apart from the fetch client, so code that only
 * needs the error type (route-helpers' catch, tests) can import it without
 * dragging in `config`, whose import-time env requirements would fail outside
 * a configured server process.
 */
import 'server-only';

export class ApiError extends Error {
  /**
   * `details` is the upstream's structured context, such as the scopes a 403
   * says are missing. The BFF passes it through to the browser.
   */
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
    public readonly details?: Record<string, unknown>,
  ) {
    super(message);
    this.name = 'ApiError';
  }

  get isNotFound() {
    return this.status === 404;
  }

  get isConflict() {
    return this.status === 409;
  }

  get isUnauthorized() {
    return this.status === 401;
  }
}
