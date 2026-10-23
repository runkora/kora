/**
 * @tier 3 · triggers-scoped. The console's words for the chat command kind:
 * the chat events every chat platform maps to, and the chat platforms that are
 * planned but not built. The events and fields themselves come from the API.
 */

import type { IconName } from '@runkora/hades';

/** Readable text for the chat events in the chat schema. */
const CHAT_EVENTS: Record<string, { label: string; description: string; icon: IconName }> = {
  mention: {
    label: 'Mention',
    description: 'Someone mentions the bot in a channel it is in.',
    icon: 'users',
  },
  direct_message: {
    label: 'Direct message',
    description: 'Someone sends the bot a direct message.',
    icon: 'user',
  },
};

/** A chat event's label, e.g. "Direct message"; an unknown event reads as its id with spaces. */
export function chatEventLabel(eventType: string): string {
  return CHAT_EVENTS[eventType]?.label ?? eventType.replace(/_/g, ' ');
}

/** A chat event's one-line description; empty for an unknown event. */
export function chatEventDescription(eventType: string): string {
  return CHAT_EVENTS[eventType]?.description ?? '';
}

/** A chat event's icon. */
export function chatEventIcon(eventType: string): IconName {
  return CHAT_EVENTS[eventType]?.icon ?? 'send';
}

/**
 * The key that names one conversation for an event. A thread id is unique
 * only within its conversation, so a mention keys on both, and a direct
 * message conversation is one key.
 */
export function chatConversationKey(eventType: string): string {
  return eventType === 'direct_message'
    ? '{{ conversation.id }}'
    : '{{ conversation.id }}:{{ thread.id }}';
}

/** Chat platforms that are planned. Shown disabled in the platform choice. */
export const LATER_CHAT_PLATFORMS: { name: string; icon: IconName }[] = [
  { name: 'Microsoft Teams', icon: 'send' },
];

/** The placeholder syntax the backend reads, so the console names the same keys it does. */
const PLACEHOLDER = /\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}/g;

/** The distinct keys a template's placeholders name. */
export function placeholderKeys(template: string): string[] {
  return [...new Set([...template.matchAll(PLACEHOLDER)].map((m) => m[1] ?? ''))];
}

/**
 * The first key a chat trigger may not read, or undefined when all are fields.
 * Mirrors the backend's check: a key must be a field path, and never a parent
 * such as `user`, which would render every key a platform puts under it.
 */
export function firstUnknownChatField(
  keys: readonly string[],
  fields: readonly string[],
): string | undefined {
  return keys.find(
    (key) => !fields.includes(key) || fields.some((path) => path.startsWith(`${key}.`)),
  );
}
