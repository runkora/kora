'use client';

/**
 * @tier 3 — run-page-scoped. The run page's right rail: the people involved, the run's links,
 * what it runs on and what it spent.
 */

import {
  Anchor,
  Avatar,
  Button,
  Icon,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Stack,
  Text,
  type IconName,
} from '@runkora/hades';
import { isSafeUrl } from '@runkora/lib/safe-url';
import { sourceIcon } from '@runkora/components';
import {
  harnessLabel,
  participantsOf,
  tokensLine,
  type Participant,
  type RunView,
} from '../_lib/run-view';
import { branchName, LINE } from '../_lib/run-styles';
import { SystemMark } from './trigger-event';

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

/** A participant's mark: a person's avatar, a bot for an agent, a bolt for a trigger. */
function Mark({ p }: { p: Participant }) {
  if (p.kind === 'person') return <Avatar name={p.name} size="xs" />;
  return <SystemMark icon={p.kind === 'agent' ? 'bot' : 'zap'} size={24} />;
}

const plural = (n: number, one: string, many: string) => (n ? `${n} ${n === 1 ? one : many}` : '');

/** People as an overlapping row of marks that opens to how each took part. */
function PeopleStack({ run }: { run: RunView }) {
  const ps = participantsOf(run);
  if (!ps.length) return null;
  const count = (k: Participant['kind']) => ps.filter((p) => p.kind === k).length;
  return (
    <Stack gap="cozy">
      <Label>People</Label>
      <Popover>
        <PopoverTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            aria-label="Who took part"
            style={{ justifyContent: 'flex-start', paddingInline: 0, height: 'auto' }}
          >
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
                  >
                    <Mark p={p} />
                  </Stack>
                ))}
              </Stack>
              <Text as="span" size="caption" color="muted">
                {[
                  plural(count('person'), 'person', 'people'),
                  plural(count('agent'), 'agent', 'agents'),
                  plural(count('trigger'), 'trigger', 'triggers'),
                ]
                  .filter(Boolean)
                  .join(' · ')}
              </Text>
            </Stack>
          </Button>
        </PopoverTrigger>
        <PopoverContent align="start">
          <Stack gap="snug" padding="tight">
            {ps.map((p) => (
              <Stack key={p.name} direction="horizontal" align="center" gap="snug">
                <Mark p={p} />
                <Stack gap="none" minWidth="none">
                  <Text size="supporting">{p.name}</Text>
                  <Text size="caption" color="subtle">
                    {p.detail}
                  </Text>
                </Stack>
              </Stack>
            ))}
          </Stack>
        </PopoverContent>
      </Popover>
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
          <Icon name={sourceIcon(src)} size="2xs" color="subtle" label="" />
          <Text size="supporting" truncate>
            {/* A source URL is recorded data; only an http(s) one becomes a link. */}
            {isSafeUrl(src.url) ? (
              <Anchor href={src.url} target="_blank" rel="noopener noreferrer">
                {branchName(src.label)}
              </Anchor>
            ) : (
              branchName(src.label)
            )}
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
    <Stack align="center" justify="center" shrink style={{ height: 20, width: 14 }}>
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
      {(run.harness || model) && (
        <Stack direction="horizontal" align="start" gap="snug">
          <LeadIcon name="square-terminal" />
          <Stack grow minWidth="none" gap="none">
            <Text size="supporting">{harnessLabel(run.harness)}</Text>
            <Text size="caption" color="subtle" truncate>
              {model}
              {provider && ` · ${provider}`}
            </Text>
          </Stack>
        </Stack>
      )}
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
        {r.requests} model calls
      </Text>
      <Text size="caption" color="muted" tabular>
        {tokensLine(run)}
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
      shrink
      overflowY="auto"
      style={{
        width: sheet ? '100%' : 320,
        padding: sheet ? '8px 20px 28px' : '24px 22px',
        borderLeft: sheet ? undefined : LINE,
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
