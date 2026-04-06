'use client';

import { Heart } from 'lucide-react';
import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Grid,
  Stack,
  Text,
  Heading,
} from '@orbit/terra';
import type { ModelRow } from '../../_types';
import { useModelsContext } from '../../_context/models-context';
import { AvailabilityDot } from './availability-dot';

// ── ModelCard ──────────────────────────────────────────────────────────────────

interface ModelCardProps {
  model: ModelRow;
  onRemove?: (id: string) => void;
}

function ModelCard({ model, onRemove }: ModelCardProps) {
  return (
    <Stack asChild>
      <article className="relative min-w-0">
        <Card column>
          {onRemove && (
            <button
              type="button"
              aria-label="Remove from favourites"
              onClick={() => onRemove(model.id)}
              className="absolute right-3 top-3 rounded p-1 text-rose-400 transition-colors hover:bg-surface-elevated hover:text-rose-600 focus-visible:outline-2 focus-visible:outline-accent"
            >
              <Heart className="size-3.5 fill-current" />
            </button>
          )}
          <CardHeader compact>
            <Stack direction="horizontal" align="center" gap="tight">
              <Stack gap="none" maxWidth="full">
                <CardTitle>{model.name}</CardTitle>
                <Text size="caption" color="muted" mono truncate>
                  {model.id}
                </Text>
              </Stack>
            </Stack>
          </CardHeader>
          <CardContent>
            <Stack gap="tight">
              {/* Availability */}
              <Stack direction="horizontal" align="center" gap="tight">
                <AvailabilityDot status={model.status} />
                <Text size="caption" color="muted">
                  {model.status === 'available' ? 'Available' : 'Unavailable'}
                </Text>
              </Stack>

              {/* Capabilities */}
              {model.capabilities.length > 0 && (
                <Stack direction="horizontal" wrap gap="tight">
                  {model.capabilities.map((cap) => (
                    <Badge key={cap} variant="outline" size="sm">
                      {cap}
                    </Badge>
                  ))}
                </Stack>
              )}

              {/* Input modalities */}
              {model.input_modalities.length > 0 && (
                <Stack direction="horizontal" align="center" justify="between" gap="tight">
                  <Text size="caption" color="muted">
                    Input
                  </Text>
                  <Text size="caption" mono>
                    {model.input_modalities.join(', ')}
                  </Text>
                </Stack>
              )}

              {/* Output modalities */}
              {model.output_modalities.length > 0 && (
                <Stack direction="horizontal" align="center" justify="between" gap="tight">
                  <Text size="caption" color="muted">
                    Output
                  </Text>
                  <Text size="caption" mono>
                    {model.output_modalities.join(', ')}
                  </Text>
                </Stack>
              )}
            </Stack>
          </CardContent>
        </Card>
      </article>
    </Stack>
  );
}

// ── ModelCardGrid ──────────────────────────────────────────────────────────────

interface ModelCardGridProps {
  /** Set of model IDs to display. */
  models: Set<string>;
  /** When provided the grid is interactive: each card shows a remove button. */
  onRemove?: (id: string) => void;
  title?: string;
  description?: string;
}

export function ModelCardGrid({
  models,
  onRemove,
  title = 'Favourites',
  description,
}: ModelCardGridProps) {
  const { models: allModels } = useModelsContext();
  const rows = allModels.filter((m) => models.has(m.id));

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

      {rows.length === 0 && onRemove ? (
        <Card>
          <CardContent>
            <Stack align="center" justify="center" gap="tight">
              <Heart className="size-8 text-border" />
              <Text size="supporting" color="muted">
                No favourites yet
              </Text>
              <Text size="caption" color="subtle">
                Click the heart icon on any model row to add it here.
              </Text>
            </Stack>
          </CardContent>
        </Card>
      ) : (
        <Grid cols={{ base: 1, sm: 2, lg: 3, xl: 4 }} gap="snug">
          {rows.map((model) => (
            <ModelCard key={model.id} model={model} onRemove={onRemove} />
          ))}
        </Grid>
      )}
    </Stack>
  );
}
