/** Test-only HTTP fixture for Next.js server fetches, which page.route cannot intercept. */
import { createServer } from 'node:http';
import {
  MARKETPLACE_FIXTURES,
  SKILL_DETAIL_FIXTURES,
  SKILL_FIXTURES_BY_MARKETPLACE,
} from './marketplace-fixtures.ts';

const port = Number(process.env.PLAYWRIGHT_MARKETPLACE_PORT ?? '3101');

createServer((req, res) => {
  const url = new URL(req.url, 'http://localhost');
  const parts = url.pathname.split('/').filter(Boolean).map(decodeURIComponent);
  function json(status, body) {
    res.writeHead(status, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(body));
  }

  if (url.pathname === '/health') return json(200, {});
  if (!req.headers.authorization?.startsWith('Bearer '))
    return json(401, { message: 'Unauthorized' });
  if (url.pathname === '/v1/marketplaces') return json(200, { marketplaces: MARKETPLACE_FIXTURES });

  const [version, resource, marketplaceSlug, skillName] = parts;
  const catalogue = SKILL_FIXTURES_BY_MARKETPLACE[marketplaceSlug];
  if (version !== 'v1' || resource !== 'marketplaces' || !catalogue) {
    return json(404, { message: 'Marketplace not found' });
  }
  if (parts.length === 3) {
    const search = (url.searchParams.get('search') ?? '').toLowerCase();
    return json(200, {
      ...MARKETPLACE_FIXTURES.find((entry) => entry.marketplaceSlug === marketplaceSlug),
      skills: catalogue.filter((skill) => skill.name.toLowerCase().includes(search)),
    });
  }
  if (parts.length !== 4) return json(404, { message: 'Skill not found' });
  const name = skillName.startsWith(`${marketplaceSlug}__`)
    ? skillName.slice(marketplaceSlug.length + 2)
    : skillName;
  if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(name)) {
    return json(400, { message: 'Invalid skill name' });
  }
  if (name === 'upstream-error') return json(503, { message: 'Temporary catalogue failure.' });
  const skillSlug = `${marketplaceSlug}__${name}`;
  const skill = catalogue.some((entry) => entry.skillSlug === skillSlug)
    ? SKILL_DETAIL_FIXTURES.find((entry) => entry.skillSlug === skillSlug)
    : undefined;
  return skill ? json(200, skill) : json(404, { message: 'Skill not found' });
}).listen(port, '127.0.0.1');
