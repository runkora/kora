import { scopeOptionsFor } from '@runkora/components/scope-option';
import type { ScopeOption } from '@runkora/components/scope-option';
import { hasScopeCovering } from '@runkora/lib/permissions';

/** The scope that gates the policy vocabulary endpoint. */
export const VOCABULARY_READ_SCOPE = 'platform:grants:read';

interface VocabularyResult {
  ok: boolean;
  data?: { scopes?: Parameters<typeof scopeOptionsFor>[1] | null };
}

/**
 * Describes the effective scopes with vocabulary descriptions and grouping. The vocabulary is
 * fetched only when the caller may read it, so a token-only user never triggers a 403. When the
 * fetch is skipped or fails, every scope stays selectable as a bare string.
 */
export async function loadScopeOptions(
  effectiveScopes: string[],
  fetchVocabulary: () => Promise<VocabularyResult>,
): Promise<ScopeOption[]> {
  if (!hasScopeCovering(effectiveScopes, VOCABULARY_READ_SCOPE)) {
    return scopeOptionsFor(effectiveScopes, []);
  }
  const vocab = await fetchVocabulary();
  return scopeOptionsFor(effectiveScopes, vocab.ok ? (vocab.data?.scopes ?? []) : []);
}
