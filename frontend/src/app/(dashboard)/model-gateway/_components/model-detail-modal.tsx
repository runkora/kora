'use client';

import {
  Badge,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Separator,
  Stack,
  Text,
  Icon,
} from '@orbit/terra';
import { fmtCost } from '@orbit/utils';
import type { ModelRow } from '../_types';
import { AvailabilityDot } from './availability-dot';
import { CopyRow } from '@orbit/components';

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
      paddingInline="cozy"
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
          <Stack
            direction="horizontal"
            align="center"
            gap="snug"
            paddingBlock="snug"
            paddingInline="comfortable"
          >
            <AvailabilityDot status={model.status} />
            <Text size="supporting">
              {model.status === 'available' ? 'Available' : 'Unavailable'}
            </Text>
          </Stack>
          <Separator />

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
            <Stack padding="snug" gap="none">
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
            <Stack padding="snug" gap="none">
              <Stack paddingTop="none" paddingInline="snug" paddingBottom="tight">
                <Text size="caption" color="muted">
                  Modalities
                </Text>
              </Stack>
              <ChipRow label="Input" values={model.input_modalities} />
              <ChipRow label="Output" values={model.output_modalities} />
            </Stack>

            <Separator />

            {/* Pricing */}
            <Stack padding="snug" gap="none">
              <Stack paddingTop="none" paddingInline="snug" paddingBottom="tight">
                <Text size="caption" color="muted">
                  Pricing
                </Text>
              </Stack>
              {model.pricing ? (
                <>
                  <Stack
                    direction="horizontal"
                    align="center"
                    justify="between"
                    paddingBlock="tight"
                    paddingInline="cozy"
                  >
                    <Text size="caption" color="muted">
                      Input /1M tokens
                    </Text>
                    <Text size="supporting" mono>
                      {fmtCost(model.pricing.effective_input_cost_per_1m)}
                    </Text>
                  </Stack>
                  <Stack
                    direction="horizontal"
                    align="center"
                    justify="between"
                    paddingBlock="tight"
                    paddingInline="cozy"
                  >
                    <Text size="caption" color="muted">
                      Output /1M tokens
                    </Text>
                    <Text size="supporting" mono>
                      {fmtCost(model.pricing.effective_output_cost_per_1m)}
                    </Text>
                  </Stack>
                </>
              ) : (
                <Stack
                  direction="horizontal"
                  align="center"
                  gap="tight"
                  paddingBlock="tight"
                  paddingInline="cozy"
                >
                  <Icon name="alert-circle" size="2xs" color="status-degraded" />
                  <Text size="caption" color="muted">
                    Unknown
                  </Text>
                </Stack>
              )}
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
