// ── Shared API types ─────────────────────────────────────────────────────────
//
// These interfaces mirror Go backend JSON response shapes exactly.
// Field names match json tags — do not rename without updating the backend.

export interface HealthResponse {
  status: string;
  /** Human-readable service name, e.g. "orbit-identity". */
  service: string;
  timestamp: string;
}

// ── Inference Gateway — /v1/models ────────────────────────────────────────────

/**
 * Content type that a model can accept as input or produce as output.
 * Mirrors Go: inferencegw.Modality
 */
export type Modality = 'text' | 'image' | 'embedding' | 'audio' | 'video';

/**
 * Wire protocol used to call a chat model.
 * Mirrors Go: inferencegw.APIDialect
 */
export type APIDialect = 'openai' | 'anthropic';

/**
 * Endpoint family a model belongs to.
 * Mirrors Go: inferencegw.ModelCapability
 *
 * Three-layer taxonomy (matches ADR-0026 / ADR-0027):
 *   ModelCapability — what endpoint family?    (chat, embedding, image, …)
 *   APIDialect      — which wire format?       (openai, anthropic) [chat only]
 *   Modality        — what physical I/O types? (text, image, embedding, …)
 */
export type ModelCapability =
  | 'chat' // POST /v1/chat/completions or /v1/messages
  | 'embedding' // POST /v1/embeddings (handler not yet active)
  | 'image' // POST /v1/images/generations (handler not yet active)
  | 'video' // async video generation (no standard endpoint yet)
  | 'rerank' // POST /v1/rerank (handler not yet active)
  | 'audio'; // TTS / live audio (no standard endpoint yet)

/** Raw model object returned by the inference gateway. */
export interface ModelObject {
  id: string;
  object: string;
  created: number;
  owned_by: string;
  display_name: string;
  aliases: string[];
  input_modalities: Modality[];
  output_modalities: Modality[];
  /** Only populated for models with CapabilityChat. */
  dialects: APIDialect[];
  capabilities: ModelCapability[];
}

/** Top-level response from GET /v1/models. */
export interface ModelsListResponse {
  object: 'list';
  data: ModelObject[];
}
