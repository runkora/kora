/** @tier 3 — mcp-authorize-scoped. The OAuth consent screen + its result states. */
'use client';

import { useState } from 'react';
import { Button, Icon, Stack, Text } from '@runkora/hades';

// ── Props (all fetched server-side, passed as serialisable data) ──────────────

export interface ConsentTool {
  name: string;
  scope: string;
  granted: boolean;
}
export interface ConsentDomain {
  name: string;
  scope: string;
  tools: ConsentTool[];
}
export interface ConsentServer {
  name: string;
  displayName: string;
  endpoint: string;
}
export interface ConsentFlowProps {
  clientName: string;
  userEmail: string;
  server: ConsentServer | null;
  domains: ConsentDomain[];
  grantedCount: number;
  totalCount: number;
  techRows: { k: string; v: string; accent?: boolean }[];
  /** POST endpoint (/api/mcp/authorize) with the OAuth params already in its query. */
  actionUrl: string;
  csrfToken: string;
}

type View = 'consent' | 'authorizing' | 'connected' | 'cancelled';

const CARD = 472;

export function ConsentFlow(props: ConsentFlowProps) {
  const [view, setView] = useState<View>('consent');
  const [techOpen, setTechOpen] = useState(false);
  const [redirect, setRedirect] = useState('');
  const [error, setError] = useState('');

  async function decide(decision: 'approve' | 'deny') {
    if (decision === 'deny') {
      setView('cancelled');
      return;
    }
    setView('authorizing');
    try {
      const res = await fetch(props.actionUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({ decision, csrf_token: props.csrfToken }),
      });
      const body = (await res.json()) as { redirect?: string; error?: string };
      if (!res.ok || !body.redirect) {
        setError(body.error ?? 'Authorisation failed');
        setView('consent');
        return;
      }
      setRedirect(body.redirect);
      setView('connected');
      // Deliver the code to the client's loopback after a beat on the success screen.
      window.setTimeout(() => {
        window.location.href = body.redirect as string;
      }, 1400);
    } catch {
      setError('Network error. Please try again.');
      setView('consent');
    }
  }

  if (view === 'authorizing') return <AuthorizingCard />;
  if (view === 'connected')
    return <ConnectedCard clientName={props.clientName} redirect={redirect} />;
  if (view === 'cancelled') return <CancelledCard onBack={() => setView('consent')} />;

  return (
    <ConsentCard
      {...props}
      techOpen={techOpen}
      onToggleTech={() => setTechOpen((v) => !v)}
      error={error}
      onApprove={() => decide('approve')}
      onDeny={() => decide('deny')}
    />
  );
}

// ── Consent state ─────────────────────────────────────────────────────────────

