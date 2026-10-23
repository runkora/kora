/**
 * @tier 3 · triggers-scoped. How each source provider is shown. Which trigger
 * kind lists a provider comes from the category its event types report, never
 * from this list.
 */

import type { IconName } from '@runkora/hades';
import type { SourceCategory } from '../_types';
import { DISCORD_PROVIDER } from './discord-invite';
import { SLACK_PROVIDER } from './slack-app-manifest';

export const PROVIDERS: { id: string; name: string; description: string; icon: IconName }[] = [
  {
    id: 'github',
    name: 'GitHub',
    description: 'Repository and organisation webhooks, verified by HMAC signature.',
    icon: 'code-2',
  },
  {
    id: 'alertmanager',
    name: 'Prometheus Alertmanager',
    description: 'Alert notifications, verified by bearer token.',
    icon: 'bell',
  },
  {
    id: SLACK_PROVIDER,
    name: 'Slack',
    description: 'Mentions and direct messages to a Slack app, verified by its signing secret.',
    icon: 'send',
  },
  {
    id: DISCORD_PROVIDER,
    name: 'Discord',
    description: 'Mentions and direct messages to a Discord bot, connected with its bot token.',
    icon: 'send',
  },
];

/** What a provider that issues the source's secret calls it, and the words around it. */
export interface IssuedSecret {
  /** The field label and heading, e.g. "Signing secret". */
  title: string;
  /** The secret in a sentence, e.g. "signing secret". */
  label: string;
  /** The secret in a button, e.g. "secret". */
  short: string;
  /** The field hint while the source waits for its first secret. */
  waitingHint: string;
  /** The field hint once a secret is stored. */
  replaceHint: string;
  /** What the admin is told after replacing the secret. */
  replacedNote: string;
}

/**
 * Providers that issue the source's secret, so the source is created without
 * one and the console takes it afterwards.
 */
export const ISSUED_SECRETS: Record<string, IssuedSecret> = {
  [SLACK_PROVIDER]: {
    title: 'Signing secret',
    label: 'signing secret',
    short: 'secret',
    waitingHint:
      "Waiting for the signing secret. Copy it from your Slack app's Basic Information page and paste it here.",
    replaceHint: 'Regenerate it in your Slack app, then paste the new secret here.',
    replacedNote: 'The new secret is stored. Requests signed with the old one are rejected.',
  },
  [DISCORD_PROVIDER]: {
    title: 'Bot token',
    label: 'bot token',
    short: 'token',
    waitingHint:
      'Waiting for the bot token. Copy it from the Bot page of your Discord application and paste it here.',
    replaceHint:
      'Reset the token on the Bot page of your Discord application, then paste the new bot token here.',
    replacedNote: 'The new token is stored. The old one stops working and the bot reconnects.',
  },
};

export const PROVIDER_IDS = PROVIDERS.map((p) => p.id);

/** An example of sender ids, per source category, for the sender restriction field. */
export const SENDER_ID_EXAMPLE: Record<SourceCategory, string> = {
  webhook: 'octocat, hubot',
  chat: 'U012AB3CD, U045EF6GH',
};

/** A provider's display name; an unknown provider shows its id. */
export function providerName(id: string): string {
  return PROVIDERS.find((p) => p.id === id)?.name ?? id;
}
