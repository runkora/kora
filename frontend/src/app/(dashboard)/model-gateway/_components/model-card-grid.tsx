'use client';

import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Grid,
  Icon,
  Stack,
  Text,
  Heading,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@clear-route/hades';
import { fmtCost } from '@orbit/utils';
import type { ModelRow } from '../_types';
import { AvailabilityDot } from './availability-dot';

// ── ModelCard ──────────────────────────────────────────────────────────────────

interface ModelCardProps {
  model: ModelRow;
  onRemove?: (id: string) => void;
  onSelect?: (model: ModelRow) => void;
}

function ModelCard({ model, onRemove, onSelect }: ModelCardProps) {
  return (
    <Stack minWidth="none">
      <Card
        column
        interactive={!!onSelect}
        role={onSelect ? 'button' : undefined}
        tabIndex={onSelect ? 0 : undefined}
        onClick={() => onSelect?.(model)}
        onKeyDown={(e) => {
          if (!onSelect) return;
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            onSelect(model);
          }
        }}
      >
        <CardHeader compact>
          <Stack direction="horizontal" align="start" justify="between" grow>
            <Stack gap="none" minWidth="none" grow>
              <CardTitle>{model.name}</CardTitle>
              <Text size="caption" color="muted" mono truncate>
                {model.id}
              </Text>
            </Stack>
            {onRemove && (
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Remove from favourites"
                onClick={(e) => {
                  e.stopPropagation();
                  onRemove(model.id);
                }}
              >
                <Icon name="heart" size="2xs" color="rose" filled />
              </Button>
            )}
          </Stack>
        </CardHeader>
        <CardContent>
          <Stack gap="snug">
            {/* Availability */}
            <Stack direction="horizontal" align="center" gap="tight">
              <AvailabilityDot status={model.status} />
              <Text size="caption" color="muted">
                {model.status === 'available' ? 'Available' : 'Unavailable'}
              </Text>
            </Stack>

            {/* Capabilities & Dialects */}
            {(model.capabilities.length > 0 || model.dialects.length > 0) && (
              <Stack direction="horizontal" wrap gap="tight">
                {model.capabilities.map((cap) => (
                  <Badge key={cap} variant="outline" size="sm">
                    {cap}
                  </Badge>
                ))}
                {model.dialects.map((d) => (
                  <Badge key={d} variant="outline" size="sm">
                    {d}
                  </Badge>
                ))}
              </Stack>
            )}

            {/* Pricing */}
            <Stack direction="horizontal" align="center" justify="between">
              <Text size="caption" color="muted">
                Effective Price
              </Text>
              {model.pricing?.non_token_only ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      variant="muted"
                      size="icon-sm"
                      aria-label="Non-token pricing — see model details"
                    >
                      <Icon name="info" size="2xs" color="muted" />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>Non-token pricing — see model details</TooltipContent>
                </Tooltip>
              ) : model.pricing ? (
                <Stack gap="none" textAlign="end">
                  <Text size="caption" mono>
                    {fmtCost(model.pricing.effective_input_cost_per_1m ?? null)}/1M
                  </Text>
                  <Text size="caption" color="muted" mono>
                    out: {fmtCost(model.pricing.effective_output_cost_per_1m ?? null)}/1M
                  </Text>
                </Stack>
              ) : (
                <Text size="caption" color="muted">
                  —
                </Text>
              )}
            </Stack>

            {/* Modalities */}
            <Stack gap="snug">
              {model.input_modalities.length > 0 && (
                <Stack direction="horizontal" align="center" justify="between">
                  <Text size="caption" color="muted">
                    Input
                  </Text>
                  <Text size="caption" mono>
                    {model.input_modalities.join(', ')}
                  </Text>
                </Stack>
              )}

              {model.output_modalities.length > 0 && (
                <Stack direction="horizontal" align="center" justify="between">
                  <Text size="caption" color="muted">
                    Output
                  </Text>
                  <Text size="caption" mono>
                    {model.output_modalities.join(', ')}
                  </Text>
                </Stack>
              )}
            </Stack>
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}

// ── ModelCardGrid ──────────────────────────────────────────────────────────────

interface ModelCardGridProps {
  /** Model rows to display. */
  models: ModelRow[];
  /** When provided the grid is interactive: each card shows a remove button. */
  onRemove?: (id: string) => void;
  /** Called when a card is clicked — opens the detail modal. */
  onSelect?: (model: ModelRow) => void;
  title?: string;
  description?: string;
}

export function ModelCardGrid({
  models,
  onRemove,
  onSelect,
  title = 'Favourites',
  description,
}: ModelCardGridProps) {
  if (models.length === 0) return null;

  return (
    <Stack gap="snug">
      <Stack gap="none">
        <Heading variant="section">{title}</Heading>
        {description && (
          <Text size="supporting" color="muted">
            {description}
          </Text>
        )}
      </Stack>

      <Grid cols={{ base: 1, sm: 2, lg: 3, xl: 4 }} gap="snug">
        {models.map((model) => (
          <ModelCard key={model.id} model={model} onRemove={onRemove} onSelect={onSelect} />
        ))}
      </Grid>
    </Stack>
  );
}