function ConsentCard(
  p: ConsentFlowProps & {
    techOpen: boolean;
    onToggleTech: () => void;
    error: string;
    onApprove: () => void;
    onDeny: () => void;
  },
) {
  return (
    <Stack
      style={{
        width: CARD,
        borderRadius: 'var(--radius-lg)',
        border: '1px solid var(--color-border)',
        background: 'var(--color-surface)',
        overflow: 'hidden',
      }}
    >
      {/* Header */}
      <Stack direction="horizontal" style={{ gap: 14, padding: '22px 24px 4px' }}>
        <IconBadge name="zap" size={44} radius="var(--radius-lg)" />
        <Stack style={{ gap: 0 }}>
          <Text style={{ fontSize: 19, fontWeight: 700, letterSpacing: '-0.01em' }}>
            Authorise {p.clientName}
          </Text>
          <Text
            style={{ fontSize: 13, lineHeight: 1.5, color: 'var(--color-muted)', marginTop: 7 }}
          >
            It wants to connect to the{' '}
            <Strong>
              {(p.server?.displayName ?? 'kora').replace(/\s*MCP\s*$/i, '') || 'kora'}
            </Strong>{' '}
            MCP server and call its tools <Strong>as you</Strong>.
          </Text>
        </Stack>
      </Stack>

      <Stack style={{ padding: '18px 24px', gap: 18 }}>
        {p.error ? (
          <Text
            style={{
              fontSize: 12,
              color: 'var(--color-status-error)',
              background: 'color-mix(in oklch, var(--color-status-error) 8%, transparent)',
              border: '1px solid color-mix(in oklch, var(--color-status-error) 30%, transparent)',
              borderRadius: 'var(--radius-md)',
              padding: '8px 11px',
            }}
          >
            {p.error}
          </Text>
        ) : null}

        {/* Signed in as */}
        <Stack
          direction="horizontal"
          align="center"
          style={{
            gap: 11,
            padding: '11px 13px',
            borderRadius: 'var(--radius-md)',
            background: 'var(--color-background)',
            border: '1px solid var(--color-border-subtle)',
          }}
        >
          <IconBadge name="user" size={32} radius="var(--radius-full)" iconSize="sm" />
          <Stack style={{ flex: 1, minWidth: 0, gap: 0 }}>
            <Text as="span" style={{ fontSize: 12.5, fontWeight: 600 }}>
              {p.userEmail}
            </Text>
            <Text as="span" style={{ fontSize: 11.5, color: 'var(--color-subtle)' }}>
              Signed in · acting as delegated identity
            </Text>
          </Stack>
        </Stack>

        {/* Tool surface */}
        {p.server ? <ToolSurface {...p} /> : null}

        {/* Request details (technical, collapsible) */}
        <Stack
          style={{
            border: '1px solid var(--color-border-subtle)',
            borderRadius: 'var(--radius-md)',
            overflow: 'hidden',
            background: 'var(--color-background)',
          }}
        >
          <Button variant="ghost" onClick={p.onToggleTech} style={{ justifyContent: 'flex-start' }}>
            <Stack direction="horizontal" align="center" style={{ gap: 9, width: '100%' }}>
              <Icon name="lock" size="sm" color="subtle" />
              <Text
                as="span"
                style={{
                  flex: 1,
                  textAlign: 'left',
                  fontSize: 12,
                  fontWeight: 600,
                  color: 'var(--color-muted)',
                }}
              >
                Request details
              </Text>
              <Text as="span" mono style={{ fontSize: 10.5, color: 'var(--color-subtle)' }}>
                OAuth 2.1 · PKCE
              </Text>
              <Icon name={p.techOpen ? 'chevron-up' : 'chevron-down'} size="sm" color="subtle" />
            </Stack>
          </Button>
          {p.techOpen ? (
            <Stack
              style={{
                padding: '2px 13px 12px',
                gap: 8,
                borderTop: '1px solid var(--color-border-subtle)',
              }}
            >
              {p.techRows.map((row) => (
                <Stack
                  key={row.k}
                  direction="horizontal"
                  justify="between"
                  style={{ gap: 12, paddingTop: 9 }}
                >
                  <Text
                    as="span"
                    mono
                    style={{ fontSize: 11, color: 'var(--color-subtle)', flexShrink: 0 }}
                  >
                    {row.k}
                  </Text>
                  <Text
                    as="span"
                    mono
                    style={{
                      fontSize: 11,
                      color: row.accent ? 'var(--color-accent)' : 'var(--color-muted)',
                      textAlign: 'right',
                      wordBreak: 'break-all',
                    }}
                  >
                    {row.v}
                  </Text>
                </Stack>
              ))}
            </Stack>
          ) : null}
        </Stack>
      </Stack>

      {/* Actions */}
      <Stack style={{ padding: '0 24px 22px', gap: 10 }}>
        <Button onClick={p.onApprove} style={{ width: '100%', height: 44 }}>
          Authorise access
        </Button>
        <Button variant="outline" onClick={p.onDeny} style={{ width: '100%', height: 42 }}>
          Cancel
        </Button>
        <Text
          style={{
            fontSize: 11,
            lineHeight: 1.5,
            color: 'var(--color-subtle)',
            textAlign: 'center',
            marginTop: 5,
          }}
        >
          By authorising, kora issues a short-lived token that acts as you, bounded by your grants;
          {p.clientName} re-authorises when it expires. You can revoke it any time from{' '}
          <Text as="span" mono style={{ fontSize: 11 }}>
            Connections
          </Text>
          .
        </Text>
      </Stack>
    </Stack>
  );
}

