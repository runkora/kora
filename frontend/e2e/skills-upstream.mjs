/**
 * Test-only stand-in for skills-service and for the identity preferences the Favourites view reads,
 * reached by Next.js server fetches that page.route cannot see.
 */
import { createServer } from 'node:http';
import { BROKEN, MARKETPLACES, SKILLS, UNAVAILABLE_MESSAGE } from './skills-fixtures.ts';

const port = Number(process.env.PLAYWRIGHT_SKILLS_PORT ?? '3101');
const unavailable = { code: 'marketplace_unavailable', message: UNAVAILABLE_MESSAGE };
// One preferences document per session token, as identity keeps one per user.
const preferences = new Map();

createServer((req, res) => {
  const url = new URL(req.url, 'http://localhost');
  const json = (status, body) => {
    res.writeHead(status, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(body));
  };

  if (url.pathname === '/health') return json(200, {});
  if (!req.headers.authorization?.startsWith('Bearer ')) return json(401, { code: 'unauthorized' });
  if (url.pathname === '/identity/preferences') {
    const user = req.headers.authorization;
    if (req.method !== 'PUT') return json(200, preferences.get(user) ?? {});
    let body = '';
    req.on('data', (chunk) => (body += chunk));
    req.on('end', () => {
      preferences.set(user, JSON.parse(body || '{}'));
      json(200, {});
    });
    return;
  }
  if (!url.pathname.startsWith('/v1/')) return json(404, { code: 'not_found' });
  if (url.pathname === '/v1/marketplaces') return json(200, { marketplaces: MARKETPLACES });

  if (url.pathname === '/v1/marketplaces/skills') {
    const slug = url.searchParams.get('marketplace') ?? '';
    const keys = (url.searchParams.get('keys') ?? '').split(',').filter(Boolean);
    const search = (url.searchParams.get('search') ?? '').toLowerCase();
    const offset = Number(url.searchParams.get('offset') ?? '0');
    const limit = Number(url.searchParams.get('limit') ?? '10');
    if (slug === BROKEN.slug) return json(502, unavailable);
    const matched = SKILLS.map((entry) => entry.summary)
      .filter(
        (s) =>
          (!slug || s.marketplace === slug) &&
          (keys.length === 0 || keys.includes(`${s.marketplace}:${s.path}`)) &&
          (!search || `${s.title} ${s.name} ${s.description}`.toLowerCase().includes(search)),
      )
      .sort((a, b) => a.title.localeCompare(b.title, 'en', { sensitivity: 'base' }));
    const everyMarketplace = !slug && keys.length === 0;
    return json(200, {
      skills: matched.slice(offset, offset + limit),
      total: matched.length,
      offset,
      limit,
      ...(everyMarketplace && {
        failures: [{ marketplace: BROKEN.slug, message: UNAVAILABLE_MESSAGE }],
      }),
    });
  }

  const [, , slug, , ...path] = url.pathname.split('/').filter(Boolean).map(decodeURIComponent);
  if (slug === BROKEN.slug) return json(502, unavailable);
  const found = SKILLS.find(
    (entry) => entry.detail.marketplace === slug && entry.detail.path === path.join('/'),
  );
  if (!found) return json(404, { code: 'not_found', message: 'Skill not found.' });
  const file = url.searchParams.get('file');
  if (file === null) return json(200, found.detail);
  return file in found.bodies
    ? json(200, { path: file, content: found.bodies[file] })
    : json(404, { code: 'not_found', message: 'File not found.' });
}).listen(port, '127.0.0.1');
