/** Whether a URL from recorded data is safe to link: http or https only, so a javascript: URL never becomes a live link. */
export function isSafeUrl(url: string | undefined | null): url is string {
  if (!url) return false;
  try {
    const { protocol } = new URL(url);
    return protocol === 'https:' || protocol === 'http:';
  } catch {
    return false;
  }
}
