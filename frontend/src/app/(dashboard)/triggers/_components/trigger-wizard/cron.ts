/**
 * @tier 3 — triggers-scoped. Pure cron builder: turns the schedule step's
 * builder state into a 5-field cron expression and the sentence a person reads
 * next to it. Kept free of React so both are unit-testable and so the review
 * step and the done screen derive the same strings the builder showed.
 */

/** Builder frequencies. `custom` means the user typed the expression themselves. */
export type Frequency = 'minutes' | 'hourly' | 'daily' | 'weekly' | 'monthly' | 'custom';

/** The subset of wizard state the cron builder reads. */
export interface CronSpec {
  freq: Frequency;
  /** Minutes between fires when `freq` is `minutes`. */
  interval: string;
  /** Minutes past the hour when `freq` is `hourly`. */
  atMinute: string;
  /** `HH:MM` when `freq` is daily, weekly, or monthly. */
  time: string;
  /** Cron day-of-week numbers (0 = Sunday) when `freq` is `weekly`. */
  weekdays: number[];
  /** Day of month when `freq` is `monthly`. */
  dom: string;
  /** Raw expression used when `freq` is `custom`. */
  customCron: string;
}

export interface FreqOption {
  id: Frequency;
  label: string;
}

/** Builder frequencies offered in the segmented control, in order. */
export const FREQS: FreqOption[] = [
  { id: 'minutes', label: 'Minutes' },
  { id: 'hourly', label: 'Hourly' },
  { id: 'daily', label: 'Daily' },
  { id: 'weekly', label: 'Weekly' },
  { id: 'monthly', label: 'Monthly' },
];

export interface WeekdayOption {
  /** Cron day-of-week number. */
  n: number;
  /** Short label. */
  s: string;
}

/** Monday-first so the weekday run reads as a working week. */
export const WEEKDAYS: WeekdayOption[] = [
  { n: 1, s: 'Mon' },
  { n: 2, s: 'Tue' },
  { n: 3, s: 'Wed' },
  { n: 4, s: 'Thu' },
  { n: 5, s: 'Fri' },
  { n: 6, s: 'Sat' },
  { n: 0, s: 'Sun' },
];

/** Preset intervals offered beside the minutes input. */
export const INTERVAL_PRESETS = ['5', '10', '15', '30'];

/** Preset minutes-past values offered beside the hourly input. */
export const MINUTE_PRESETS = ['0', '15', '30', '45'];

/** `parseInt` with a fallback for both NaN and 0, matching the design's `||`. */
function intOr(value: string, fallback: number): number {
  const n = parseInt(value, 10);
  return n || fallback;
}

function splitTime(time: string): { hour: number; minute: number } {
  const parts = (time || '02:00').split(':');
  const hour = parseInt(parts[0] ?? '', 10);
  const minute = parseInt(parts[1] ?? '', 10);
  return {
    hour: Number.isNaN(hour) ? 0 : hour,
    minute: Number.isNaN(minute) ? 0 : minute,
  };
}

/** Builds the 5-field cron expression for the current builder state. */
export function cronFor(w: CronSpec): string {
  if (w.freq === 'custom') return w.customCron;
  const { hour, minute } = splitTime(w.time);
  if (w.freq === 'minutes') return `*/${intOr(w.interval, 15)} * * * *`;
  if (w.freq === 'hourly') return `${intOr(w.atMinute, 0)} * * * *`;
  if (w.freq === 'daily') return `${minute} ${hour} * * *`;
  if (w.freq === 'weekly') {
    const days = (w.weekdays.length ? w.weekdays : [1]).slice().sort((a, b) => a - b);
    return `${minute} ${hour} * * ${days.join(',')}`;
  }
  if (w.freq === 'monthly') return `${minute} ${hour} ${intOr(w.dom, 1)} * *`;
  return '0 2 * * *';
}

/** English ordinal for a day of the month: 1st, 2nd, 3rd, 11th, 21st. */
export function ordinal(n: number): string {
  const suffixes = ['th', 'st', 'nd', 'rd'];
  const v = n % 100;
  return `${n}${suffixes[(v - 20) % 10] ?? suffixes[v] ?? suffixes[0]}`;
}

