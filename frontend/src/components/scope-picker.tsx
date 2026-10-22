'use client';

/** @tier 2 — kora-domain-aware: scope selection grouped by resource type and server or connection. Used by: policy grants wizard, policy roles, model-gateway token modals. */

import { useMemo, useState } from 'react';
import {
  Button,
  Card,
  CardContent,
  Checkbox,
  Collapsible,
  CollapsibleContent,
  Icon,
  Input,
  Separator,
  Stack,
  Text,
} from '@runkora/hades';
import type { ScopeOption } from './scope-option';

const NO_SCOPES: string[] = [];

/** Shortest query that filters the list and force-opens the groups holding matches. */
const MIN_SEARCH_LENGTH = 2;

export interface ScopePickerProps {
  /** Selectable scopes with vocabulary metadata. Takes precedence over `scopes` when non-empty. */
  options?: ScopeOption[];
  /** Plain scope strings (e.g. `agents:runs:read`): the fallback when no vocabulary is available. */
  scopes?: string[];
  /** Currently selected scopes. */
  selected: string[];
  /** Called with the full next selection whenever any checkbox changes. */
  onChange: (scopes: string[]) => void;
  /** Search box over scope strings and descriptions; groups with matches open while it is in use. On by default. */
  searchable?: boolean;
}

// ── Grouping ─────────────────────────────────────────────────────────────────

interface SubGroup {
  key: string;
  items: ScopeOption[];
  /** Scopes in the sub-group before any search filter. */
  fullTotal: number;
}

interface Group {
  key: string;
  /** Scopes with no server or connection level, listed straight under the group. */
  direct: ScopeOption[];
  subs: SubGroup[];
  all: ScopeOption[];
  /** Scopes in the group before any search filter. */
  fullTotal: number;
}

/**
 * Resource type, then server, connection or category (`agents` → `runs`, `types`, `usage`). Prefers the vocabulary's `resourceType` and `path[0]`
 * (`mcp` → `github`); without them, splits the scope string, treating the last segment as the
 * action (`mcp:github:issues:use` → `mcp` → `github`; `mcp:invoke` has no sub-group).
 */
function placement(o: ScopeOption): { group: string; sub: string | null } {
  const parts = o.scope.split(':');
  const group = o.resourceType || parts[0];
  if (o.path) return { group, sub: o.path[0] || null };
  return { group, sub: parts.length >= 3 ? parts[1] : null };
}

function buildGroups(options: ScopeOption[]): Group[] {
  const groups = new Map<string, { direct: ScopeOption[]; subs: Map<string, ScopeOption[]> }>();
  for (const o of options) {
    const { group, sub } = placement(o);
    let g = groups.get(group);
    if (!g) {
      g = { direct: [], subs: new Map() };
      groups.set(group, g);
    }
    if (sub === null) {
      g.direct.push(o);
    } else {
      const items = g.subs.get(sub);
      if (items) items.push(o);
      else g.subs.set(sub, [o]);
    }
  }
  return Array.from(groups.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, g]) => {
      // A sub-group of one scope is noise (`platform:grants:read`, `trigger:<id>:fire`): list it
      // straight under the group instead.
      const direct = [...g.direct];
      const subs: SubGroup[] = [];
      const entries = Array.from(g.subs.entries()).sort(([a], [b]) => a.localeCompare(b));
      for (const [subKey, items] of entries) {
        if (items.length >= 2) subs.push({ key: subKey, items, fullTotal: items.length });
        else direct.push(...items);
      }
      const all = [...direct, ...subs.flatMap((sub) => sub.items)];
      return { key, direct, subs, all, fullTotal: all.length };
    });
}

/**
 * Keeps the scopes matching `query`, dropping groups and sub-groups left empty. The tree shape is
 * decided before filtering, so a sub-group does not turn into loose rows as the filter narrows.
 */
