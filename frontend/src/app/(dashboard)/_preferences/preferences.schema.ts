/**
 * preferences.schema.ts
 *
 * Canonical `UserPreferences` type and hardcoded defaults for all v1 preference
 * keys. This file is the sole source of truth for what constitutes a valid
 * preferences document. The backend is schema-agnostic — it stores and returns
 * the document as-is. Missing keys always fall back to the defaults defined here.
 *
 * Serialisation rule: only keys whose values differ from PREFERENCE_DEFAULTS are
 * included in PUT payloads (sparse document). Call `serializeSparse()` to produce
 * the PUT body.
 */

// ── Types ─────────────────────────────────────────────────────────────────────

export type PeriodValue = '7d' | '30d' | '90d';
export type PageSize = 10 | 25 | 50 | 100;

export interface UserPreferences {
  tables: {
    /** Default rows-per-page applied to all paginated tables. */
    defaultPageSize: PageSize;
    /** Per-table column visibility: Record<tableId, Record<columnId, visible>>. */
    columns: Record<string, Record<string, boolean>>;
  };
  analytics: {
    /** Default time window for model gateway analytics dashboards. */
    defaultPeriod: PeriodValue;
  };
  myUsage: {
    /** Default time window for the /my-usage page. */
    defaultPeriod: PeriodValue;
  };
  modelGateway: {
    /** IDs of models the user has favourited on the /models page. */
    favouriteModels: string[];
  };
  marketplace: {
    /** marketplaceSlug of the Marketplace that the user selected last on the /marketplace page. '' means none. */
    lastSelected: string;
  };
}

// ── Defaults ──────────────────────────────────────────────────────────────────

export const PREFERENCE_DEFAULTS: UserPreferences = {
  tables: {
    defaultPageSize: 25,
    columns: {},
  },
  analytics: {
    defaultPeriod: '7d',
  },
  myUsage: {
    defaultPeriod: '7d',
  },
  modelGateway: {
    favouriteModels: [],
  },
  marketplace: {
    lastSelected: '',
  },
};

// ── Helpers ───────────────────────────────────────────────────────────────────

/**
 * Merge a partial stored document with the hardcoded defaults.
 * Missing keys fall back to defaults; unknown keys in `stored` are ignored.
 */
export function mergeWithDefaults(stored: Record<string, unknown>): UserPreferences {
  const tables = (stored.tables as Partial<UserPreferences['tables']> | undefined) ?? {};
  const analytics = (stored.analytics as Partial<UserPreferences['analytics']> | undefined) ?? {};
  const myUsage = (stored.myUsage as Partial<UserPreferences['myUsage']> | undefined) ?? {};
  const modelGateway =
    (stored.modelGateway as Partial<UserPreferences['modelGateway']> | undefined) ?? {};

  const storedMarketplace = stored.marketplace as
    Partial<UserPreferences['marketplace']> | null | undefined;
  let lastSelectedMarketplace: string;
  if (storedMarketplace && typeof storedMarketplace.lastSelected === 'string') {
    // The stored document has a Marketplace name.
    lastSelectedMarketplace = storedMarketplace.lastSelected;
  } else {
    // No stored name, or a value that is not a string. Use the default.
    lastSelectedMarketplace = PREFERENCE_DEFAULTS.marketplace.lastSelected;
  }

  return {
    tables: {
      defaultPageSize: isValidPageSize(tables.defaultPageSize)
        ? tables.defaultPageSize
        : PREFERENCE_DEFAULTS.tables.defaultPageSize,
      columns:
        typeof tables.columns === 'object' && tables.columns !== null
          ? Object.fromEntries(
              Object.entries(tables.columns as Record<string, unknown>)
                .filter(([, v]) => typeof v === 'object' && v !== null)
                .map(([tableId, v]) => [
                  tableId,
                  Object.fromEntries(
                    Object.entries(v as Record<string, unknown>).filter(
                      (e): e is [string, boolean] => typeof e[1] === 'boolean',
                    ),
                  ),
                ]),
            )
          : PREFERENCE_DEFAULTS.tables.columns,
    },
    analytics: {
      defaultPeriod: isValidPeriod(analytics.defaultPeriod)
        ? analytics.defaultPeriod
        : PREFERENCE_DEFAULTS.analytics.defaultPeriod,
    },
    myUsage: {
      defaultPeriod: isValidPeriod(myUsage.defaultPeriod)
        ? myUsage.defaultPeriod
        : PREFERENCE_DEFAULTS.myUsage.defaultPeriod,
    },
    modelGateway: {
      favouriteModels: Array.isArray(modelGateway.favouriteModels)
        ? modelGateway.favouriteModels.filter((x): x is string => typeof x === 'string')
        : PREFERENCE_DEFAULTS.modelGateway.favouriteModels,
    },
    marketplace: {
      lastSelected: lastSelectedMarketplace,
    },
  };
}

/**
 * Serialise preferences sparsely: only include keys that differ from defaults.
 * The returned object is sent as the PUT body so orphaned keys cannot accumulate.
 */
export function serializeSparse(prefs: UserPreferences): Record<string, unknown> {
  const doc: Record<string, unknown> = {};

  const tables: Record<string, unknown> = {};
  if (prefs.tables.defaultPageSize !== PREFERENCE_DEFAULTS.tables.defaultPageSize) {
    tables.defaultPageSize = prefs.tables.defaultPageSize;
  }
  const nonEmptyColumns = Object.fromEntries(
    Object.entries(prefs.tables.columns).filter(([, v]) => Object.keys(v).length > 0),
  );
  if (Object.keys(nonEmptyColumns).length > 0) {
    tables.columns = nonEmptyColumns;
  }
  if (Object.keys(tables).length > 0) doc.tables = tables;

  if (prefs.analytics.defaultPeriod !== PREFERENCE_DEFAULTS.analytics.defaultPeriod) {
    doc.analytics = { defaultPeriod: prefs.analytics.defaultPeriod };
  }

  if (prefs.myUsage.defaultPeriod !== PREFERENCE_DEFAULTS.myUsage.defaultPeriod) {
    doc.myUsage = { defaultPeriod: prefs.myUsage.defaultPeriod };
  }

  const mgKeys: Record<string, unknown> = {};
  if (prefs.modelGateway.favouriteModels.length > 0) {
    mgKeys.favouriteModels = prefs.modelGateway.favouriteModels;
  }
  if (Object.keys(mgKeys).length > 0) doc.modelGateway = mgKeys;

  if (prefs.marketplace.lastSelected !== PREFERENCE_DEFAULTS.marketplace.lastSelected) {
    doc.marketplace = { lastSelected: prefs.marketplace.lastSelected };
  }

  return doc;
}

// ── Type guards ───────────────────────────────────────────────────────────────

const VALID_PERIODS = new Set<PeriodValue>(['7d', '30d', '90d']);
const VALID_PAGE_SIZES = new Set<PageSize>([10, 25, 50, 100]);

function isValidPeriod(v: unknown): v is PeriodValue {
  return typeof v === 'string' && VALID_PERIODS.has(v as PeriodValue);
}

function isValidPageSize(v: unknown): v is PageSize {
  return typeof v === 'number' && VALID_PAGE_SIZES.has(v as PageSize);
}
