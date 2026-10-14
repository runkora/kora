/**
 * Asset checks: the file extension, the size, and a content sniff per format.
 * SVG is text and is held to the versioning ADR's SD-3 constraints: no
 * scripts, no event attributes, no external references. WOFF2 is checked by
 * its magic number. Pure functions over bytes.
 */

/** The lower-cased extension of a bundle path, or null when it has none. */
export function extensionOf(path: string): string | null {
  const name = path.slice(path.lastIndexOf('/') + 1);
  const dot = name.lastIndexOf('.');
  return dot <= 0 ? null : name.slice(dot + 1).toLowerCase();
}

/** An `on*=` attribute inside a tag, however it is separated from the name before it. */
const EVENT_ATTRIBUTE = /[\s/"']on[a-z]+\s*=/i;

/** An `href`, with or without a namespace prefix, whose value is not a fragment or an image data URI. */
const HREF_ATTRIBUTE =
  /(?:^|[\s"'/])(?:[a-z_][\w.-]*:)?href\s*=\s*(?:["']\s*)?(?!#|data:image\/)[^\s"'>]/i;

/**
 * The text between `<` and `>` of every tag in the document, a `>` inside a
 * quoted attribute value not ending the tag. Comments, CDATA sections,
 * processing instructions and declarations are skipped whole, and text content
 * is never returned, so attribute checks see attributes and nothing else.
 */
function tagInteriors(text: string): string[] {
  const interiors: string[] = [];
  let i = 0;
  while (i < text.length) {
    const open = text.indexOf('<', i);
    if (open === -1) break;
    const skipTo = (terminator: string): number => {
      const end = text.indexOf(terminator, open);
      return end === -1 ? text.length : end + terminator.length;
    };
    if (text.startsWith('<!--', open)) {
      i = skipTo('-->');
    } else if (text.startsWith('<![CDATA[', open)) {
      i = skipTo(']]>');
    } else if (text.startsWith('<?', open)) {
      i = skipTo('?>');
    } else if (text.startsWith('<!', open)) {
      i = skipTo('>');
    } else {
      let j = open + 1;
      let quote = '';
      for (; j < text.length; j++) {
        const c = text[j]!;
        if (quote) {
          if (c === quote) quote = '';
        } else if (c === '"' || c === "'") {
          quote = c;
        } else if (c === '>') {
          break;
        }
      }
      interiors.push(text.slice(open + 1, j));
      i = j + 1;
    }
  }
  return interiors;
}

const decoder = new TextDecoder('utf-8', { fatal: false });

/**
 * Returns the faults in an SVG asset, empty when it is acceptable. The
 * document must begin, after an optional BOM, XML prolog, comments and
 * whitespace, with `<svg`; a DOCTYPE is refused because it can declare
 * entities.
 */
export function svgFaults(bytes: Uint8Array): string[] {
  const text = decoder.decode(bytes);
  const faults: string[] = [];

  let i = text.charCodeAt(0) === 0xfeff ? 1 : 0;
  for (;;) {
    while (i < text.length && /\s/.test(text[i]!)) i++;
    if (text.startsWith('<?', i)) {
      const end = text.indexOf('?>', i);
      if (end === -1) return ['unterminated XML prolog'];
      i = end + 2;
      continue;
    }
    if (text.startsWith('<!--', i)) {
      const end = text.indexOf('-->', i);
      if (end === -1) return ['unterminated comment'];
      i = end + 3;
      continue;
    }
    break;
  }
  if (/^<!DOCTYPE/i.test(text.slice(i, i + 9))) {
    faults.push('a DOCTYPE is not accepted; the document must begin with <svg');
  } else if (!/^<svg[\s/>]/.test(text.slice(i, i + 5))) {
    faults.push('does not begin with <svg');
  }

  if (/<(?:[a-z_][\w.-]*:)?script/i.test(text)) faults.push('contains a <script> element');
  if (/<(?:[a-z_][\w.-]*:)?foreignObject/i.test(text)) {
    faults.push('uses foreignObject, which can embed HTML');
  }
  const tags = tagInteriors(text);
  if (tags.some((tag) => EVENT_ATTRIBUTE.test(tag))) {
    faults.push('contains an event attribute (on*=)');
  }
  if (tags.some((tag) => HREF_ATTRIBUTE.test(tag))) {
    faults.push('references an external resource through href');
  }
  const url = /url\(\s*(?:["']\s*)?(?!#)[^\s"')]/i;
  if (/@import/i.test(text)) faults.push('references an external stylesheet through @import');
  if (url.test(text)) faults.push('references an external resource through url()');

  return faults;
}

const WOFF2_MAGIC = [0x77, 0x4f, 0x46, 0x32]; // "wOF2"

export function isWoff2(bytes: Uint8Array): boolean {
  return bytes.length >= 4 && WOFF2_MAGIC.every((b, i) => bytes[i] === b);
}

/** Content faults for an asset of the given format, empty when acceptable. */
export function contentFaults(format: string, bytes: Uint8Array): string[] {
  switch (format) {
    case 'svg':
      return svgFaults(bytes);
    case 'woff2':
      return isWoff2(bytes) ? [] : ['is not a WOFF2 file (missing the wOF2 signature)'];
    default:
      return [];
  }
}
