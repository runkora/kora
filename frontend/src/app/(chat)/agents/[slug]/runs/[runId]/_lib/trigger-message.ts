/**
 * @tier 3 — run-page-scoped. Splits a trigger's rendered message into the parts every
 * trigger template has: a lead instruction, a block of `Key: value` lines, closing instructions, and
 * the event the platform appends inside `<delivery-data>`.
 */

export interface TriggerField {
  key: string;
  value: string;
}

export interface TriggerMessageParts {
  lead: string;
  fields: TriggerField[];
  tail: string;
  /** The event as pretty JSON, without the platform's note to the model. */
  data?: string;
}

const FIELD = /^([A-Z][\w .'/-]{0,30}):[ \t]*(.*)$/;
const DATA = /<delivery-data>([\s\S]*?)<\/delivery-data>/;

/**
 * Looks up a `{{ path }}` placeholder the template left in. A webhook's data block is a flat map
 * keyed by the whole path (`"data.service"`); a platform event is the nested event itself.
 */
function lookup(data: unknown, path: string): string | undefined {
  if (data !== null && typeof data === 'object' && path in data) {
    const flat = (data as Record<string, unknown>)[path];
    return typeof flat === 'string' ? flat : undefined;
  }
  let cur: unknown = data;
  for (const key of path.split('.')) {
    if (cur === null || typeof cur !== 'object') return undefined;
    cur = (cur as Record<string, unknown>)[key];
  }
  return cur === undefined || cur === null || typeof cur === 'object' ? undefined : String(cur);
}

export function parseTriggerMessage(text: string): TriggerMessageParts {
  const match = DATA.exec(text);
  let data: unknown;
  let dataText: string | undefined;
  if (match) {
    // The event starts on the first line that opens an object; the note above it may mention `{{ path }}`.
    const json = match[1].slice(match[1].search(/^\{/m));
    try {
      data = JSON.parse(json);
      dataText = JSON.stringify(data, null, 2);
    } catch {
      dataText = json.trim();
    }
  }
  const resolve = (s: string) =>
    s.replace(/\{\{\s*([\w.]+)\s*\}\}/g, (all, path: string) => lookup(data, path) ?? all);

  const body = resolve(text.replace(DATA, '').trim());
  const paragraphs = body.split(/\n\s*\n/);
  // The field block is the first paragraph whose lines, after any leading sentence, are two or more
  // fields. A lone "Follow-up: …" line is a sentence, not a block.
  const at = paragraphs.findIndex((p) => {
    const lines = p.split('\n');
    const first = lines.findIndex((line) => FIELD.test(line));
    const rest = lines.slice(first);
    return first !== -1 && rest.length >= 2 && rest.every((line) => FIELD.test(line));
  });
  if (at === -1) return { lead: body, fields: [], tail: '', data: dataText };

  const lines = paragraphs[at].split('\n');
  const first = lines.findIndex((line) => FIELD.test(line));
  const fields = lines
    .slice(first)
    .map((line) => {
      const [, key, value] = FIELD.exec(line) ?? [];
      return { key, value: value.trim() };
    })
    // An empty value, or a link already given by an earlier field, adds nothing.
    .filter(
      (f, i, all) =>
        !['', '[]', '{}'].includes(f.value) && all.findIndex((g) => g.value === f.value) === i,
    );
  return {
    lead: [...paragraphs.slice(0, at), ...lines.slice(0, first)].join('\n\n').trim(),
    fields,
    tail: paragraphs.slice(at + 1).join('\n\n'),
    data: dataText,
  };
}
