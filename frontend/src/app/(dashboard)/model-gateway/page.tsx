import { redirect } from 'next/navigation';
import { APP_ROUTES } from '@orbit/lib/routing';

export default function ModelGatewayIndex() {
  redirect(APP_ROUTES.modelGateway.overview);
}
