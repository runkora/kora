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

export type ActorKind = 'person' | 'bot' | 'system';

export const ACTOR_KINDS: { value: ActorKind; label: string }[] = [
  { value: 'person', label: 'Person' },
  { value: 'bot', label: 'Bot' },
  { value: 'system', label: 'System' },
];

/** The `attribution` block of a generic source's config, as the API stores it. */
export type GenericAttribution = {
  links?: { label: string; url: string }[];
  actor?: { name: string; kind: ActorKind; url?: string };
  object?: { label: string; url?: string };
  /** Event type to the verb the run page shows for it. */
  verbs?: Record<string, string>;
};

/** The attribution form's state. Every string but the actor kind is a template. */
export interface AttributionDraft {
  links: { label: string; url: string }[];
  actor: { name: string; kind: ActorKind | ''; url: string };
  object: { label: string; url: string };
  verbs: { eventType: string; verb: string }[];
}

export const EMPTY_ATTRIBUTION: AttributionDraft = {
  links: [],
  actor: { name: '', kind: '', url: '' },
  object: { label: '', url: '' },
  verbs: [],
};

/** The backend keeps at most this many links per source. */
export const MAX_ATTRIBUTION_LINKS = 10;

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
  attribution: AttributionDraft;
}

export const EMPTY_GENERIC_DRAFT: GenericSourceDraft = {
  method: '',
  header: '',
  prefix: '',
  encoding: 'hex',
  secretChoice: 'generate',
  eventLocation: 'none',
  eventKey: '',
  attribution: EMPTY_ATTRIBUTION,
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
  const attribution = attributionFrom(draft.attribution);
  return {
    verification,
    ...(draft.eventLocation !== 'none' && { event_type: { [draft.eventLocation]: key } }),
    ...(Object.keys(attribution).length > 0 && { attribution }),
  };
}

/** The attribution block for the draft, trimmed. Empty when nothing is set, which removes it on update. */
export function attributionFrom(draft: AttributionDraft): GenericAttribution {
  const links = draft.links.map((l) => ({ label: l.label.trim(), url: l.url.trim() }));
  const actorName = draft.actor.name.trim();
  const actorUrl = draft.actor.url.trim();
  const objectLabel = draft.object.label.trim();
  const objectUrl = draft.object.url.trim();
  const verbs = draft.verbs.map((v) => [v.eventType.trim(), v.verb.trim()] as const);
  return {
    ...(links.length > 0 && { links }),
    ...(actorName !== '' && {
      actor: {
        name: actorName,
        kind: draft.actor.kind as ActorKind,
        ...(actorUrl !== '' && { url: actorUrl }),
      },
    }),
    ...(objectLabel !== '' && {
      object: { label: objectLabel, ...(objectUrl !== '' && { url: objectUrl }) },
    }),
    ...(verbs.length > 0 && { verbs: Object.fromEntries(verbs) }),
  };
}

const text = (value: unknown): string => (typeof value === 'string' ? value : '');

/** The form state for a stored source's attribution block. */
export function attributionDraftOf(config: Record<string, unknown> | undefined): AttributionDraft {
  const block = objectAt(config, 'attribution');
  if (block === undefined) return EMPTY_ATTRIBUTION;
  const links = Array.isArray(block.links) ? (block.links as Record<string, unknown>[]) : [];
  const actor = objectAt(block, 'actor');
  const object = objectAt(block, 'object');
  const kind = ACTOR_KINDS.find((k) => k.value === actor?.kind)?.value ?? '';
  return {
    links: links.map((l) => ({ label: text(l?.label), url: text(l?.url) })),
    actor: { name: text(actor?.name), kind, url: text(actor?.url) },
    object: { label: text(object?.label), url: text(object?.url) },
    verbs: Object.entries(objectAt(block, 'verbs') ?? {}).map(([eventType, verb]) => ({
      eventType,
      verb: text(verb),
    })),
  };
}

/** The placeholder syntax the backend substitutes, `templateVarPattern` in template.go. */
const PLACEHOLDER = /\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}/g;

/** The backend's limits on a URL template, in bytes, and on every other string, in runes. */
const MAX_URL_BYTES = 2048;
const MAX_TEXT_RUNES = 300;

/** Whether a template has a malformed path, or a `{{` left over once the placeholders are taken out. */
const badPlaceholder = (t: string) =>
  [...t.matchAll(PLACEHOLDER)].some((m) => !FIELD_PATH.test(m[1]!)) ||
  t.replace(PLACEHOLDER, '').includes('{{');
const urlTooLong = (u: string) => new TextEncoder().encode(u.trim()).length > MAX_URL_BYTES;
export const textTooLong = (t: string) => [...t.trim()].length > MAX_TEXT_RUNES;

/** Whether one template breaks its own rules: the placeholder grammar or its length limit. */
export function templateInvalid(value: string, url: boolean): boolean {
  return badPlaceholder(value) || (url ? urlTooLong(value) : textTooLong(value));
}

/** Why the attribution cannot be saved, or undefined when it can. Mirrors ADR Decision 3. */
export function attributionHint(draft: AttributionDraft): string | undefined {
  const { links, actor, object, verbs } = draft;
  if (links.length > MAX_ATTRIBUTION_LINKS)
    return `A source can have at most ${MAX_ATTRIBUTION_LINKS} links.`;
  if (links.some((l) => !l.label.trim() || !l.url.trim()))
    return 'Give every link a label and a URL.';
  if (actor.url.trim() && !actor.name.trim()) return 'Name the actor, or clear its URL.';
  if (actor.kind !== '' && !actor.name.trim()) return 'Name the actor, or clear its kind.';
  if (object.url.trim() && !object.label.trim()) return 'Label the object, or clear its URL.';
  if (!actor.name.trim() !== !object.label.trim())
    return 'Set the actor name and the object label together.';
  if (actor.name.trim() && actor.kind === '') return 'Choose what kind of actor it is.';
  if (verbs.some((v) => !v.eventType.trim() || !v.verb.trim()))
    return 'Give every verb an event type and a verb.';
  if (new Set(verbs.map((v) => v.eventType.trim())).size < verbs.length)
    return 'Map each event type to one verb.';

  const urls = [...links.map((l) => l.url), actor.url, object.url];
  const texts = [
    ...links.map((l) => l.label),
    actor.name,
    object.label,
    ...verbs.flatMap((v) => [v.eventType, v.verb]),
  ];
  const templates = [...urls, ...links.map((l) => l.label), actor.name, object.label];
  if (templates.some(badPlaceholder))
    return 'Write each placeholder as a dotted payload path, such as {{ data.id }}.';
  if (urls.some(urlTooLong))
    return `A URL template can be at most ${MAX_URL_BYTES.toLocaleString('en')} bytes.`;
  if (texts.some(textTooLong))
    return `A label, name or verb can be at most ${MAX_TEXT_RUNES} characters.`;
  return undefined;
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
  return attributionHint(draft.attribution);
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
