/**
 * @tier 3 · triggers-scoped. The Slack app manifest an admin pastes into Slack
 * to create the app behind a Slack trigger source. Built here from the brand's
 * product name so the app is named after the product it talks to.
 */

import { endpointFor } from './endpoints';

/**
 * The Slack provider id. Slack issues a source's signing secret, so the console
 * asks for it at create and rotate instead of showing a generated one.
 */
export const SLACK_PROVIDER = 'slack';

/** Stands in for the source id in the request URL until the source exists. */
export const SLACK_SOURCE_ID_PLACEHOLDER = 'SOURCE_ID';

/** Slack's limit on an app's display name. */
const MAX_APP_NAME = 35;

export interface SlackAppManifest {
  display_information: { name: string };
  features: {
    app_home: { messages_tab_enabled: boolean; messages_tab_read_only_enabled: boolean };
    bot_user: { display_name: string };
  };
  oauth_config: { scopes: { bot: string[] } };
  settings: {
    event_subscriptions: { request_url: string; bot_events: string[] };
    org_deploy_enabled: boolean;
    socket_mode_enabled: boolean;
    token_rotation_enabled: boolean;
  };
}

/** Slack's limit on a bot's display name. */
const MAX_BOT_NAME = 80;

/**
 * The product name in the characters Slack allows in a bot's display name.
 * Accents are dropped rather than replaced, and a name with no usable
 * character falls back to "bot", since Slack rejects an empty one.
 */
export function slackBotDisplayName(productName: string): string {
  const slug = productName
    .normalize('NFKD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, MAX_BOT_NAME);
  return slug || 'bot';
}

export function slackAppManifest(productName: string, ingestHost: string): SlackAppManifest {
  return {
    display_information: {
      // Cut by code point so a name is never split inside a character.
      name: Array.from(productName.trim()).slice(0, MAX_APP_NAME).join(''),
    },
    features: {
      // The messages tab lets people send the bot direct messages.
      app_home: { messages_tab_enabled: true, messages_tab_read_only_enabled: false },
      bot_user: { display_name: slackBotDisplayName(productName) },
    },
    oauth_config: {
      scopes: {
        bot: [
          // The trigger source hears mentions and direct messages.
          'app_mentions:read',
          'im:history',
          // The agent reads the thread and replies through the Slack MCP server.
          'chat:write',
          'channels:history',
          'groups:history',
          'users:read',
        ],
      },
    },
    settings: {
      event_subscriptions: {
        request_url: endpointFor(ingestHost, SLACK_SOURCE_ID_PLACEHOLDER),
        bot_events: ['app_mention', 'message.im'],
      },
      org_deploy_enabled: false,
      socket_mode_enabled: false,
      token_rotation_enabled: false,
    },
  };
}
