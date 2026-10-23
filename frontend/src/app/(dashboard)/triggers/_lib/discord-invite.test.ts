import { describe, expect, it } from 'vitest';

import { DISCORD_PERMISSIONS, discordInviteUrl } from './discord-invite';

describe('discordInviteUrl', () => {
  it('builds the bot invite link for an application id', () => {
    expect(discordInviteUrl('123456789012345678')).toBe(
      'https://discord.com/oauth2/authorize?client_id=123456789012345678&scope=bot&permissions=274877975552',
    );
  });

  it('trims the id', () => {
    expect(discordInviteUrl('  123456789012345678 ')).toContain('client_id=123456789012345678&');
  });

  it('returns null for an empty or non numeric id', () => {
    expect(discordInviteUrl('')).toBeNull();
    expect(discordInviteUrl('   ')).toBeNull();
    expect(discordInviteUrl('12ab')).toBeNull();
    expect(discordInviteUrl('1&scope=applications.commands')).toBeNull();
  });

  it('asks for the four permissions and nothing else', () => {
    const bits = [1024, 2048, 65536, 274877906944];
    expect(bits.reduce((sum, bit) => sum + bit, 0)).toBe(DISCORD_PERMISSIONS);
  });

  it('asks for the bot scope only', () => {
    const url = new URL(discordInviteUrl('1') ?? '');
    expect(url.searchParams.get('scope')).toBe('bot');
  });
});
