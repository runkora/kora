/**
 * Helpers for items declared in a tenant's configuration repository.
 *
 * A declared item (`source: "crd"`) is changed only through its resource, and
 * a tenant with API authoring off takes every change from its repository. The
 * API refuses such writes with the codes below.
 */

/** Where an item came from: the API, or a declarative resource. */
export type ResourceSource = 'api' | 'crd';

/** Whether the item is projected from a declarative resource. */
export function isDeclared(item: { source?: ResourceSource } | null | undefined): boolean {
  return item?.source === 'crd';
}

const REFUSALS: Record<string, string> = {
  managed_by_resource: 'This item is managed in git. Make changes in the configuration repository.',
  api_authoring_disabled:
    'This tenant is managed in git. Make changes in the configuration repository.',
};

/** The message for a write refused because the item or tenant is managed in git, or null. */
export function declarativeRefusal(code: unknown): string | null {
  return typeof code === 'string' ? (REFUSALS[code] ?? null) : null;
}

/**
 * The message for a refused write: the managed-in-git explanation for the
 * declarative refusal codes, else the API's own message.
 */
export function writeErrorMessage(status: number, body: unknown): string {
  const { code, message } = (body ?? {}) as { code?: unknown; message?: unknown };
  return (
    declarativeRefusal(code) ??
    (typeof message === 'string' && message ? message : `Request failed (${status})`)
  );
}
