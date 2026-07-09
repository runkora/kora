// ── Profile feature view types ────────────────────────────────────────────────
//
// Feature-local types for the profile page. Backend wire types are imported
// from their canonical locations and re-exported for convenience.

export type { ScopesExplainResponse, RoleDetail, GrantDetail } from '@orbit/shared/api';
export type { PAT } from '@orbit/shared/api';

/**
 * Matches ADR-0042 GET /identity/users/{id} response shape.
 * Kept here for use by profile components — the server service defines
 * its own internal copy for the fetch boundary.
 */
export interface UserProfile {
  id: string;
  email: string;
  display_name: string;
}
