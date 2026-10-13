/**
 * Route: GET /theme/<hash>/assets/<path>
 *
 * Serves one asset of the loaded theme: a logo, a favicon or a font. The hash
 * is the loaded theme's content hash, so the URL changes with every bundle and
 * the response can be cached for a year as immutable. Anything else is a 404:
 * a different hash, a path the theme does not reference, or no theme at all.
 * The session proxy exempts this prefix so the login page gets its brand
 * before a session exists. See the white-labelling theme ADR, decision 4.
 */
import 'server-only';
import { NextResponse } from 'next/server';
import { getThemeAsset } from '@orbit/server/theme';

interface RouteContext {
  params: Promise<{ hash: string; path: string[] }>;
}

export async function GET(_request: Request, context: RouteContext): Promise<NextResponse> {
  const { hash, path } = await context.params;
  const asset = await getThemeAsset(hash, path.join('/'));
  if (!asset) {
    return new NextResponse(null, { status: 404 });
  }
  return new NextResponse(Buffer.from(asset.bytes), {
    status: 200,
    headers: {
      'Content-Type': asset.contentType,
      'Content-Length': String(asset.bytes.byteLength),
      'Cache-Control': 'public, max-age=31536000, immutable',
    },
  });
}
