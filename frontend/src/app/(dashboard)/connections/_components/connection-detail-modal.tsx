/** @tier 3 — connections-scoped. Full grantable-action and usage detail for a
 * connection, reached by clicking its row — the table itself only shows the
 * first few action chips to keep rows from wrapping. */
'use client';

import { useState } from 'react';
import Link from 'next/link';
import { Badge, Modal, ModalBody, ModalHeader, Separator, Stack, Text } from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import { adapterLabel } from '../_types';
import type { ConnectionRow } from '../_types';

// ── SectionLabel ────────────────────────────────────────────────────────────

function SectionLabel({ children }: { children: string }) {
  return (
    <Stack paddingTop="none" paddingInline="snug" paddingBottom="tight">
      <Text size="caption" color="muted">
        {children}
      </Text>
    </Stack>
  );
}

// ── UsedByPill — links to the agent's page; swaps to Badge's accent variant
// on hover/focus so the pill itself takes the accent fill, not just a halo
// painted behind a transparent badge. ──────────────────────────────────────

function UsedByPill({ slug, name }: { slug: string; name: string }) {
  const [active, setActive] = useState(false);
  return (
    <Link
      href={APP_ROUTES.agents.detail(slug)}
      onMouseEnter={() => setActive(true)}
      onMouseLeave={() => setActive(false)}
      onFocus={() => setActive(true)}
      onBlur={() => setActive(false)}
      className="rounded-md outline-none focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent"
    >
      <Badge variant={active ? 'default' : 'outline'} size="sm">
        {name}
      </Badge>
    </Link>
  );
}

// ── ConnectionDetailModal ────────────────────────────────────────────────────

interface ConnectionDetailModalProps {
  connection: ConnectionRow | null;
  onClose: () => void;
}

export function ConnectionDetailModal({ connection, onClose }: ConnectionDetailModalProps) {
  return (
    <Modal open={!!connection} onClose={onClose} size="lg">
      {connection && (
        <>
          <ModalHeader title={connection.name} description={adapterLabel(connection.adapter)} />
          <Separator />

          <ModalBody flush gapless constrained>
            <Stack padding="snug" gap="tight">
              <SectionLabel>Grantable actions</SectionLabel>
              {connection.actions.length > 0 ? (
                <Stack direction="horizontal" gap="tight" wrap paddingInline="snug">
                  {connection.actions.map((action) => (
                    <Badge key={action} variant="elevated" size="sm" mono>
                      {action}
                    </Badge>
                  ))}
                </Stack>
              ) : (
                <Stack paddingInline="snug">
                  <Text size="supporting" color="subtle">
                    No grantable actions.
                  </Text>
                </Stack>
              )}
            </Stack>

            <Separator />

            <Stack padding="snug" gap="tight">
              <SectionLabel>Used by</SectionLabel>
              {connection.usedBy.length > 0 ? (
                <Stack direction="horizontal" gap="tight" wrap paddingInline="snug">
                  {connection.usedBy.map((usage) => (
                    <UsedByPill key={usage.slug} slug={usage.slug} name={usage.name} />
                  ))}
                </Stack>
              ) : (
                <Stack paddingInline="snug">
                  <Text size="supporting" color="subtle">
                    Not attached to any agents.
                  </Text>
                </Stack>
              )}
            </Stack>
          </ModalBody>
        </>
      )}
    </Modal>
  );
}
