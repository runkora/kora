'use client';

/**
 * @tier 3 — proposed, run-page-scoped. The run page's right rail, from data the API records today:
 * the run's messages and their senders, its sources, usage and runtime.
 */

import {
  Anchor,
  Avatar,
  Icon,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Stack,
  Text,
  type IconName,
} from '@runkora/hades';
import { HARNESS_LABEL, type RunView } from '../_lib/run-view-proposed';

const LINE = '1px solid var(--color-border-subtle)';

const k = (n: number) => (n >= 1000 ? `${(n / 1000).toFixed(1)}K` : String(n));

const SOURCE_ICON: Record<string, IconName> = {
  trigger: 'github',
  pull_request: 'github',
  repository: 'folder',
  branch: 'git-branch',
};
const PROVIDER_ICON: Record<string, IconName> = {
  github: 'github',
  slack: 'hash',
  alertmanager: 'prometheus',
};

function Label({ children, aside }: { children: React.ReactNode; aside?: string }) {
  return (
    <Stack direction="horizontal" justify="between" align="center">
      <Text size="caption" color="subtle">
        {children}
      </Text>
      {aside && (
        <Text size="caption" color="subtle" tabular>
          {aside}
        </Text>
      )}
    </Stack>
  );
}

// ── People: everyone who started or steered the run ─────────────────────────

interface Participant {
  name: string;
  kind: 'person' | 'trigger';
  /** How they took part: "Started it on Slack", "2 messages", "Commented on GitHub". */
  detail: string;
  first: number;
}

const PROVIDER_NAME: Record<string, string> = {
  github: 'GitHub',
  slack: 'Slack',
  alertmanager: 'Alertmanager',
};

/**
 * Everyone involved, from the run's messages: people by their recorded sender, people outside
 * the platform by the event a trigger carried (a GitHub login), and triggers with no person behind them.
 */
function participants(run: RunView): Participant[] {
  const by = new Map<
    string,
    { kind: Participant['kind']; first: number; messages: number; started: boolean; via?: string }
  >();
  run.entries.forEach((e, i) => {
    if (e.type !== 'message') return;
    const actor = e.event?.actor;
    const isPerson = e.author.kind === 'person' || actor?.kind === 'person';
    const name =
      e.author.kind === 'person'
        ? e.author.name
        : actor?.kind === 'person'
          ? actor.name
          : e.author.name;
    const prev = by.get(name);
    // How they reached the run: by hand, or through a provider's trigger.
    const via =
      e.author.kind !== 'trigger'
        ? undefined
        : e.author.via === 'manual'
          ? 'by hand'
          : `via ${(e.author.provider && PROVIDER_NAME[e.author.provider]) || e.author.name}`;
    by.set(name, {
      kind: isPerson ? 'person' : 'trigger',
      first: prev?.first ?? e.at,
      messages: (prev?.messages ?? 0) + 1,
      started: prev?.started ?? i === 0,
      via: prev?.via ?? via,
    });
  });
  return [...by].map(([name, p]) => {
    const count = `${p.messages} ${p.messages === 1 ? 'message' : 'messages'}`;
    const parts = p.started
      ? [p.via ? `Started it ${p.via}` : 'Started it', p.messages > 1 ? count : '']
      : [count, p.via ?? ''];
    return { name, kind: p.kind, detail: parts.filter(Boolean).join(' · '), first: p.first };
  });
}

function Mark({ p, size = '2xs' }: { p: Participant; size?: '2xs' | 'xs' }) {
  return p.kind === 'person' ? (
    <Avatar name={p.name} size={size} />
  ) : (
    <Stack
      align="center"
      justify="center"
      style={{
        width: 18,
        height: 18,
        borderRadius: 999,
        background: 'var(--color-surface-overlay)',
        flexShrink: 0,
      }}
    >
      <Icon name="zap" size="2xs" color="muted" label="" />
    </Stack>
  );
}

/** People as an overlapping row of marks, names underneath. */
function PeopleStack({ run }: { run: RunView }) {
  const ps = participants(run);
  if (!ps.length) return null;
  const people = ps.filter((p) => p.kind === 'person').length;
  const triggers = ps.length - people;
  return (
    <Stack gap="cozy">
      <Label>People</Label>
      <Stack direction="horizontal" align="center" gap="snug">
        <Stack direction="horizontal">
          {ps.map((p, i) => (
            <Stack
              key={p.name}
              style={{
                marginLeft: i ? -6 : 0,
                borderRadius: 999,
                boxShadow: '0 0 0 2px var(--color-background)',
              }}
              title={`${p.name}: ${p.detail}`}
            >
              <Mark p={p} size="xs" />
            </Stack>
          ))}
        </Stack>
        <Text size="caption" color="muted">
          {[
            people && `${people} ${people === 1 ? 'person' : 'people'}`,
            triggers && `${triggers} ${triggers === 1 ? 'trigger' : 'triggers'}`,
          ]
            .filter(Boolean)
            .join(' · ')}
        </Text>
      </Stack>
      <Text size="caption" color="subtle">
        {ps.map((p) => p.name).join(', ')}
      </Text>
    </Stack>
  );
}

// ── Links ────────────────────────────────────────────────────────────────────

