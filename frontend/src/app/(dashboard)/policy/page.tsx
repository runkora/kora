import { redirect } from 'next/navigation';
import { headers } from 'next/headers';
import { APP_ROUTES, getDevEnvPrefix } from '@orbit/lib/routing';

export default async function PolicyIndexPage() {
  const prefix = getDevEnvPrefix(await headers());
  redirect(prefix + APP_ROUTES.policy.overview);
}
