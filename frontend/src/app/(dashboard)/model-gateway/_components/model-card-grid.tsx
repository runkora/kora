'use client';

import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Grid,
  Icon,
  Stack,
  Text,
  Heading,
} from '@orbit/terra';
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
    <Stack asChild>
      <article
        className="relative min-w-0 cursor-pointer transition-colors hover:bg-surface-elevated/50"
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
        <Card column>
          {onRemove && (
            <button
              type="button"
              aria-label="Remove from favourites"
              onClick={(e) => {
                e.stopPropagation();
                onRemove(model.id);
              }}
              className="absolute right-3 top-3 z-10 rounded p-1 transition-colors hover:bg-surface-elevated focus-visible:outline-2 focus-visible:outline-accent"
            >
              <Icon name="heart" size="2xs" color="rose" filled />
            </button>
          )}
          <CardHeader compact>
            <Stack gap="none" maxWidth="full">
              <CardTitle>{model.name}</CardTitle>
              <Text size="caption" color="muted" mono truncate>
                {model.id}
              </Text>
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

              {/* Modalities */}
              <Stack gap="none">
                {model.input_modalities.length > 0 && (
                  // eslint-disable-next-line local/no-raw-div
                  <div className="flex items-center justify-between py-1">
                    <Text size="caption" color="muted">
                      Input
                    </Text>
                    <Text size="caption" mono>
                      {model.input_modalities.join(', ')}
                    </Text>
                  </div>
                )}

                {model.output_modalities.length > 0 && (
                  // eslint-disable-next-line local/no-raw-div
                  <div className="flex items-center justify-between py-1">
                    <Text size="caption" color="muted">
                      Output
                    </Text>
                    <Text size="caption" mono>
                      {model.output_modalities.join(', ')}
                    </Text>
                  </div>
                )}
              </Stack>
            </Stack>
          </CardContent>
        </Card>
      </article>
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
