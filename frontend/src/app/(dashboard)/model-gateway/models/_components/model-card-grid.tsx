'use client';

import { Heart } from 'lucide-react';
import { Badge, Card, CardContent, CardHeader, CardTitle, Text, Heading } from '@orbit/terra';
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
    <div className="relative min-w-0">
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
          <div className="flex items-center gap-2 pr-6">
            <div className="min-w-0">
              <CardTitle>{model.name}</CardTitle>
              <Text size="caption" color="muted" mono truncate>
                {model.id}
              </Text>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col gap-2">
            {/* Availability */}
            <div className="flex items-center gap-2">
              <AvailabilityDot status={model.status} />
              <Text size="caption" color="muted">
                {model.status === 'available' ? 'Available' : 'Unavailable'}
              </Text>
            </div>

            {/* Capabilities */}
            {model.capabilities.length > 0 && (
              <div className="flex flex-wrap gap-1">
                {model.capabilities.map((cap) => (
                  <Badge key={cap} variant="outline" size="sm">
                    {cap}
                  </Badge>
                ))}
              </div>
            )}

            {/* Input modalities */}
            {model.input_modalities.length > 0 && (
              <div className="flex items-center justify-between gap-2">
                <Text size="caption" color="muted">
                  Input
                </Text>
                <Text size="caption" mono>
                  {model.input_modalities.join(', ')}
                </Text>
              </div>
            )}

            {/* Output modalities */}
            {model.output_modalities.length > 0 && (
              <div className="flex items-center justify-between gap-2">
                <Text size="caption" color="muted">
                  Output
                </Text>
                <Text size="caption" mono>
                  {model.output_modalities.join(', ')}
                </Text>
              </div>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
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
    <div className="flex flex-col gap-3">
      <div>
        <Heading variant="section">{title}</Heading>
        {description && (
          <div className="mt-0.5">
            <Text size="supporting" color="muted">
              {description}
            </Text>
          </div>
        )}
      </div>

      {rows.length === 0 && onRemove ? (
        <Card>
          <CardContent>
            <div className="flex flex-col items-center justify-center gap-2 py-8 text-center">
              <Heart className="size-8 text-border" />
              <Text size="supporting" color="muted">
                No favourites yet
              </Text>
              <Text size="caption" color="subtle">
                Click the heart icon on any model row to add it here.
              </Text>
            </div>
          </CardContent>
        </Card>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {rows.map((model) => (
            <ModelCard key={model.id} model={model} onRemove={onRemove} />
          ))}
        </div>
      )}
    </div>
  );
}