function Links({ run }: { run: RunView }) {
  if (!run.sources.length) return null;
  return (
    <Stack gap="cozy">
      <Label>Links</Label>
      {run.sources.map((src) => (
        <Stack key={src.url} direction="horizontal" align="center" gap="snug">
          <Icon
            name={
              (src.kind === 'trigger' && src.provider && PROVIDER_ICON[src.provider]) ||
              SOURCE_ICON[src.kind] ||
              'external-link'
            }
            size="2xs"
            color="subtle"
            label=""
          />
          <Text size="supporting" truncate>
            <Anchor href={src.url} target="_blank" rel="noopener noreferrer">
              {src.label.replace(/^branch /, '')}
            </Anchor>
          </Text>
        </Stack>
      ))}
    </Stack>
  );
}

// ── Runs on ──────────────────────────────────────────────────────────────────

/**
 * A connector's access in one line: what it can change, then the rest behind a link that opens
 * every resource and its verbs, the way the header's "+2" opens the rest of the links.
 */
function Access({ actions }: { actions: string[] }) {
  const by = new Map<string, Set<string>>();
  for (const a of actions) {
    const [res, verb] = a.split(':');
    by.set(res, (by.get(res) ?? new Set()).add(verb));
  }
  const all = [...by].sort(([, a], [, b]) => Number(b.has('write')) - Number(a.has('write')));
  const writes = all.filter(([, v]) => v.has('write')).map(([r]) => r);
  const reads = all.length - writes.length;
  return (
    <Text size="caption" color="muted">
      {writes.length ? `Writes ${writes.join(', ')}` : 'Read only'}
      {reads > 0 && (
        <>
          {' · '}
          <Popover>
            <Anchor asChild size="inherit">
              <PopoverTrigger style={{ textDecoration: 'underline', textUnderlineOffset: 4 }}>
                reads {reads} more
              </PopoverTrigger>
            </Anchor>
            <PopoverContent align="start">
              <Stack gap="tight" style={{ minWidth: 200, padding: '4px 6px' }}>
                {all.map(([res, verbs]) => (
                  <Stack key={res} direction="horizontal" justify="between" gap="cozy">
                    <Text size="supporting">{res}</Text>
                    <Text size="supporting" color={verbs.has('write') ? 'foreground' : 'subtle'}>
                      {verbs.has('write') ? 'read, write' : 'read'}
                    </Text>
                  </Stack>
                ))}
              </Stack>
            </PopoverContent>
          </Popover>
        </>
      )}
    </Text>
  );
}

/** An icon centred on the first line of the text beside it, whatever that text's size. */
function LeadIcon({ name }: { name: IconName }) {
  return (
    <Stack align="center" justify="center" style={{ height: 20, width: 14, flexShrink: 0 }}>
      <Icon name={name} size="2xs" color="subtle" label="" />
    </Stack>
  );
}

function RunsOn({ run }: { run: RunView }) {
  const r = run.record;
  const [provider, model] = run.model.includes('/') ? run.model.split('/') : ['', run.model];
  const image = r?.sandbox?.image.split('/').pop() ?? '';
  const [imageName, digest] = image.split('@sha256:');
  return (
    <Stack gap="cozy">
      <Label>Runs on</Label>
      <Stack direction="horizontal" align="start" gap="snug">
        <LeadIcon name="square-terminal" />
        <Stack grow minWidth="none" gap="none">
          <Text size="supporting">{HARNESS_LABEL[run.harness]}</Text>
          <Text size="caption" color="subtle" truncate>
            {model}
            {provider && ` · ${provider}`}
          </Text>
        </Stack>
      </Stack>
      {r?.sandbox && (
        <Stack direction="horizontal" align="start" gap="snug">
          <LeadIcon name="box" />
          <Stack grow minWidth="none" gap="none">
            <Text size="supporting">{imageName}</Text>
            <Text size="caption" color="subtle" truncate>
              {r.sandbox.environment} environment
              {digest && (
                <>
                  {' · '}
                  <Text as="span" size="caption" color="subtle" mono>
                    {digest.slice(0, 7)}
                  </Text>
                </>
              )}
            </Text>
          </Stack>
        </Stack>
      )}
      {r?.connectors.map((c) => (
        <Stack key={c.connection} direction="horizontal" align="start" gap="snug">
          <LeadIcon name={c.connection.includes('github') ? 'github' : 'key'} />
          <Stack grow minWidth="none" gap="none">
            <Text size="supporting">{c.connection}</Text>
            <Access actions={c.actions} />
          </Stack>
        </Stack>
      ))}
    </Stack>
  );
}

function Spend({ run }: { run: RunView }) {
  const r = run.record;
  if (!r) return null;
  return (
    <Stack gap="tight">
      <Label>Spend</Label>
      <Text size="body" weight="emphasis" tabular>
        ${run.spend.usd.toFixed(3)}
      </Text>
      <Text size="caption" color="muted" tabular>
        {r.requests} model calls · {k(r.inputTokens)} in · {k(r.outputTokens)} out
      </Text>
    </Stack>
  );
}

/**
 * The rail: everyone involved, links, runtime and spend. As a sheet it
 * fills the panel it opens in instead of standing beside the thread.
 */
export function RunRail({ run, sheet }: { run: RunView; sheet?: boolean }) {
  return (
    <Stack
      gap="loose"
      style={{
        width: sheet ? '100%' : 320,
        flexShrink: 0,
        padding: sheet ? '8px 20px 28px' : '24px 22px',
        borderLeft: sheet ? undefined : LINE,
        overflowY: 'auto',
        scrollbarWidth: 'none',
      }}
    >
      <PeopleStack run={run} />
      <Links run={run} />
      <RunsOn run={run} />
      <Spend run={run} />
    </Stack>
  );
}
