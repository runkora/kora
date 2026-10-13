'use client';

/** @tier 2 — Orbit-domain-aware: resolved effective access (hosts, connectors, environments). Used by: agents builder, agents configuration, environments.
 * Shows the hosts an agent can reach, colour-coded by source; hovering a source highlights its hosts. */

import { useMemo, useState } from 'react';
import {
  Alert,
  Badge,
  Card,
  CardContent,
  Code,
  Icon,
  OrbitalLoader,
  Stack,
  StatusDot,
  Text,
} from '@orbit/hades';
import type { BadgeProps, StatusDotProps } from '@orbit/hades';
import type { EffectiveAccess } from '@orbit/shared/api';

// ── Provenance model ────────────────────────────────────────────────────────────
// Three sources contribute hosts to the resolved union. Each gets a distinct,
// subtle Hades accent (StatusDot status + matching Badge variant) so a chip's
// origin is legible at a glance without inventing raw colours.

type Source = 'egress' | 'environment' | 'connectors';

interface SourceStyle {
  label: string;
  icon: 'network' | 'layers' | 'key';
  dot: NonNullable<StatusDotProps['status']>;
  badge: NonNullable<BadgeProps['variant']>;
}

const SOURCE_STYLE: Record<Source, SourceStyle> = {
  egress: { label: 'Egress posture', icon: 'network', dot: 'pending', badge: 'pending' },
  environment: { label: 'Environment', icon: 'layers', dot: 'degraded', badge: 'degraded' },
  connectors: { label: 'Connectors', icon: 'key', dot: 'accent', badge: 'default' },
};

// Priority order when a host is contributed by more than one source — the chip
// shows a single "primary" marker.
const SOURCE_PRIORITY: Source[] = ['egress', 'environment', 'connectors'];

// ── Source-tinted host chip ─────────────────────────────────────────────────────

function SourceChip({ host, source, dimmed }: { host: string; source: Source; dimmed: boolean }) {
  return (
    <Stack
      direction="horizontal"
      align="center"
      gap="tight"
      style={{ opacity: dimmed ? 0.25 : 1, transition: 'opacity 0.15s' }}
    >
      <StatusDot status={SOURCE_STYLE[source].dot} size="sm" aria-hidden="true" />
      <Code>{host}</Code>
    </Stack>
  );
}

// Plain chips (no source marker) for the per-source breakdown sections.
function PlainHostChips({ hosts }: { hosts: string[] }) {
  if (hosts.length === 0) {
    return (
      <Text size="supporting" color="muted">
        No hosts.
      </Text>
    );
  }
  return (
    <Stack direction="horizontal" gap="tight" wrap>
      {hosts.map((host) => (
        <Code key={host}>{host}</Code>
      ))}
    </Stack>
  );
}

// ── Per-source breakdown section ────────────────────────────────────────────────

