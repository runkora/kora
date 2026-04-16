/**
 * Stable public barrel for @orbit/utils.
 *
 * All app code should import from '@orbit/utils' rather than from a specific
 * sub-module. This lets us reorganise within utils/ without touching call sites.
 *
 *   import { fmtDate, fmtCost } from '@orbit/utils';
 */
export * from './format';
