/** @tier 2 — kora-domain-aware: the scope shape ScopePicker consumes. Used by: scope-picker, policy grants/roles pages, model-gateway keys page. */

/**
 * One selectable scope, with the vocabulary metadata that lets the picker group and describe it.
 * Only `scope` is required: a bare scope string is grouped by splitting it on `:`.
 */
export interface ScopeOption {
  /** The scope string (e.g. `mcp:github:issues.create:use`). */
  scope: string;
  /** What the scope allows, shown under the scope string and searched. */
  description?: string;
  /** Top-level group (e.g. `mcp`, `connection`, `agents`). Falls back to the scope's first segment. */
  resourceType?: string;
  /** Path under the resource type; `path[0]` is the server or connection. Falls back to the scope string. */
  path?: string[];
}

/** The vocabulary-entry fields {@link toScopeOption} reads; `ScopeEntry` satisfies it structurally. */
interface ScopeEntryLike {
  scope: string;
  resource_type?: string;
  path?: string[] | null;
  description?: string;
}

/** Maps a policy vocabulary entry to the picker's input type. */
export function toScopeOption(entry: ScopeEntryLike): ScopeOption {
  return {
    scope: entry.scope,
    description: entry.description || undefined,
    resourceType: entry.resource_type || undefined,
    path: entry.path ?? undefined,
  };
}

/**
 * Narrows the vocabulary to the scopes in `allowed`, in `allowed`'s order. A scope the vocabulary
 * does not know stays selectable as a bare string, so the picker never loses an effective scope.
 */
export function scopeOptionsFor(allowed: string[], vocabulary: ScopeEntryLike[]): ScopeOption[] {
  const byScope = new Map(vocabulary.map((e) => [e.scope, e]));
  return allowed.map((scope) => {
    const entry = byScope.get(scope);
    return entry ? toScopeOption(entry) : { scope };
  });
}
