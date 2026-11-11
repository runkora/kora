'use client';

/**
 * @tier 3 — run-page-scoped. A tool call in the thread: a quiet block with its verb, target and a
 * preview of what came back, opening to the whole call. Clean reads and searches fold into a line.
 */

import { useState } from 'react';
import {
  Button,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Icon,
  Stack,
  Text,
  type IconName,
} from '@runkora/hades';
import { commandParts, formatSpan, quietSummary, type Step, type StepKind } from '../_lib/run-view';
import { HAIR, NOWRAP } from '../_lib/run-styles';

const KIND_ICON: Record<StepKind, IconName> = {
  read: 'file-text',
  edit: 'edit',
  execute: 'square-terminal',
  search: 'search',
  fetch: 'globe',
  think: 'sparkles',
  mcp: 'network',
  subagent: 'bot',
  sandbox: 'box',
  other: 'wrench',
};

function shortName(path: string): { name: string; dir: string } {
  const i = path.lastIndexOf('/');
  return i < 0 ? { name: path, dir: '' } : { name: path.slice(i + 1), dir: path.slice(0, i) };
}

const kb = (bytes: number) => `${Math.round(bytes / 1024)} KB`;

/** Everything a call recorded: the whole command, its full output and each file's diff lines. */
function CallDetail({ step }: { step: Step }) {
  return (
    <Stack
      gap="snug"
      overflowY="auto"
      style={{ borderTop: HAIR, padding: '8px 14px 10px', maxHeight: 480, wordBreak: 'break-word' }}
    >
      {step.command && (
        <Text size="caption" mono preserveWhitespace>
          {step.command}
        </Text>
      )}
      {step.diffs?.map((d, i) => (
        <Stack key={`${d.path}-${i}`} gap="none">
          <Text size="caption" weight="strong">
            {d.path}
          </Text>
          {d.lines?.map((l, k) => (
            <Text
              key={k}
              size="caption"
              mono
              preserveWhitespace
              color={
                l.kind === 'add' ? 'status-healthy' : l.kind === 'del' ? 'status-error' : 'subtle'
              }
            >
              {l.kind === 'add' ? '+ ' : l.kind === 'del' ? '− ' : '  '}
              {l.text}
            </Text>
          ))}
        </Stack>
      ))}
      {step.output && (
        <Text
          size="caption"
          mono
          preserveWhitespace
          color={step.status === 'failed' ? 'status-error' : 'subtle'}
        >
          {step.output}
        </Text>
      )}
      {step.truncated && (
        <Text size="caption" color="subtle">
          Recorded result cut at 64 KB
          {step.truncated.originalBytes ? ` of ${kb(step.truncated.originalBytes)}` : ''}.
        </Text>
      )}
    </Stack>
  );
}

