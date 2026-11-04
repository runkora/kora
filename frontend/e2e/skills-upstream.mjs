/** Test-only stand-in for skills-service, reached by Next.js server fetches that page.route cannot see. */
import { createServer } from 'node:http';
import { BROKEN, MARKETPLACES, SKILLS, UNAVAILABLE_MESSAGE } from './skills-fixtures.ts';

const port = Number(process.env.PLAYWRIGHT_SKILLS_PORT ?? '3101');

createServer((req, res) => {
  const url = new URL(req.url, 'http://localhost');
  const json = (status, body) => {
    res.writeHead(status, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(body));
  };

  if (url.pathname === '/health') return json(200, {});
  if (!req.headers.authorization?.startsWith('Bearer ')) return json(401, { code: 'unauthorized' });
  if (url.pathname === '/v1/marketplaces') return json(200, { marketplaces: MARKETPLACES });

  const [, , slug, resource, ...path] = url.pathname
    .split('/')
    .filter(Boolean)
    .map(decodeURIComponent);
  const marketplace = MARKETPLACES.find((entry) => entry.slug === slug);
  if (!marketplace) return json(404, { code: 'not_found', message: 'Marketplace not found.' });
  if (marketplace === BROKEN) {
    return json(502, { code: 'marketplace_unavailable', message: UNAVAILABLE_MESSAGE });
  }
  const skills = SKILLS[slug] ?? [];

  if (resource === 'skills') {
    const found = skills.find((entry) => entry.path === path.join('/'));
    return found ? json(200, found) : json(404, { code: 'not_found', message: 'Skill not found.' });
  }

  const search = (url.searchParams.get('search') ?? '').toLowerCase();
  const offset = Number(url.searchParams.get('offset') ?? '0');
  const limit = Number(url.searchParams.get('limit') ?? '25');
  const matched = skills
    .filter(
      (entry) => !search || `${entry.name} ${entry.description}`.toLowerCase().includes(search),
    )
    .sort((a, b) => a.name.localeCompare(b.name, 'en'));
  return json(200, {
    marketplace,
    skills: matched
      .slice(offset, offset + limit)
      .map(({ path, name, description }) => ({ path, name, description })),
    total: matched.length,
    offset,
    limit,
  });
}).listen(port, '127.0.0.1');
