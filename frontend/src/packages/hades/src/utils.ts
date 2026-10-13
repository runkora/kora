import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

/**
 * Merge class names with Tailwind conflict resolution.
 *
 * Combines clsx (conditional class logic) with tailwind-merge (deduplication
 * of conflicting utility classes). Use this everywhere a component accepts
 * an external `className` prop.
 *
 * @example
 *   cn("px-4 py-2", isActive && "bg-accent", className)
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