/** A tool call as a quiet block: bold verb, monospace target, then a fading preview of what came back. */
export function CallBlock({ step, nested }: { step: Step; nested?: Step[] }) {
  const failed = step.status === 'failed' || step.status === 'denied';
  const running = step.status === 'running';
  const preview =
    step.errorLine ?? step.policy?.reason ?? step.output?.split('\n').slice(0, 4).join('\n');
  const parts = commandParts(step);
  const [open, setOpen] = useState(false);
  const full =
    (parts.length > 1 || !!step.output || !!step.diffs?.some((d) => d.lines?.length)) && !running;
  return (
    <Stack
      overflow="hidden"
      style={{
        border: `1px solid ${failed ? 'color-mix(in oklch, var(--color-status-error) 30%, transparent)' : 'color-mix(in oklch, var(--color-foreground) 8%, transparent)'}`,
        background: 'color-mix(in oklch, var(--color-foreground) 2.5%, transparent)',
        borderRadius: 8,
      }}
    >
      <Stack direction="horizontal" align="center" gap="snug" style={{ padding: '9px 14px' }}>
        <Icon
          name={running ? 'loader' : KIND_ICON[step.kind]}
          spin={running}
          size="2xs"
          color={failed ? 'status-error' : running ? 'accent' : 'subtle'}
          label={failed ? (step.status === 'denied' ? 'Denied' : 'Failed') : ''}
        />
        {step.verb && (
          <Text size="supporting" weight="strong" style={NOWRAP}>
            {step.verb}
          </Text>
        )}
        <Text size="supporting" mono color="muted" truncate style={{ flex: 1, minWidth: 0 }}>
          {parts[0]}
        </Text>
        {parts.length > 1 && (
          <Text size="caption" color="subtle" style={NOWRAP}>
            +{parts.length - 1} more
          </Text>
        )}
        {step.exitCode !== undefined && step.exitCode !== 0 && (
          <Text size="caption" mono color="status-error" style={NOWRAP}>
            exit {step.exitCode}
          </Text>
        )}
        <Text size="caption" color="subtle" tabular style={NOWRAP}>
          {running ? 'now' : step.durationMs !== null ? formatSpan(step.durationMs) : ''}
        </Text>
        {full && (
          <Button
            size="icon-sm"
            variant="ghost"
            aria-expanded={open}
            aria-label={open ? 'Hide the full call' : 'Show the full call'}
            onClick={() => setOpen(!open)}
          >
            <Icon name={open ? 'chevron-down' : 'chevron-right'} size="2xs" label="" />
          </Button>
        )}
      </Stack>
      {open && <CallDetail step={step} />}
      {!open && step.diffs && step.diffs.length > 0 && (
        <Stack style={{ borderTop: HAIR }}>
          {step.diffs.map((d, i) => {
            const { name, dir } = shortName(d.path);
            return (
              <Stack
                key={`${d.path}-${i}`}
                direction="horizontal"
                align="center"
                gap="snug"
                style={{ padding: '7px 14px' }}
              >
                <Text size="supporting">{name}</Text>
                <Text size="caption" color="subtle" truncate style={{ flex: 1, minWidth: 0 }}>
                  {dir}
                </Text>
                {d.lines !== null && (
                  <>
                    <Text size="caption" mono color="status-healthy">
                      +{d.added}
                    </Text>
                    <Text size="caption" mono color="status-error">
                      −{d.removed}
                    </Text>
                  </>
                )}
              </Stack>
            );
          })}
        </Stack>
      )}
      {!open && preview && !step.diffs?.length && (
        <Stack
          overflow="hidden"
          style={{
            borderTop: HAIR,
            padding: '8px 14px 10px',
            maxHeight: 92,
            maskImage: 'linear-gradient(to bottom, black 55%, transparent)',
          }}
        >
          <Text
            size="caption"
            mono
            preserveWhitespace
            color={failed ? 'status-error' : 'subtle'}
            style={{ lineHeight: 1.6 }}
          >
            {preview}
          </Text>
        </Stack>
      )}
      {nested && nested.length > 0 && (
        <Stack style={{ borderTop: HAIR, padding: '8px 14px 10px' }} gap="tight">
          {nested.map((c) => (
            <Stack key={c.id} direction="horizontal" align="center" gap="snug">
              <Icon
                name={c.status === 'running' ? 'loader' : KIND_ICON[c.kind]}
                spin={c.status === 'running'}
                size="2xs"
                color={c.status === 'running' ? 'accent' : 'subtle'}
                label=""
              />
              <Text size="caption" color="muted" style={NOWRAP}>
                {c.verb}
              </Text>
              <Text size="caption" mono color="subtle" truncate>
                {c.target}
              </Text>
            </Stack>
          ))}
        </Stack>
      )}
    </Stack>
  );
}

/** Reads and searches the reader skims past: one muted line that opens to the calls. */
export function QuietLine({ steps }: { steps: Step[] }) {
  const [open, setOpen] = useState(false);
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger asChild>
        <Button size="sm" variant="ghost" style={{ paddingInline: 2, height: 24 }}>
          <Icon name="file-text" size="2xs" color="subtle" label="" />
          <Text as="span" size="supporting" color="subtle">
            {quietSummary(steps)}
          </Text>
          <Icon name={open ? 'chevron-down' : 'chevron-right'} size="2xs" color="subtle" label="" />
        </Button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <Stack gap="snug" style={{ paddingTop: 8 }}>
          {steps.map((s) => (
            <CallBlock key={s.id} step={s} />
          ))}
        </Stack>
      </CollapsibleContent>
    </Collapsible>
  );
}
