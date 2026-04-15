import { redirect } from 'next/navigation';
import { APP_ROUTES } from '@orbit/lib/routing';

export default function PolicyIndexPage() {
  redirect(APP_ROUTES.policy.overview);
}
