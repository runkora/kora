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
  /** Canonical vendor/model-family identifier used for policy scope (e.g. "anthropic", "google", "meta"). */
  family: string;
  display_name: string;
  status: 'available' | 'unavailable';
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

// ── Identity — /api/v1/identity/tokens ───────────────────────────────────────

/**
 * A Personal Access Token summary as returned by LIST.
 * The raw token value is never included — it is only returned once on creation.
 * Mirrors Go: identity.PATSummary
 */
export interface PAT {
  id: string;
  name: string;
  expires_at: string;
  created_at: string;
}

/** Request body for creating a new PAT. Mirrors Go: identity.CreatePATRequest */
export interface CreatePATRequest {
  name: string;
  /** Optional. Defaults to 90 days on the backend. Max 365. */
  expires_in_days?: number;
}

/**
 * Response from POST /tokens — includes the raw `orb_<jwt>` token.
 * This is the only time the token is returned; it cannot be retrieved again.
 * Mirrors Go: identity.CreatePATResponse
 */
export interface CreatePATResponse {
  id: string;
  name: string;
  /** Raw PAT string (`orb_<jwt>`). Store it securely — shown once only. */
  token: string;
  expires_at: string;
  created_at: string;
}

/** Top-level response from GET /tokens. Mirrors Go: identity.ListPATsResponse */
export interface ListPATsResponse {
  tokens: PAT[];
}
