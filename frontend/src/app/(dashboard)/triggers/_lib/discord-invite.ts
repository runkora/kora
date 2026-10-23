/**
 * @tier 3 · triggers-scoped. The invite link an admin opens to add the Discord
 * bot behind a Discord trigger source to a server. Built in the browser from
 * the application id; the id is never sent to the backend.
 */

/**
 * The Discord provider id. A Discord source is created without a token and the
 * console then takes the bot token, as for Slack's signing secret.
 */
export const DISCORD_PROVIDER = 'discord';

/**
 * The permissions the bot is invited with: View Channels (1024), Send Messages
 * (2048), Read Message History (65536) and Send Messages in Threads
 * (274877906944), since the agent replies in the thread it was mentioned in.
 */
export const DISCORD_PERMISSIONS = 274877975552;

/** The invite link for an application id, or null when the id is not a Discord snowflake. */
export function discordInviteUrl(applicationId: string): string | null {
  const id = applicationId.trim();
  if (!/^\d{1,20}$/.test(id)) return null;
  return `https://discord.com/oauth2/authorize?client_id=${id}&scope=bot&permissions=${DISCORD_PERMISSIONS}`;
}
