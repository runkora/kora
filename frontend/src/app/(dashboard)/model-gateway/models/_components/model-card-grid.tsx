'use client';

import { Heart } from 'lucide-react';
import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
  Text,
  Heading,
} from '@orbit/terra';
import { MODELS, type ModelRow } from '../../_data/mock';
import { ProviderIcon } from './provider-icon';
import { AvailabilityDot } from './availability-dot';

// ── ModelCard ──────────────────────────────────────────────────────────────────

interface ModelCardProps {
  model: ModelRow;
  tag?: string;
  onRemove?: (id: string) => void;
}

function ModelCard({ model, tag, onRemove }: ModelCardProps) {
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
            <ProviderIcon provider={model.provider} size="md" />
            <div className="min-w-0">
              <CardTitle>{model.name}</CardTitle>
              <Text size="caption" color="muted" mono truncate>
                {model.id}
              </Text>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center gap-2">
              <AvailabilityDot status={model.status} />
              <Text size="caption" color="muted">
                {model.status === 'available' ? 'Available' : 'Unavailable'}
              </Text>
            </div>
            <div className="flex items-center justify-between">
              <Text size="caption" color="muted">
                Context
              </Text>
              <Text size="caption" mono>
                {model.contextK >= 1000 ? `${model.contextK / 1000}M` : `${model.contextK}k`}
              </Text>
            </div>
            <div className="flex items-center justify-between">
              <Text size="caption" color="muted">
                Prompt
              </Text>
              <Text size="caption" mono>
                ${model.promptPer1M.toFixed(2)}/1M
              </Text>
            </div>
            <div className="flex items-center justify-between">
              <Text size="caption" color="muted">
                Output
              </Text>
              <Text size="caption" mono>
                ${model.outputPer1M.toFixed(2)}/1M
              </Text>
            </div>
          </div>
          {tag && (
            <div className="mt-3 flex justify-end">
              <Badge variant="outline">{tag}</Badge>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// ── ModelCardGrid ──────────────────────────────────────────────────────────────

interface ModelCardGridProps {
  /** Set of model IDs to display. */
  models: Set<string>;
  /** Optional per-model "why" labels shown as a badge (e.g. "Best Value"). */
  tags?: Record<string, string>;
  /** When provided the grid is interactive: each card shows a remove button. */
  onRemove?: (id: string) => void;
  title?: string;
  description?: string;
}

export function ModelCardGrid({
  models,
  tags,
  onRemove,
  title = 'Favourites',
  description,
}: ModelCardGridProps) {
  const rows = MODELS.filter((m) => models.has(m.id));

  // Show empty state only when the widget is interactive (i.e. onRemove is present).
  if (rows.length === 0 && onRemove) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
          <CardDescription>
            {description ?? "Models you've starred for quick access"}
          </CardDescription>
        </CardHeader>
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
    );
  }

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
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
        {rows.map((model) => (
          <ModelCard key={model.id} model={model} tag={tags?.[model.id]} onRemove={onRemove} />
        ))}
      </div>
    </div>
  );
}
