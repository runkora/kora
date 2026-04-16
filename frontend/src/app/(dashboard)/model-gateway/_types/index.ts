// ── Feature-local types ───────────────────────────────────────────────────────
//
// Single import point for all types used across the model-gateway feature.
// Components import from here, never directly from `_data/mock`.

import type {
  ModelObject,
  ModelsListResponse,
  Modality,
  APIDialect,
  ModelCapability,
} from '@orbit/shared/api';

export type { ModelObject, ModelsListResponse, Modality, APIDialect, ModelCapability };

// ── Value-type unions ─────────────────────────────────────────────────────────

export type ModelStatus = 'available' | 'unavailable';

// ── ModelRow ──────────────────────────────────────────────────────────────────
//
// Display-layer shape derived from the backend ModelObject. All UI components
// work with ModelRow; the raw ModelObject is only used in toModelRow and the
// API service layer.

export interface ModelRow {
  // ── Identity (always populated from the API) ─────────────────────────────
  id: string;
  name: string; // display_name ?? id
  owned_by: string; // hosting provider: "bedrock" | "vertexai" | "azure"
  family: string; // vendor/model-family: "anthropic" | "google" | "meta" | …
  aliases: string[];

  // ── Capabilities (always populated from the API) ──────────────────────────
  input_modalities: Modality[];
  output_modalities: Modality[];
  dialects: APIDialect[];
  capabilities: ModelCapability[];

  // ── Availability ──────────────────────────────────────────────────────────
  status: ModelStatus;

  // ── UI state ──────────────────────────────────────────────────────────────
  favourite: boolean;
}

// ── Transformer ───────────────────────────────────────────────────────────────

export function toModelRow(m: ModelObject): ModelRow {
  return {
    id: m.id,
    name: m.display_name || m.id,
    owned_by: m.owned_by,
    family: m.family ?? '',
    aliases: m.aliases ?? [],
    input_modalities: m.input_modalities ?? [],
    output_modalities: m.output_modalities ?? [],
    dialects: m.dialects ?? [],
    capabilities: m.capabilities ?? [],
    status: 'available',
    favourite: false,
  };
}
