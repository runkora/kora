import { redirect } from 'next/navigation';
import { headers } from 'next/headers';
import { APP_ROUTES, getDevEnvPrefix } from '@orbit/lib/routing';

export default async function ModelGatewayIndex() {
  const prefix = getDevEnvPrefix(await headers());
  redirect(prefix + APP_ROUTES.modelGateway.overview);
}
