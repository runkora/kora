// RSC — an old Skill address. A Skill now opens in place on the overview, so
// the address never names it. Old and shared links open the default overview.

import { redirect } from 'next/navigation';
import { APP_ROUTES } from '@orbit/lib/routing';

export default function OldSkillAddressPage(): never {
  redirect(APP_ROUTES.marketplace.overview);
}