function filterGroups(groups: Group[], query: string): Group[] {
  const out: Group[] = [];
  for (const g of groups) {
    const direct = g.direct.filter((o) => matches(o, query));
    const subs = g.subs
      .map((sub) => ({ ...sub, items: sub.items.filter((o) => matches(o, query)) }))
      .filter((sub) => sub.items.length > 0);
    const all = [...direct, ...subs.flatMap((sub) => sub.items)];
    if (all.length > 0) out.push({ ...g, direct, subs, all });
  }
  return out;
}

function matches(o: ScopeOption, query: string): boolean {
  return (
    o.scope.toLowerCase().includes(query) || (o.description ?? '').toLowerCase().includes(query)
  );
}

// ── Component ────────────────────────────────────────────────────────────────

/**
 * ScopePicker
 *
 * Tri-state scope selector for vocabularies of several hundred scopes. Scopes are grouped by
 * resource type and then by server or connection; each level has a select-all checkbox, shows its
 * selected count, and renders its children only while open. Rows show the scope's description.
 * A summary line counts the selection and clears it.
 */
export function ScopePicker({
  options,
  scopes = NO_SCOPES,
  selected,
  onChange,
  searchable = true,
}: ScopePickerProps) {
  const [search, setSearch] = useState('');
  const trimmed = searchable ? search.trim().toLowerCase() : '';
  // One character matches too much to be a search: show everything and leave groups as they are.
  const query = trimmed.length >= MIN_SEARCH_LENGTH ? trimmed : '';
  const tooShort = trimmed !== '' && query === '';

  const all = useMemo<ScopeOption[]>(() => {
    const source = options && options.length > 0 ? options : scopes.map((scope) => ({ scope }));
    const seen = new Set<string>();
    return source.filter((o) => !seen.has(o.scope) && seen.add(o.scope));
  }, [options, scopes]);

  const selectedSet = useMemo(() => new Set(selected), [selected]);

  const tree = useMemo(() => buildGroups(all), [all]);
  const groups = useMemo(() => (query ? filterGroups(tree, query) : tree), [tree, query]);

  const summary = useMemo(() => {
    const groupOf = new Map(all.map((o) => [o.scope, placement(o).group]));
    const counts = new Map<string, number>();
    for (const s of selectedSet) {
      const g = groupOf.get(s) ?? s.split(':')[0];
      counts.set(g, (counts.get(g) ?? 0) + 1);
    }
    return Array.from(counts.entries())
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([g, n]) => `${g} ${n}`)
      .join(' · ');
  }, [all, selectedSet]);

  const toggle = (toToggle: ScopeOption[], on: boolean) => {
    const next = new Set(selectedSet);
    for (const o of toToggle) {
      if (on) next.add(o.scope);
      else next.delete(o.scope);
    }
    onChange([...next]);
  };

  return (
    <Stack gap="cozy">
      {searchable && (
        <Input
          type="text"
          placeholder="Search scopes or descriptions..."
          aria-label="Filter scopes"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      )}
      {tooShort && (
        <Text size="caption" color="subtle">
          Type {MIN_SEARCH_LENGTH} or more characters to filter
        </Text>
      )}
      {/* Always rendered so the list does not jump on the first and last selection. */}
      <Stack direction="horizontal" align="center" gap="cozy">
        <Stack grow minWidth="none" gap="none">
          <Text as="span" size="caption" weight="heavy">
            {selectedSet.size} {selectedSet.size === 1 ? 'scope' : 'scopes'} selected
          </Text>
          <Text as="span" size="micro" color="subtle" truncate>
            {summary || 'Choose scopes below'}
          </Text>
        </Stack>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={selectedSet.size === 0}
          onClick={() => onChange([])}
        >
          Clear all
        </Button>
      </Stack>
      {all.length === 0 ? (
        <Stack paddingBlock="comfortable" align="center">
          <Text size="caption" color="muted">
            No scopes available
          </Text>
        </Stack>
      ) : groups.length === 0 && query ? (
        <Stack paddingBlock="comfortable" align="center">
          <Text size="caption" color="muted">
            No scopes match your filter
          </Text>
        </Stack>
      ) : (
        groups.map((group) => (
          <ScopeGroupCard
            key={group.key}
            group={group}
            selectedSet={selectedSet}
            forceOpen={query !== ''}
            filtered={query !== ''}
            onToggle={toggle}
          />
        ))
      )}
    </Stack>
  );
}

