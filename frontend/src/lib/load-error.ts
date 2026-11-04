/**
 * Turns a failed load into a sentence for the page. A 403 reads as no access,
 * never as empty or as an expired session. `detail` is a backend message, used
 * for other failures; never pass a raw response body.
 */
export function loadErrorMessage(status: number, subject: string, detail?: string): string {
  if (status === 401) return 'Session expired. Sign in again.';
  if (status === 403) return `You do not have access to ${subject}.`;
  return detail || `Could not load ${subject}. Try again.`;
}
