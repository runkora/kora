import { redirect } from 'next/navigation';
import { APP_ROUTES } from '@runkora/lib/routing';

export default function ModelGatewayIndex() {
  redirect(APP_ROUTES.modelGateway.overview);
}
