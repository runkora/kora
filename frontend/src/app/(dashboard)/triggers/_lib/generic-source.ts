/**
 * @tier 3 · triggers-scoped. The generic webhook source: the draft the source
 * step collects, the config the API stores, and what the console reads back.
 */

import { FIELD_PATH } from './field-path';

/** The provider id of a generic webhook source. */
export const GENERIC_PROVIDER = 'generic';

/** The event type of every delivery to a generic source with no event type location. */
export const GENERIC_EVENT_TYPE = 'webhook';

export type VerificationMethod = 'bearer' | 'hmac_sha256' | 'standard_webhooks';
export type SignatureEncoding = 'hex' | 'base64';
/** Whether the platform generates the secret or the admin pastes the sender's. */
export type SecretChoice = 'generate' | 'paste';
export type EventTypeLocation = 'none' | 'header' | 'path';

export const VERIFICATION_METHODS: {
  value: VerificationMethod;
  title: string;
  description: string;
}[] = [
  {
    value: 'hmac_sha256',
    title: 'HMAC-SHA256',
    description: 'A signature over the raw body, in a header you name.',
  },
  {
    value: 'standard_webhooks',
    title: 'Standard Webhooks',
    description: 'The webhook-id, webhook-timestamp and webhook-signature headers.',
  },
  {
    value: 'bearer',
    title: 'Bearer token',
    description: 'A token in the Authorization header. Always generated.',
  },
];

/** What the source step collects for a new generic source. */
export interface GenericSourceDraft {
  method: VerificationMethod | '';
  /** The signature header, for `hmac_sha256`. */
  header: string;
  prefix: string;
  encoding: SignatureEncoding;
  secretChoice: SecretChoice;
  eventLocation: EventTypeLocation;
  /** The header name or payload path the event type is read from. */
  eventKey: string;
}

export const EMPTY_GENERIC_DRAFT: GenericSourceDraft = {
  method: '',
  header: '',
  prefix: '',
  encoding: 'hex',
  secretChoice: 'generate',
  eventLocation: 'none',
  eventKey: '',
};

/** An HTTP header name: one RFC 9110 token. */
const HEADER_NAME = /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/;

/** Headers that carry credentials or signatures; the backend refuses them as the event type header. */
const RESERVED_EVENT_HEADERS = [
  'authorization',
  'webhook-id',
  'webhook-timestamp',
  'webhook-signature',
];

/** Whether `name` may hold the event type: not a reserved header nor the signature header. */
function eventHeaderAllowed(name: string, signatureHeader: string): boolean {
  const lower = name.toLowerCase();
  return !RESERVED_EVENT_HEADERS.includes(lower) && lower !== signatureHeader.trim().toLowerCase();
}

/** The backend refuses a longer prefix. */
const MAX_PREFIX_BYTES = 32;

/** Whether the method lets the admin paste a secret the sender issued. */
export function takesPastedSecret(method: VerificationMethod | ''): boolean {
  return method === 'hmac_sha256' || method === 'standard_webhooks';
}

/** Whether the draft sends a pasted secret, rather than asking for a generated one. */
export function pastesSecret(draft: GenericSourceDraft): boolean {
  return takesPastedSecret(draft.method) && draft.secretChoice === 'paste';
}

/** The config the API stores for the draft. */
export function genericConfigFrom(draft: GenericSourceDraft): Record<string, unknown> {
  const verification: Record<string, string> = { method: draft.method };
  if (draft.method === 'hmac_sha256') {
    verification.header = draft.header.trim();
    if (draft.prefix !== '') verification.prefix = draft.prefix;
    verification.encoding = draft.encoding;
  }
  const key = draft.eventKey.trim();
  return {
    verification,
    ...(draft.eventLocation !== 'none' && { event_type: { [draft.eventLocation]: key } }),
  };
}

/** Why the draft cannot be created yet, or undefined when it can. */
export function genericDraftHint(draft: GenericSourceDraft, secret: string): string | undefined {
  if (draft.method === '') return 'Choose how deliveries are verified.';
  if (draft.method === 'hmac_sha256') {
    if (!draft.header.trim()) return 'Name the header that carries the signature.';
    if (!HEADER_NAME.test(draft.header.trim()))
      return 'The signature header must be a valid HTTP header name.';
    if (new TextEncoder().encode(draft.prefix).length > MAX_PREFIX_BYTES)
      return 'The signature prefix can be at most 32 bytes.';
  }
  if (pastesSecret(draft) && !secret.trim()) return "Paste the sender's signing secret.";
  if (draft.eventLocation === 'header') {
    const header = draft.eventKey.trim();
    if (!HEADER_NAME.test(header)) return 'Name a valid HTTP header for the event type.';
    if (!eventHeaderAllowed(header, draft.method === 'hmac_sha256' ? draft.header : ''))
      return 'The event type header cannot be a credential or signature header.';
  } else if (draft.eventLocation === 'path') {
    if (!FIELD_PATH.test(draft.eventKey.trim()))
      return 'Name the event type field as a dotted path, such as type or data.action.';
  }
  return undefined;
}

/** The event type the trigger step starts with for a generic source. */
export function defaultEventType(location: EventTypeLocation): string {
  return location === 'none' ? GENERIC_EVENT_TYPE : '';
}

function objectAt(value: unknown, key: string): Record<string, unknown> | undefined {
  if (typeof value !== 'object' || value === null) return undefined;
  const child = (value as Record<string, unknown>)[key];
  return typeof child === 'object' && child !== null
    ? (child as Record<string, unknown>)
    : undefined;
}

/** A stored generic source's verification method; undefined for any other source. */
export function verificationMethodOf(
  provider: string,
  config: Record<string, unknown> | undefined,
): VerificationMethod | undefined {
  if (provider !== GENERIC_PROVIDER) return undefined;
  const method = objectAt(config, 'verification')?.method;
  return VERIFICATION_METHODS.some((m) => m.value === method)
    ? (method as VerificationMethod)
    : undefined;
}

/** The event type every delivery carries, for a generic source with no event type location. */
export function fixedEventType(
  provider: string,
  config: Record<string, unknown> | undefined,
): string | undefined {
  if (provider !== GENERIC_PROVIDER) return undefined;
  return objectAt(config, 'event_type') === undefined ? GENERIC_EVENT_TYPE : undefined;
}

const COUNTED =
  'are rejected before any run starts, and are counted on the source rather than recorded as deliveries.';

/** How the drawer describes a generic source's verification. */
export function verificationNote(method: VerificationMethod): string {
  switch (method) {
    case 'bearer':
      return `Deliveries are verified by the bearer token in the Authorization header. Requests without it ${COUNTED}`;
    case 'standard_webhooks':
      return `Deliveries are verified with a Standard Webhooks signature. Unsigned requests and requests more than five minutes old ${COUNTED}`;
    case 'hmac_sha256':
      return `Deliveries are verified with an HMAC-SHA256 signature over the raw body. Unsigned requests ${COUNTED}`;
  }
}
