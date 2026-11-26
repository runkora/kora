// The console opens on the Runs tab.

import { redirect } from 'next/navigation';
import { APP_ROUTES } from '@runkora/lib/routing';

export default function HomeRoute() {
  redirect(APP_ROUTES.runs);
}
