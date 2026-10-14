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

  if (/<script/i.test(text)) faults.push('contains a <script> element');
  if (/[\s/"']on[a-z]+\s*=/i.test(text)) faults.push('contains an event attribute (on*=)');
  const href = /(?:^|[\s"'/])(?:xlink:)?href\s*=\s*(?:["']\s*)?(?!#|data:image\/)[^\s"'>]/i;
  if (href.test(text)) faults.push('references an external resource through href');
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