function ToolSurface(p: ConsentFlowProps) {
  return (
    <Stack style={{ gap: 0 }}>
      <Stack direction="horizontal" align="center" justify="between" style={{ marginBottom: 10 }}>
        <Text
          as="span"
          style={{
            fontSize: 10,
            fontWeight: 600,
            letterSpacing: '0.13em',
            color: 'var(--color-subtle)',
          }}
        >
          GRANTING YOUR TOOLS ON
        </Text>
        <Text as="span" style={{ fontSize: 11, color: 'var(--color-muted)' }}>
          <Text as="span" style={{ color: 'var(--color-accent)', fontWeight: 600 }}>
            {p.grantedCount}
          </Text>{' '}
          of {p.totalCount} shared
        </Text>
      </Stack>

      {/* Server identity */}
      <Stack
        direction="horizontal"
        align="center"
        style={{
          gap: 11,
          padding: '11px 13px',
          borderRadius: 'var(--radius-md) var(--radius-md) 0 0',
          background: 'var(--color-background)',
          border: '1px solid var(--color-border-subtle)',
        }}
      >
        <IconBadge name="circle-dot" size={30} radius="var(--radius-sm)" iconSize="sm" />
        <Stack style={{ flex: 1, minWidth: 0, gap: 0 }}>
          <Text as="span" style={{ fontSize: 13, fontWeight: 600 }}>
            {p.server?.displayName}
          </Text>
          <Text as="span" mono style={{ fontSize: 11, color: 'var(--color-subtle)' }}>
            {p.server?.endpoint}
          </Text>
        </Stack>
      </Stack>

      {/* Tools grouped by domain */}
      <Stack
        style={{
          border: '1px solid var(--color-border-subtle)',
          borderTop: 'none',
          borderRadius: '0 0 var(--radius-md) var(--radius-md)',
          background: 'var(--color-background)',
          maxHeight: 224,
          overflowY: 'auto',
          gap: 0,
        }}
      >
        {p.domains.map((dm) => (
          <Stack
            key={dm.name}
            style={{ borderTop: '1px solid var(--color-border-subtle)', gap: 0 }}
          >
            <Stack
              direction="horizontal"
              align="center"
              style={{ gap: 9, padding: '8px 13px', background: 'var(--color-surface)' }}
            >
              <Text
                as="span"
                style={{ fontSize: 12, fontWeight: 600, textTransform: 'capitalize' }}
              >
                {dm.name}
              </Text>
              <Text
                as="span"
                mono
                style={{
                  fontSize: 10.5,
                  color: 'var(--color-subtle)',
                  padding: '1px 6px',
                  borderRadius: 'var(--radius-sm)',
                  background: 'var(--color-background)',
                  border: '1px solid var(--color-border-subtle)',
                }}
              >
                {dm.scope}
              </Text>
              <Text
                as="span"
                mono
                style={{ marginLeft: 'auto', fontSize: 10.5, color: 'var(--color-subtle)' }}
              >
                {dm.tools.length}
              </Text>
            </Stack>
            {dm.tools.map((t) => (
              <Stack
                key={t.name}
                direction="horizontal"
                align="center"
                style={{
                  gap: 10,
                  padding: '8px 13px 8px 15px',
                  borderTop: '1px solid var(--color-border-subtle)',
                  opacity: t.granted ? 1 : 0.45,
                }}
              >
                <Icon
                  name={t.granted ? 'circle-check' : 'circle-x'}
                  size="sm"
                  color={t.granted ? 'accent' : 'subtle'}
                />
                <Text
                  as="span"
                  mono
                  style={{
                    fontSize: 11.5,
                    fontWeight: 600,
                    color: t.granted ? 'var(--color-foreground)' : 'var(--color-subtle)',
                    flexShrink: 0,
                  }}
                >
                  {t.name}
                </Text>
                <Text
                  as="span"
                  mono
                  style={{
                    fontSize: 10.5,
                    color: 'var(--color-subtle)',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                    flex: 1,
                    textAlign: 'right',
                  }}
                >
                  {t.scope}
                </Text>
                {!t.granted ? (
                  <Text
                    as="span"
                    style={{
                      flexShrink: 0,
                      fontSize: 10,
                      color: 'var(--color-subtle)',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    not in your grants
                  </Text>
                ) : null}
              </Stack>
            ))}
          </Stack>
        ))}
      </Stack>

      {/* Shield note */}
      <Stack
        direction="horizontal"
        style={{
          gap: 9,
          marginTop: 12,
          padding: '11px 12px',
          borderRadius: 'var(--radius-md)',
          background: 'var(--color-accent-subtle)',
          border: '1px solid var(--color-accent-border)',
        }}
      >
        <Icon name="shield-check" size="sm" color="accent" />
        <Text style={{ fontSize: 11.5, lineHeight: 1.5, color: 'var(--color-muted)' }}>
          {p.clientName} receives only the tools <Strong>you</Strong> hold grants for on this
          server. Tools you can’t use are hidden from{' '}
          <Text as="span" mono style={{ fontSize: 11.5 }}>
            tools/list
          </Text>
          . Authorisation is <Strong>per server</Strong>: connecting another MCP server needs its
          own approval.
        </Text>
      </Stack>
    </Stack>
  );
}

// ── Result states ─────────────────────────────────────────────────────────────

function AuthorizingCard() {
  return (
    <ResultCard>
      <Spinner />
      <Stack style={{ gap: 0 }}>
        <Text style={{ fontSize: 16, fontWeight: 600 }}>Authorising…</Text>
        <Text
          style={{ fontSize: 12.5, lineHeight: 1.5, color: 'var(--color-muted)', marginTop: 7 }}
        >
          Issuing an authorisation code and redirecting.
        </Text>
      </Stack>
      <Text as="span" mono style={{ fontSize: 11, color: 'var(--color-subtle)' }}>
        POST /token · grant_type=authorization_code
      </Text>
    </ResultCard>
  );
}

function ConnectedCard({ clientName, redirect }: { clientName: string; redirect: string }) {
  return (
    <Stack
      style={{
        width: CARD,
        borderRadius: 'var(--radius-lg)',
        border: '1px solid var(--color-border)',
        background: 'var(--color-surface)',
        overflow: 'hidden',
      }}
    >
      <Stack align="center" style={{ padding: '36px 32px 24px', gap: 14, textAlign: 'center' }}>
        <IconBadge
          name="circle-check"
          size={58}
          iconSize="lg"
          tone="healthy"
          radius="var(--radius-full)"
        />
        <Stack style={{ gap: 0 }}>
          <Text style={{ fontSize: 19, fontWeight: 700, letterSpacing: '-0.01em' }}>
            {clientName} connected
          </Text>
          <Text
            style={{ fontSize: 13, lineHeight: 1.55, color: 'var(--color-muted)', marginTop: 8 }}
          >
            Your tools are live. Return to {clientName}; its tool list now shows what you
            authorised.
          </Text>
        </Stack>
      </Stack>
      <Stack
        style={{
          margin: '0 24px',
          padding: '13px 15px',
          borderRadius: 'var(--radius-md)',
          background: 'var(--color-background)',
          border: '1px solid var(--color-border-subtle)',
          gap: 9,
        }}
      >
        <Stack direction="horizontal" align="center" justify="between" style={{ gap: 12 }}>
          <Text as="span" mono style={{ fontSize: 11, color: 'var(--color-subtle)' }}>
            token
          </Text>
          <Text as="span" mono style={{ fontSize: 11, color: 'var(--color-muted)' }}>
            short-lived · acts as you
          </Text>
        </Stack>
      </Stack>
      <Stack align="center" style={{ padding: '20px 24px 24px', gap: 10 }}>
        <Stack
          direction="horizontal"
          align="center"
          style={{ gap: 8, fontSize: 12.5, color: 'var(--color-subtle)' }}
        >
          <Icon name="circle-check" size="sm" color="status-healthy" />
          <Text as="span" style={{ fontSize: 12.5, color: 'var(--color-subtle)' }}>
            You can close this window
          </Text>
        </Stack>
        <Button
          variant="outline"
          onClick={() => (window.location.href = redirect)}
          style={{ width: '100%', height: 40 }}
        >
          Continue
        </Button>
      </Stack>
    </Stack>
  );
}

function CancelledCard({ onBack }: { onBack: () => void }) {
  return (
    <ResultCard>
      <IconBadge
        name="circle-x"
        size={54}
        iconSize="lg"
        tone="subtle"
        radius="var(--radius-full)"
      />
      <Stack style={{ gap: 0 }}>
        <Text style={{ fontSize: 18, fontWeight: 700 }}>Request cancelled</Text>
        <Text style={{ fontSize: 13, lineHeight: 1.55, color: 'var(--color-muted)', marginTop: 8 }}>
          Access was not granted. No token was issued and nothing changed.
        </Text>
      </Stack>
      <Text as="span" mono style={{ fontSize: 11, color: 'var(--color-subtle)' }}>
        error=access_denied
      </Text>
      <Button onClick={onBack} style={{ width: '100%', height: 42, marginTop: 4 }}>
        Back to request
      </Button>
    </ResultCard>
  );
}

// ── Small building blocks ─────────────────────────────────────────────────────

function ResultCard({ children }: { children: React.ReactNode }) {
  return (
    <Stack
      align="center"
      style={{
        width: CARD,
        borderRadius: 'var(--radius-lg)',
        border: '1px solid var(--color-border)',
        background: 'var(--color-surface)',
        padding: '44px 32px',
        gap: 16,
        textAlign: 'center',
      }}
    >
      {children}
    </Stack>
  );
}

function Strong({ children }: { children: React.ReactNode }) {
  // Inherit the surrounding sentence's size/line-height — otherwise Hades' Text
  // default size makes bold words jump larger than the text around them.
  return (
    <Text
      as="span"
      style={{
        fontWeight: 600,
        fontSize: 'inherit',
        lineHeight: 'inherit',
        color: 'var(--color-foreground)',
      }}
    >
      {children}
    </Text>
  );
}

function IconBadge({
  name,
  size,
  radius,
  iconSize = 'md',
  tone = 'accent',
}: {
  name: string;
  size: number;
  radius: string;
  iconSize?: 'sm' | 'md' | 'lg';
  tone?: 'accent' | 'healthy' | 'subtle';
}) {
  const bg =
    tone === 'healthy'
      ? 'color-mix(in oklch, var(--color-status-healthy) 15%, transparent)'
      : tone === 'subtle'
        ? 'var(--color-surface-overlay)'
        : 'var(--color-accent-muted)';
  const border =
    tone === 'healthy'
      ? 'color-mix(in oklch, var(--color-status-healthy) 34%, transparent)'
      : tone === 'subtle'
        ? 'var(--color-border-subtle)'
        : 'var(--color-accent-border)';
  const color =
    tone === 'healthy'
      ? 'var(--color-status-healthy)'
      : tone === 'subtle'
        ? 'var(--color-subtle)'
        : 'var(--color-accent)';
  const iconColor = tone === 'healthy' ? 'status-healthy' : tone === 'subtle' ? 'subtle' : 'accent';
  return (
    <Stack
      align="center"
      justify="center"
      style={{
        width: size,
        height: size,
        flexShrink: 0,
        borderRadius: radius,
        background: bg,
        border: `1px solid ${border}`,
        color,
      }}
    >
      <Icon name={name as never} size={iconSize} color={iconColor} />
    </Stack>
  );
}

function Spinner() {
  return (
    <Stack
      style={{
        width: 46,
        height: 46,
        borderRadius: 9999,
        border: '2.5px solid var(--color-surface-overlay)',
        borderTopColor: 'var(--color-accent)',
        animation: 'spin .8s linear infinite',
      }}
    />
  );
}
