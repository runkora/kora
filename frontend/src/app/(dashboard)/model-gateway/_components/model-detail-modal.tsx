'use client';

import { useState, useCallback } from 'react';
import {
  Badge,
  Button,
  Icon,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Separator,
  Stack,
  Text,
} from '@orbit/terra';
import type { ModelRow } from '../_types';
import { AvailabilityDot } from './availability-dot';

// ── CopyRow ───────────────────────────────────────────────────────────────────

interface CopyRowProps {
  label: string;
  value: string;
  mono?: boolean;
}

function CopyRow({ label, value, mono }: CopyRowProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(() => {
    void navigator.clipboard.writeText(value).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }, [value]);

  return (
    <Button variant="ghost" size="sm" fullWidth onClick={handleCopy} aria-label={`Copy ${label}`}>
      <Stack direction="horizontal" align="center" justify="between" grow>
        <Stack gap="none">
          <Text size="caption" color="muted">
            {label}
          </Text>
          <Text size="supporting" weight="emphasis" mono={mono}>
            {value}
          </Text>
        </Stack>
        {copied ? (
          <Icon name="check" size="2xs" color="status-healthy" shrink />
        ) : (
          <Icon name="copy" size="2xs" color="muted" />
        )}
      </Stack>
    </Button>
  );
}

// ── ChipRow ───────────────────────────────────────────────────────────────────

interface ChipRowProps {
  label: string;
  values: string[];
}

function ChipRow({ label, values }: ChipRowProps) {
  return (
    <Stack
      direction="horizontal"
      wrap
      align="center"
      gap="snug"
      paddingBlock="tight"
      paddingInline="snug"
    >
      <Text size="caption" color="muted">
        {label}
      </Text>
      {values.length > 0 ? (
        <Stack direction="horizontal" wrap gap="tight">
          {values.map((v) => (
            <Badge key={v} variant="outline" size="sm">
              {v}
            </Badge>
          ))}
        </Stack>
      ) : (
        <Text size="supporting" color="subtle">
          —
        </Text>
      )}
    </Stack>
  );
}

// ── ModelDetailModal ──────────────────────────────────────────────────────────

interface ModelDetailModalProps {
  model: ModelRow | null;
  onClose: () => void;
}

export function ModelDetailModal({ model, onClose }: ModelDetailModalProps) {
  return (
    <Modal open={!!model} onClose={onClose} size="lg">
      {model && (
        <>
          {/* ModalHeader satisfies the Radix DialogTitle requirement.
              description renders the model ID as muted supporting text. */}
          <ModalHeader title={model.name} description={model.id} />

          {/* Status strip — lives between header and body intentionally */}
          <Stack asChild direction="horizontal" align="center" gap="snug">
            <aside className="border-b border-border px-4 py-2">
              <AvailabilityDot status={model.status} />
              <Text size="supporting">
                {model.status === 'available' ? 'Available' : 'Unavailable'}
              </Text>
            </aside>
          </Stack>

          {/* Scrollable body — flush/gapless so inner sections own their
              padding; constrained caps height and overflow-y-auto enables scrolling. */}
          <ModalBody flush gapless constrained>
            {/* Identity */}
            <Stack padding="snug">
              <Stack paddingTop="none" paddingInline="snug" paddingBottom="tight">
                <Text size="caption" color="muted">
                  Identity
                </Text>
              </Stack>
              <CopyRow label="Model ID" value={model.id} mono />
              <CopyRow label="Provider" value={model.owned_by} />
              <CopyRow label="Display Name" value={model.name} />
              {model.aliases.length > 0 && <ChipRow label="Aliases" values={model.aliases} />}
            </Stack>

            <Separator />

            {/* Capabilities */}
            <Stack padding="snug">
              <Stack paddingTop="none" paddingInline="snug" paddingBottom="tight">
                <Text size="caption" color="muted">
                  Capabilities
                </Text>
              </Stack>
              <ChipRow label="Capabilities" values={model.capabilities} />
              <ChipRow label="API Dialects" values={model.dialects} />
            </Stack>

            <Separator />

            {/* Modalities */}
            <Stack padding="snug">
              <Stack paddingTop="none" paddingInline="snug" paddingBottom="tight">
                <Text size="caption" color="muted">
                  Modalities
                </Text>
              </Stack>
              <ChipRow label="Input" values={model.input_modalities} />
              <ChipRow label="Output" values={model.output_modalities} />
            </Stack>
          </ModalBody>

          <ModalFooter justify="start">
            <Text size="caption" color="subtle">
              Click any identity row to copy to clipboard.
            </Text>
          </ModalFooter>
        </>
      )}
    </Modal>
  );
}