/** `2/5 scopes`; while a search filters the list, `3 of 5 shown · 2 selected` so it never reads as the whole group. */
function counts(selected: number, shown: number, full: number, filtered: boolean): string {
  return filtered
    ? `${shown} of ${full} shown · ${selected} selected`
    : `${selected}/${shown} scopes`;
}

function triState(selected: number, total: number) {
  const allSelected = total > 0 && selected === total;
  return { allSelected, some: selected > 0 && !allSelected };
}

/**
 * Open state for a group or sub-group. Until the user opens or closes it, it follows the
 * selection: one that holds a scope is open, so existing grants are visible even when the saved
 * scopes load after mount. A search force-opens it without touching that state.
 *
 * `pin` is called when the user changes the picker's own checkboxes: it freezes the open state
 * they were made in, so select-all on a closed group does not mount its rows and clearing the last
 * scope does not collapse the group under the cursor. It does nothing while a search force-opens
 * the group, because `selectedCount` then counts only the matches and would pin a wrong state.
 */
function useFollowedOpen(selectedCount: number, forceOpen: boolean) {
  const [manualOpen, setManualOpen] = useState<boolean | null>(null);
  const followed = manualOpen ?? selectedCount > 0;
  const pin = () => {
    if (!forceOpen && manualOpen === null) setManualOpen(followed);
  };
  // Clicking a header row opens or closes it. A search holds the group open, so the click waits.
  const toggleOpen = () => {
    if (!forceOpen) setManualOpen(!followed);
  };
  return { open: forceOpen || followed, toggleOpen, pin };
}

/**
 * Header rows open and close on click. The select-all checkbox sits inside the row, so its clicks
 * (mouse and keyboard, which browsers also report as clicks) stop here and only change the
 * selection.
 */
const keepRowClosed = (e: React.MouseEvent) => e.stopPropagation();

function countSelected(items: ScopeOption[], selectedSet: Set<string>): number {
  let n = 0;
  for (const o of items) if (selectedSet.has(o.scope)) n++;
  return n;
}

function ScopeGroupCard({
  group,
  selectedSet,
  forceOpen,
  filtered,
  onToggle,
}: {
  group: Group;
  selectedSet: Set<string>;
  forceOpen: boolean;
  filtered: boolean;
  onToggle: (scopes: ScopeOption[], on: boolean) => void;
}) {
  const total = group.all.length;
  const selectedInGroup = countSelected(group.all, selectedSet);
  const any = selectedInGroup > 0;
  const { allSelected, some } = triState(selectedInGroup, total);
  const { open, toggleOpen, pin } = useFollowedOpen(selectedInGroup, forceOpen);

  const toggle = (scopes: ScopeOption[], on: boolean) => {
    pin();
    onToggle(scopes, on);
  };

  return (
    <Card selected={any} selectedVariant="outline">
      <Collapsible open={open}>
        <CardContent flush>
          <Stack
            direction="horizontal"
            align="center"
            gap="cozy"
            padding="cozy"
            onClick={toggleOpen}
          >
            <Stack onClick={keepRowClosed}>
              <Checkbox
                size="md"
                checked={allSelected}
                indeterminate={some}
                onChange={(checked) => toggle(group.all, checked)}
                aria-label={`Select all ${group.key} scopes`}
              />
            </Stack>
            <Icon
              name="shield"
              size="sm"
              color={any ? 'accent' : 'muted'}
              container
              containerVariant={any ? 'accent' : 'surface-subtle'}
              label=""
            />
            <Stack grow gap="tight">
              <Text as="span" size="supporting" weight="heavy">
                {group.key}
              </Text>
              <Text as="span" size="micro" mono color="subtle">
                {counts(selectedInGroup, total, group.fullTotal, filtered)}
              </Text>
            </Stack>
            <Text as="span" size="micro" weight="heavy" color={allSelected ? 'accent' : 'muted'}>
              {any && !filtered
                ? allSelected
                  ? `All ${total} selected`
                  : `${selectedInGroup} of ${total} selected`
                : ''}
            </Text>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`${group.key} scopes`}
              aria-expanded={open}
            >
              <Icon name="chevron-down" rotate={open ? 180 : 0} />
            </Button>
          </Stack>
          {open && (
            <CollapsibleContent>
              {group.direct.map((o) => (
                <ScopeRow key={o.scope} option={o} selectedSet={selectedSet} onToggle={toggle} />
              ))}
              {group.subs.map((sub) => (
                <ScopeSubGroup
                  key={sub.key}
                  groupKey={group.key}
                  sub={sub}
                  selectedSet={selectedSet}
                  forceOpen={forceOpen}
                  filtered={filtered}
                  onToggle={toggle}
                />
              ))}
            </CollapsibleContent>
          )}
        </CardContent>
      </Collapsible>
    </Card>
  );
}

