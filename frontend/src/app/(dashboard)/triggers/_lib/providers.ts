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

/**
 * Providers that issue the source's secret, so the source is created without
 * one and the console takes it afterwards: what the secret is called, and where
 * the admin finds it.
 */
export const ISSUED_SECRETS: Record<string, { label: string; origin: string }> = {
  [SLACK_PROVIDER]: { label: 'signing secret', origin: 'Basic Information page of your Slack app' },
  [DISCORD_PROVIDER]: { label: 'bot token', origin: 'Bot page of your Discord application' },
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
