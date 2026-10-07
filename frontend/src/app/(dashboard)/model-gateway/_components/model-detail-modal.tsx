'use client';

import {
  CopyField,
  Badge,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Separator,
  Stack,
  Text,
  Icon,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@orbit/terra';
import { fmtCost } from '@orbit/utils';
import type { ModelObjectPricing, ModelRow } from '../_types';
import { AvailabilityDot } from './availability-dot';

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

// ── PricingRows ───────────────────────────────────────────────────────────────

function PricingRow({ label, value }: { label: string; value: number }) {
  return (
    <Stack
      direction="horizontal"
      align="center"
      justify="between"
      paddingBlock="tight"
      paddingInline="cozy"
    >
      <Text size="caption" color="muted">
        {label}
      </Text>
      <Text size="supporting" mono>
        {fmtCost(value)}
      </Text>
    </Stack>
  );
}

function PricingRows({ pricing }: { pricing?: ModelObjectPricing }) {
  if (!pricing) {
    return (
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
    );
  }

  const hasToken = !pricing.non_token_only;
  const hasCache =
    (pricing.cache_read_cost_per_1m ?? 0) > 0 || (pricing.cache_write_cost_per_1m ?? 0) > 0;
  const nonTokenDimensions: { label: string; value: number }[] = [
    { label: '/image in', value: pricing.image_input_cost ?? 0 },
    { label: '/image out', value: pricing.image_output_cost ?? 0 },
    { label: '/request in', value: pricing.request_input_cost ?? 0 },
    { label: '/request out', value: pricing.request_output_cost ?? 0 },
    { label: '/second in', value: pricing.second_input_cost ?? 0 },
    { label: '/second out', value: pricing.second_output_cost ?? 0 },
    { label: '/1M audio in', value: pricing.audio_input_cost_per_1m ?? 0 },
    { label: '/1M audio out', value: pricing.audio_output_cost_per_1m ?? 0 },
  ].filter((d) => d.value > 0);

  return (
    <>
      {pricing.non_token_only && (
        <Stack
          direction="horizontal"
          align="center"
          gap="tight"
          paddingBlock="tight"
          paddingInline="cozy"
        >
          <Tooltip>
            <TooltipTrigger asChild>
              <Stack direction="horizontal" align="center" gap="tight">
                <Icon name="info" size="2xs" color="muted" />
                <Text size="caption" color="muted">
                  Non-token pricing only
                </Text>
              </Stack>
            </TooltipTrigger>
            <TooltipContent>
              This model is billed by dimensions other than tokens (e.g. per image or per request).
              Token costs are not applicable.
            </TooltipContent>
          </Tooltip>
        </Stack>
      )}
      {hasToken && (
        <>
          <PricingRow label="Input /1M tokens" value={pricing.effective_input_cost_per_1m ?? 0} />
          <PricingRow label="Output /1M tokens" value={pricing.effective_output_cost_per_1m ?? 0} />
        </>
      )}
      {hasCache && (
        <>
          <PricingRow label="Cache read /1M tokens" value={pricing.cache_read_cost_per_1m ?? 0} />
          <PricingRow label="Cache write /1M tokens" value={pricing.cache_write_cost_per_1m ?? 0} />
        </>
      )}
      {nonTokenDimensions.map((d) => (
        <PricingRow key={d.label} label={d.label} value={d.value} />
      ))}
    </>
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
              <CopyField label="Model ID" value={model.id} mono wrap />
              <CopyField label="Provider" value={model.owned_by} wrap />
              <CopyField label="Display Name" value={model.name} wrap />
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
              <PricingRows pricing={model.pricing} />
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