function ScopeSubGroup({
  groupKey,
  sub,
  selectedSet,
  forceOpen,
  filtered,
  onToggle,
}: {
  groupKey: string;
  sub: SubGroup;
  selectedSet: Set<string>;
  forceOpen: boolean;
  filtered: boolean;
  onToggle: (scopes: ScopeOption[], on: boolean) => void;
}) {
  const total = sub.items.length;
  const selectedInSub = countSelected(sub.items, selectedSet);
  const { allSelected, some } = triState(selectedInSub, total);
  // With nothing selected sub-groups start closed, so a group of hundreds of scopes is a short
  // list of servers.
  const { open, toggleOpen, pin } = useFollowedOpen(selectedInSub, forceOpen);
  const label = `${groupKey} ${sub.key}`;

  const toggle = (scopes: ScopeOption[], on: boolean) => {
    pin();
    onToggle(scopes, on);
  };

  return (
    <Stack gap="none">
      <Separator />
      <Collapsible open={open}>
        <Stack
          direction="horizontal"
          align="center"
          gap="cozy"
          padding="cozy"
          paddingInline="relaxed"
          onClick={toggleOpen}
        >
          <Stack onClick={keepRowClosed}>
            <Checkbox
              checked={allSelected}
              indeterminate={some}
              onChange={(checked) => toggle(sub.items, checked)}
              aria-label={`Select all ${label} scopes`}
            />
          </Stack>
          <Stack grow gap="tight" minWidth="none">
            <Text as="span" size="caption" weight="heavy" truncate>
              {sub.key}
            </Text>
            <Text as="span" size="micro" mono color="subtle">
              {counts(selectedInSub, total, sub.fullTotal, filtered)}
            </Text>
          </Stack>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={`${label} scopes`}
            aria-expanded={open}
          >
            <Icon name="chevron-down" rotate={open ? 180 : 0} />
          </Button>
        </Stack>
        {open && (
          <CollapsibleContent>
            {sub.items.map((o) => (
              <ScopeRow
                key={o.scope}
                option={o}
                selectedSet={selectedSet}
                onToggle={toggle}
                nested
              />
            ))}
          </CollapsibleContent>
        )}
      </Collapsible>
    </Stack>
  );
}

function ScopeRow({
  option,
  selectedSet,
  onToggle,
  nested = false,
}: {
  option: ScopeOption;
  selectedSet: Set<string>;
  onToggle: (scopes: ScopeOption[], on: boolean) => void;
  nested?: boolean;
}) {
  return (
    <Stack gap="none">
      <Separator />
      <Stack
        direction="horizontal"
        align="center"
        gap="cozy"
        padding="cozy"
        paddingInline={nested ? 'loose' : 'relaxed'}
      >
        <Checkbox
          checked={selectedSet.has(option.scope)}
          onChange={(checked) => onToggle([option], checked)}
          aria-label={`Select ${option.scope}`}
        />
        <Stack grow minWidth="none" gap="tight">
          <Text as="span" size="caption" mono weight="heavy" truncate>
            {option.scope}
          </Text>
          {option.description && (
            <Text as="span" size="micro" color="subtle">
              {option.description}
            </Text>
          )}
        </Stack>
      </Stack>
    </Stack>
  );
}
