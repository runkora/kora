import { describe, expect, it } from 'vitest';

import { slackAppManifest, slackBotDisplayName } from './slack-app-manifest';

describe('slackAppManifest', () => {
  it('names the app and the bot after the product', () => {
    const manifest = slackAppManifest('Acme Agents');
    expect(manifest.display_information.name).toBe('Acme Agents');
    expect(manifest.features.bot_user.display_name).toBe('acme-agents');
  });

  it('cuts the app name to 35 characters', () => {
    const name = slackAppManifest('A'.repeat(50)).display_information.name;
    expect(name).toBe('A'.repeat(35));
  });

  it('cuts the app name by character, never inside one', () => {
    const name = slackAppManifest('😀'.repeat(40)).display_information.name;
    expect(Array.from(name)).toHaveLength(35);
    expect(name).toBe('😀'.repeat(35));
  });

  it('keeps the bot name to the characters Slack allows', () => {
    expect(slackBotDisplayName('  Ops: Bot (Prod) v2.1_x  ')).toBe('ops-bot-prod-v2.1_x');
    expect(slackBotDisplayName('Ünïcode Name')).toBe('unicode-name');
    expect(slackBotDisplayName('エージェント')).toBe('bot');
    expect(slackBotDisplayName('a'.repeat(100))).toHaveLength(80);
  });

  it('asks for the scopes the source and the replying agent need', () => {
    expect(slackAppManifest('Acme').oauth_config.scopes.bot).toEqual([
      'app_mentions:read',
      'im:history',
      'chat:write',
      'channels:history',
      'groups:history',
      'users:read',
    ]);
  });

  it('subscribes to mentions and direct messages with no request URL', () => {
    const { settings, features } = slackAppManifest('Acme');
    expect(settings.event_subscriptions).toEqual({ bot_events: ['app_mention', 'message.im'] });
    expect(JSON.stringify(slackAppManifest('Acme'))).not.toContain('request_url');
    expect(features.app_home).toEqual({
      messages_tab_enabled: true,
      messages_tab_read_only_enabled: false,
    });
    expect(settings.socket_mode_enabled).toBe(false);
    expect(settings.org_deploy_enabled).toBe(false);
    expect(settings.token_rotation_enabled).toBe(false);
  });
});
