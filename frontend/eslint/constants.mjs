/** React hooks that are only valid in Client Components. */
export const CLIENT_HOOKS = new Set([
  'useState',
  'useEffect',
  'useContext',
  'useCallback',
  'useMemo',
  'useRef',
  'useReducer',
  'useLayoutEffect',
  'useImperativeHandle',
  'useDebugValue',
  'useDeferredValue',
  'useTransition',
  'useId',
  'useSyncExternalStore',
]);

/**
 * Next.js navigation hooks that are only valid in Client Components.
 * These are not caught by the react-import check above because they are
 * imported from 'next/navigation', not from 'react'.
 */
export const CLIENT_NEXT_NAV_HOOKS = new Set([
  'useRouter',
  'usePathname',
  'useSearchParams',
  'useParams',
  'useSelectedLayoutSegment',
  'useSelectedLayoutSegments',
]);

/**
 * Convert a PascalCase or camelCase string to kebab-case, used to generate
 * rename suggestions in `require-kebab-filename` error messages.
 *
 * @param {string} str
 * @returns {string}
 */
export function toKebab(str) {
  return str
    .replace(/([a-z0-9])([A-Z])/g, '$1-$2')
    .replace(/([A-Z]+)([A-Z][a-z])/g, '$1-$2')
    .toLowerCase();
}