function SourceSection({
  source,
  detail,
  active,
  onHoverChange,
  children,
}: {
  source: Source;
  detail?: string;
  active: boolean;
  onHoverChange: (hovered: boolean) => void;
  children: React.ReactNode;
}) {
  const style = SOURCE_STYLE[source];
  return (
    <Stack
      gap="snug"
      paddingBlock="snug"
      paddingInline="comfortable"
      data-source={source}
      data-active={active ? 'true' : 'false'}
      onMouseEnter={() => onHoverChange(true)}
      onMouseLeave={() => onHoverChange(false)}
      style={{
        border: '1px solid var(--color-border)',
        borderRadius: '6px',
        background: active
          ? 'color-mix(in oklch, var(--color-muted) 12%, transparent)'
          : 'transparent',
        transition: 'background 0.15s',
      }}
    >
      <Stack direction="horizontal" align="center" gap="snug">
        <StatusDot status={style.dot} size="sm" aria-hidden="true" />
        <Icon name={style.icon} size="xs" color="subtle" />
        <Text size="supporting" weight="emphasis">
          {style.label}
        </Text>
        {detail && (
          <Text size="supporting" color="muted">
            {detail}
          </Text>
        )}
      </Stack>
      {children}
    </Stack>
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

interface EffectiveAccessPanelProps {
  access: EffectiveAccess | null;
  loading: boolean;
  error: string | null;
  /** Subtitle under the panel title. Defaults to the agent phrasing. */
  description?: string;
}

export function EffectiveAccessPanel({
  access,
  loading,
  error,
  description = 'What this agent can actually reach, resolved live as you edit.',
}: EffectiveAccessPanelProps) {
  // Which source section the user is hovering — drives the union highlight.
  const [hovered, setHovered] = useState<Source | null>(null);

  // Map each union host to its set of contributing sources, then pick a primary
  // (by SOURCE_PRIORITY) for the chip marker.
  const primaryBySource = useMemo(() => {
    const byHost = new Map<string, Set<Source>>();
    if (access) {
      const add = (host: string, source: Source) => {
        const set = byHost.get(host) ?? new Set<Source>();
        set.add(source);
        byHost.set(host, set);
      };
      for (const h of access.own_hosts) add(h, 'egress');
      for (const h of access.environment_hosts) add(h, 'environment');
      for (const ce of access.connector_egress) for (const h of ce.hosts) add(h, 'connectors');
    }
    return byHost;
  }, [access]);

  const primaryFor = (host: string): Source => {
    const set = primaryBySource.get(host);
    if (set) {
      for (const s of SOURCE_PRIORITY) if (set.has(s)) return s;
    }
    // A host present in the union but absent from every layer (shouldn't happen)
    // falls back to egress styling.
    return 'egress';
  };

  const isDimmed = (host: string): boolean => {
    if (!hovered) return false;
    return !(primaryBySource.get(host)?.has(hovered) ?? false);
  };

  return (
    <Card subtle border="default">
      <Stack
        gap="snug"
        paddingTop="comfortable"
        paddingInline="comfortable"
        direction="horizontal"
        align="center"
      >
        <Icon name="shield-check" size="sm" />
        <Stack gap="tight">
          <Text size="supporting" weight="emphasis">
            Effective access
          </Text>
          <Text size="caption" color="muted">
            {description}
          </Text>
        </Stack>
      </Stack>
      <CardContent padded>
        {loading ? (
          <Stack align="center" paddingBlock="comfortable">
            <OrbitalLoader />
          </Stack>
        ) : error ? (
          <Alert variant="error">{error}</Alert>
        ) : !access ? (
          <Text size="supporting" color="muted">
            Effective access is unavailable.
          </Text>
        ) : (
          <Stack gap="comfortable">
            {/* ── Union summary ─────────────────────────────────────────────── */}
            <Stack
              gap="snug"
              paddingBlock="comfortable"
              paddingInline="comfortable"
              style={{
                border: '1px solid var(--color-border)',
                borderRadius: '6px',
                background: 'var(--color-surface-elevated)',
              }}
            >
              <Stack direction="horizontal" align="center" gap="snug">
                <Text size="supporting" weight="emphasis">
                  Reachable hosts
                </Text>
                {access.open ? (
                  <Badge variant="default" size="sm">
                    Open egress
                  </Badge>
                ) : (
                  <Badge variant="secondary" size="sm">
                    {access.hosts.length} {access.hosts.length === 1 ? 'host' : 'hosts'}
                  </Badge>
                )}
              </Stack>
              {access.open ? (
                <Text size="supporting" color="muted">
                  Any host: open egress. Credentialed destinations are still gated by connections.
                </Text>
              ) : access.hosts.length === 0 ? (
                <Text size="supporting" color="muted">
                  No hosts. This agent has no outbound egress yet.
                </Text>
              ) : (
                <Stack direction="horizontal" gap="snug" wrap>
                  {access.hosts.map((host) => (
                    <SourceChip
                      key={host}
                      host={host}
                      source={primaryFor(host)}
                      dimmed={isDimmed(host)}
                    />
                  ))}
                </Stack>
              )}
            </Stack>

            {/* ── By source ─────────────────────────────────────────────────── */}
            <Stack gap="tight">
              <Text size="caption" color="muted" weight="emphasis" transform="uppercase">
                By source
              </Text>
              <Stack gap="snug">
                {!access.open && (
                  <SourceSection
                    source="egress"
                    active={hovered === 'egress'}
                    onHoverChange={(h) => setHovered(h ? 'egress' : null)}
                  >
                    <PlainHostChips hosts={access.own_hosts} />
                  </SourceSection>
                )}

                <SourceSection
                  source="environment"
                  detail={
                    access.environments.length === 0
                      ? 'None attached'
                      : `${access.environments.length} attached`
                  }
                  active={hovered === 'environment'}
                  onHoverChange={(h) => setHovered(h ? 'environment' : null)}
                >
                  {access.environments.length === 0 ? (
                    <Text size="supporting" color="muted">
                      No environments attached.
                    </Text>
                  ) : access.open ? (
                    <Text size="supporting" color="muted">
                      {access.environments.map((env) => env.name).join(', ')}. Open egress reaches
                      any host.
                    </Text>
                  ) : (
                    <Stack gap="snug">
                      {access.environments.map((env) => (
                        <Stack key={env.id} gap="tight">
                          <Text size="supporting" weight="emphasis">
                            {env.name}
                          </Text>
                          <PlainHostChips hosts={env.hosts} />
                        </Stack>
                      ))}
                    </Stack>
                  )}
                </SourceSection>

                <SourceSection
                  source="connectors"
                  detail="Auto-egress per connection"
                  active={hovered === 'connectors'}
                  onHoverChange={(h) => setHovered(h ? 'connectors' : null)}
                >
                  {access.connector_egress.length === 0 ? (
                    <Text size="supporting" color="muted">
                      No connectors attached.
                    </Text>
                  ) : (
                    <Stack gap="snug">
                      {access.connector_egress.map((ce) => {
                        // Resource narrowing on the matching connector grant
                        // (absent = all resources — no badge).
                        const resources =
                          access.connectors.find((c) => c.connection === ce.connection)
                            ?.resources ?? [];
                        return (
                          <Stack key={ce.connection} gap="tight">
                            <Stack direction="horizontal" align="center" gap="tight">
                              <Text size="supporting" weight="emphasis">
                                {ce.connection}
                              </Text>
                              <Badge variant="secondary" size="sm">
                                {ce.adapter}
                              </Badge>
                              {resources.length > 0 && (
                                <Badge variant="outline" size="sm">
                                  {resources.length}{' '}
                                  {resources.length === 1 ? 'resource' : 'resources'}
                                </Badge>
                              )}
                            </Stack>
                            <PlainHostChips hosts={ce.hosts} />
                          </Stack>
                        );
                      })}
                    </Stack>
                  )}
                </SourceSection>
              </Stack>
            </Stack>
          </Stack>
        )}
      </CardContent>
    </Card>
  );
}
