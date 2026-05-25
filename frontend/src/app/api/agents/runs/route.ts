import 'server-only';
import { NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { MOCK_RUNS } from '../_mock/data';

export const GET = withAuth(async () => {
  return NextResponse.json({ data: MOCK_RUNS });
});