/** The sentence shown beside the expression, so the cron never stands alone. */
export function humanFor(w: CronSpec): string {
  const t = w.time || '02:00';
  if (w.freq === 'custom') return 'Custom schedule';
  if (w.freq === 'minutes') return `Every ${intOr(w.interval, 15)} minutes`;
  if (w.freq === 'hourly') {
    const m = intOr(w.atMinute, 0);
    return `Every hour at ${m === 0 ? 'the top of the hour' : `${m} minutes past`}`;
  }
  if (w.freq === 'daily') return `Every day at ${t}`;
  if (w.freq === 'weekly') {
    const names = WEEKDAYS.filter((d) => w.weekdays.includes(d.n)).map((d) => d.s);
    let label: string;
    if (names.length === 0) label = 'Monday';
    else if (names.length === 7) label = 'every day';
    else if (names.length === 5 && [1, 2, 3, 4, 5].every((n) => w.weekdays.includes(n)))
      label = 'weekdays';
    else label = names.join(', ');
    return `Every week on ${label} at ${t}`;
  }
  if (w.freq === 'monthly') return `Monthly on the ${ordinal(intOr(w.dom, 1))} at ${t}`;
  return '';
}

/**
 * Toggles a weekday, refusing to empty the set: a weekly schedule with no day
 * selected has no meaning, and silently falling back to Monday would hide that.
 */
export function toggleWeekday(weekdays: number[], n: number): number[] {
  const next = weekdays.slice();
  const i = next.indexOf(n);
  if (i < 0) next.push(n);
  else if (next.length > 1) next.splice(i, 1);
  return next;
}

/**
 * Timezone options: the fixed three plus the viewer's own zone, deduped and
 * with the browser zone last so the list order is stable between machines.
 */
export function timezoneOptions(browserZone?: string): string[] {
  const base = ['UTC', 'Europe/London', 'Australia/Sydney'];
  if (browserZone && !base.includes(browserZone)) base.push(browserZone);
  return base;
}

/** Reads the viewer's IANA zone, tolerating environments without Intl data. */
export function resolveBrowserZone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined;
  } catch {
    return undefined;
  }
}

/**
 * Whether an expression is a 5-field cron the scheduler will accept.
 *
 * Deliberately shallow: it checks the shape, not the semantics of each field,
 * because the scheduler is the authority on what a field may contain and a
 * stricter guess here would reject expressions that actually work.
 */
export function isValidCron(expr: string): boolean {
  const parts = expr.trim().split(/\s+/).filter(Boolean);
  if (parts.length !== 5) return false;
  return parts.every((part) => /^[0-9*,/\-a-zA-Z?]+$/.test(part));
}

const NUMERIC = /^\d+$/;

/**
 * Parses a 5-field expression back into the builder's vocabulary, when it fits
 * one of the shapes the builder can produce. Returns null for anything else —
 * a range, a month restriction, a raw expression someone typed — which the
 * caller renders as 'Custom schedule' rather than guessing at.
 */
export function specFromCron(expr: string): CronSpec | null {
  const parts = expr.trim().split(/\s+/);
  if (parts.length !== 5) return null;
  const [min, hour, dom, mon, dow] = parts as [string, string, string, string, string];
  if (mon !== '*') return null;

  const base: CronSpec = {
    freq: 'custom',
    interval: '',
    atMinute: '',
    time: '',
    weekdays: [],
    dom: '',
    customCron: expr,
  };
  const time = () => `${hour.padStart(2, '0')}:${min.padStart(2, '0')}`;

  if (
    min.startsWith('*/') &&
    NUMERIC.test(min.slice(2)) &&
    hour === '*' &&
    dom === '*' &&
    dow === '*'
  ) {
    return { ...base, freq: 'minutes', interval: min.slice(2) };
  }
  if (NUMERIC.test(min) && hour === '*' && dom === '*' && dow === '*') {
    return { ...base, freq: 'hourly', atMinute: min };
  }
  if (!NUMERIC.test(min) || !NUMERIC.test(hour)) return null;
  if (dom === '*' && dow === '*') {
    return { ...base, freq: 'daily', time: time() };
  }
  if (dom === '*' && dow !== '*') {
    const days = dow.split(',');
    if (!days.every((d) => NUMERIC.test(d) && Number(d) >= 0 && Number(d) <= 6)) return null;
    return { ...base, freq: 'weekly', time: time(), weekdays: days.map(Number) };
  }
  if (dow === '*' && NUMERIC.test(dom)) {
    return { ...base, freq: 'monthly', time: time(), dom };
  }
  return null;
}

/**
 * One sentence for a stored expression, phrased exactly as the builder would
 * phrase it. Shared by the drawer and the wizard through humanFor, so the same
 * schedule can never read differently on two surfaces.
 */
export function describeCron(expr: string): string {
  const spec = specFromCron(expr);
  return spec === null || spec.freq === 'custom' ? 'Custom schedule' : humanFor(spec);
}
