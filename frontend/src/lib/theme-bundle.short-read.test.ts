/**
 * A file read may return fewer bytes than requested. The bounded read must
 * still see the whole file, and still see an oversize one.
 */
import { describe, expect, it } from 'vitest';
import { readUpTo, type ReadableHandle } from './theme-bundle';

/** A handle over `contents` that returns at most `chunk` bytes per read. */
function trickle(contents: Buffer, chunk: number): ReadableHandle & { reads: number } {
  const handle = {
    reads: 0,
    async read(buffer: Buffer, offset: number, length: number, position: number) {
      handle.reads += 1;
      const slice = contents.subarray(position, position + Math.min(length, chunk));
      slice.copy(buffer, offset);
      return { bytesRead: slice.length };
    },
  };
  return handle;
}

describe('readUpTo', () => {
  it('returns the whole file when each read returns a few bytes', async () => {
    const contents = Buffer.from('koraVersion: "1.0.0"\nproductName: Acme\n');
    const handle = trickle(contents, 3);
    const bytes = await readUpTo(handle, 1024);
    expect(Buffer.from(bytes).toString('utf8')).toBe(contents.toString('utf8'));
    expect(handle.reads).toBeGreaterThan(1);
  });

  it('stops one byte past the bound, so an oversize file is seen', async () => {
    const bytes = await readUpTo(trickle(Buffer.alloc(100, 'x'), 7), 10);
    expect(bytes.length).toBe(11);
  });

  it('returns an empty read for an empty file', async () => {
    expect((await readUpTo(trickle(Buffer.alloc(0), 4), 10)).length).toBe(0);
  });
});
