// Converts GitHub's webhook schemas from its OpenAPI description into the Orbit
// trigger schema format: {events: {<X-GitHub-Event>: schema}, definitions}.
//
// Usage: node github.mjs <path-to-api.github.com.json>
// The README pins the source commit and shows how to fetch it.

import { readFile, writeFile } from 'node:fs/promises';

const OUTPUT = new URL('../../backend/internal/triggers/schemas/github.json', import.meta.url);

const COMPONENT_REF = '#/components/schemas/';
const DEFINITION_REF = '#/definitions/';

async function loadSource(path) {
  if (!path) throw new Error('usage: node github.mjs <path-to-api.github.com.json>');
  return JSON.parse(await readFile(path, 'utf8'));
}

// eventName derives the X-GitHub-Event value from the operation ID, e.g.
// "projects-v2-item/created" -> "projects_v2_item". The X-GitHub-Event examples
// in the description are wrong for some events ("discussions", "project-v2").
function eventName(operation) {
  const base = operation.operationId?.split('/')[0];
  if (!base) throw new Error('webhook operation has no operationId');
  return base.replaceAll('-', '_');
}

// Rewrites component refs to definition refs and records each one reached.
function rewrite(node, reached) {
  if (Array.isArray(node)) return node.map((n) => rewrite(n, reached));
  if (node === null || typeof node !== 'object') return node;
  const out = {};
  for (const [key, value] of Object.entries(node)) {
    if (key === '$ref' && typeof value === 'string' && value.startsWith(COMPONENT_REF)) {
      const name = value.slice(COMPONENT_REF.length);
      reached.push(name);
      out[key] = DEFINITION_REF + name;
    } else {
      out[key] = rewrite(value, reached);
    }
  }
  return out;
}

// Sorts object keys so a regenerated file diffs cleanly.
function sorted(node) {
  if (Array.isArray(node)) return node.map(sorted);
  if (node === null || typeof node !== 'object') return node;
  return Object.fromEntries(Object.keys(node).sort().map((k) => [k, sorted(node[k])]));
}

const source = await loadSource(process.argv[2]);
const components = source.components.schemas;

const variants = new Map();
const pending = [];
for (const hook of Object.values(source['x-webhooks'])) {
  const op = hook.post;
  const schema = rewrite(op.requestBody.content['application/json'].schema, pending);
  const name = eventName(op);
  variants.set(name, [...(variants.get(name) ?? []), schema]);
}

const events = {};
for (const [name, schemas] of variants) {
  events[name] = schemas.length === 1 ? schemas[0] : { oneOf: schemas };
}

const definitions = {};
while (pending.length > 0) {
  const name = pending.pop();
  if (name in definitions) continue;
  if (!(name in components)) throw new Error(`unknown component schema ${name}`);
  definitions[name] = rewrite(components[name], pending);
}

await writeFile(OUTPUT, JSON.stringify(sorted({ events, definitions })) + '\n');
console.log(`wrote ${Object.keys(events).length} events, ${Object.keys(definitions).length} definitions`);
