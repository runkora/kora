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
  if (NUMERIC.test(min) && Number(min) <= 59 && hour === '*' && dom === '*' && dow === '*') {
    return { ...base, freq: 'hourly', atMinute: min };
  }
  if (!NUMERIC.test(min) || !NUMERIC.test(hour)) return null;
  // A time that does not exist is not one the builder can show; it stays a raw expression.
  if (Number(min) > 59 || Number(hour) > 23) return null;
  if (dom === '*' && dow === '*') {
    return { ...base, freq: 'daily', time: time() };
  }
  if (dom === '*' && dow !== '*') {
    const days = dow.split(',');
    if (!days.every((d) => NUMERIC.test(d) && Number(d) >= 0 && Number(d) <= 6)) return null;
    return { ...base, freq: 'weekly', time: time(), weekdays: days.map(Number) };
  }
  if (dow === '*' && NUMERIC.test(dom) && Number(dom) >= 1 && Number(dom) <= 31) {
    return { ...base, freq: 'monthly', time: time(), dom };
  }
  return null;
}

/**
 * One sentence for a stored expression, phrased exactly as the builder would
 * phrase it. Shared by the modal and the wizard through humanFor, so the same
 * schedule can never read differently on two surfaces.
 */
export function describeCron(expr: string): string {
  const spec = specFromCron(expr);
  return spec === null || spec.freq === 'custom' ? 'Custom schedule' : humanFor(spec);
}

interface CronFieldSpec {
  label: string;
  min: number;
  max: number;
}

/** The five fields of an expression, in order, with the range a plain number may take. */
const CRON_FIELD_SPECS: CronFieldSpec[] = [
  { label: 'minute', min: 0, max: 59 },
  { label: 'hour', min: 0, max: 23 },
  { label: 'day of month', min: 1, max: 31 },
  { label: 'month', min: 1, max: 12 },
  { label: 'day of week', min: 0, max: 7 },
];

export interface CronFieldView {
  /** What the field means: minute, hour, and so on. */
  label: string;
  /** The text typed for it, empty when the expression has fewer than five fields. */
  value: string;
  /** Why this field is out of range; undefined when it is fine or cannot be told. */
  problem?: string;
}

export interface CronBreakdown {
  fields: CronFieldView[];
  /** The first thing wrong with the expression as a sentence; undefined when nothing is. */
  problem?: string;
}

/**
 * Splits an expression into its five labelled fields for display, and names
 * what is wrong with it. The range check only judges plain numbers (`25`,
 * `1-40`); names, `?` and step sizes are left to the scheduler, which stays the
 * authority on what is accepted. This is guidance for the screen, not a gate.
 */
export function cronBreakdown(expr: string): CronBreakdown {
  const parts = expr.trim().split(/\s+/).filter(Boolean);
  const fields = CRON_FIELD_SPECS.map((spec, i): CronFieldView => {
    const value = parts[i] ?? '';
    const [list = ''] = value.split('/');
    const bad = list
      .split(/[,-]/)
      .some(
        (token) => NUMERIC.test(token) && (Number(token) < spec.min || Number(token) > spec.max),
      );
    if (!bad) return { label: spec.label, value };
    const name = spec.label.charAt(0).toUpperCase() + spec.label.slice(1);
    return {
      label: spec.label,
      value,
      problem: `${name} must be between ${spec.min} and ${spec.max}.`,
    };
  });
  const firstBad = fields.find((f) => f.problem);
  if (parts.length !== 5) return { fields, problem: 'Needs five fields separated by spaces.' };
  if (firstBad) return { fields, problem: firstBad.problem };
  if (!isValidCron(expr)) return { fields, problem: 'Not a cron expression the scheduler reads.' };
  return { fields };
}

/** "Sun 1 Nov, 19:00 UTC" for an ISO instant, so a fire can be read in both zones. */
export function utcLabel(at: string): string | null {
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) return null;
  const parts = new Intl.DateTimeFormat('en-GB', {
    timeZone: 'UTC',
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(date);
  const get = (type: string) => parts.find((p) => p.type === type)?.value ?? '';
  return `${get('weekday')} ${get('day')} ${get('month')}, ${get('hour')}:${get('minute')} UTC`;
}

/** What a person calls a zone's clock: "Sydney time", "UTC". */
export function zoneClockName(zone: string): string {
  if (zone === 'UTC') return 'UTC';
  const city = (zone.split('/').pop() ?? zone).replace(/_/g, ' ');
  return `${city} time`;
}

/** The minutes past the hour an every-N-minutes schedule fires on, as ":00, :15, :30 and :45". */
export function minuteMarks(interval: number, limit = 4): string {
  const marks: string[] = [];
  for (let m = 0; m < 60; m += interval) marks.push(`:${String(m).padStart(2, '0')}`);
  if (marks.length <= limit) {
    const last = marks.pop();
    return marks.length ? `${marks.join(', ')} and ${last}` : (last ?? ':00');
  }
  return `${marks.slice(0, limit).join(', ')} and so on`;
}

/** The builder's fallback when an expression it cannot show is swapped for one it can. */
export const FALLBACK_TIME = '09:00';

/** Whether the builder can show this expression. */
export function fitsBuilder(expr: string): boolean {
  const spec = specFromCron(expr);
  return spec !== null && spec.freq !== 'custom';
}

/** The builder fields a stored expression fills, over the current ones where it names none. */
export function builderPatchFor(expr: string, current: CronSpec): Partial<CronSpec> {
  const spec = specFromCron(expr);
  if (spec === null || spec.freq === 'custom') return { freq: 'daily', time: FALLBACK_TIME };
  return {
    freq: spec.freq,
    interval: spec.interval || current.interval,
    atMinute: spec.atMinute || current.atMinute,
    time: spec.time || current.time || FALLBACK_TIME,
    weekdays: spec.weekdays.length ? spec.weekdays : current.weekdays,
    dom: spec.dom || current.dom,
  };
}

/**
 * What a day of the month past the 28th means, or undefined when every month has it. The
 * scheduler only fires on days a month has, so the months that fall short are skipped.
 */
export function monthDayNote(dom: string): string | undefined {
  if (!NUMERIC.test(dom.trim())) return undefined;
  const day = Number(dom);
  if (day === 29) {
    return 'February is skipped except in leap years, so this runs 11 times a year (12 in a leap year).';
  }
  if (day === 30) return 'February is skipped, so this runs 11 times a year.';
  if (day === 31) return 'Months with fewer days are skipped, so this runs 7 times a year.';
  return undefined;
}
