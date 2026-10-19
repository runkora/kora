/**
 * @tier 3 — environments-scoped. Size presets for the environment wizard. Sizes
 * are Kubernetes quantities on the wire and plain cores and GiB in the wizard.
 */

import type { EnvironmentSize, EnvironmentSizes } from '@runkora/shared/api';

export type SizeKey = keyof EnvironmentSize;

export const SIZE_FIELDS: { key: SizeKey; label: string; purpose: string }[] = [
  { key: 'cpu', label: 'CPU', purpose: 'More cores make builds and tests faster.' },
  { key: 'memory', label: 'Memory', purpose: 'Large builds need more memory.' },
  { key: 'disk', label: 'Local disk', purpose: 'Space for dependencies and build outputs.' },
];

export const UNIT: Record<SizeKey, string> = { cpu: 'cores', memory: 'GiB', disk: 'GiB' };

export type Preset = 'small' | 'medium' | 'large' | 'custom';

export const PRESET_OPTIONS: { value: Preset; label: string }[] = [
  { value: 'small', label: 'Small' },
  { value: 'medium', label: 'Medium' },
  { value: 'large', label: 'Large' },
  { value: 'custom', label: 'Custom' },
];

/** The raw Custom inputs, in cores and GiB. */
export type CustomSize = Record<SizeKey, string>;

/** A preset, plus the Custom values typed so far. */
export interface SizeChoice {
  preset: Preset;
  custom: CustomSize;
  /** The saved size and its Custom values. An untouched Custom field sends the saved quantity unchanged. */
  saved?: { size: EnvironmentSize; custom: CustomSize };
}

type Amounts = Record<SizeKey, number>;

const BINARY: Record<string, number> = {
  Ki: 2 ** -20,
  Mi: 2 ** -10,
  Gi: 1,
  Ti: 2 ** 10,
  Pi: 2 ** 20,
  Ei: 2 ** 30,
};
const DECIMAL: Record<string, number> = { k: 1e3, M: 1e6, G: 1e9, T: 1e12, P: 1e15, E: 1e18 };

/** A quantity as cores ("500m" is 0.5) or GiB ("512Mi" is 0.5). NaN when it does not parse. */
export function toAmount(key: SizeKey, quantity: string): number {
  const match = /^\s*([0-9]*\.?[0-9]+(?:[eE][+-]?[0-9]+)?)\s*([a-zA-Z]*)\s*$/.exec(quantity);
  if (!match) return Number.NaN;
  const n = Number(match[1]);
  const unit = match[2] ?? '';
  if (key === 'cpu') return unit === 'm' ? n / 1000 : unit === '' ? n : Number.NaN;
  if (unit in BINARY) return n * BINARY[unit]!;
  if (unit in DECIMAL) return (n * DECIMAL[unit]!) / 2 ** 30;
  return unit === '' ? n / 2 ** 30 : Number.NaN;
}

// Amounts keep full precision, and only what is shown is rounded.
const round = (n: number) => Math.round(n * 100) / 100;

const same = (a: number, b: number) => Math.abs(a - b) < 1e-9;

export const withUnit = (key: SizeKey, n: number) =>
  key === 'cpu' ? `${round(n)} ${round(n) === 1 ? 'core' : 'cores'}` : `${round(n)} GiB`;

/** A size as one line, e.g. "2 cores · 4 GiB · 10 GiB". */
export const sizeLine = (values: Amounts) =>
  `${withUnit('cpu', values.cpu)} · ${withUnit('memory', values.memory)} · ${withUnit('disk', values.disk)}`;

function amounts(size: EnvironmentSize): Amounts {
  return {
    cpu: toAmount('cpu', size.cpu),
    memory: toAmount('memory', size.memory),
    disk: toAmount('disk', size.disk),
  };
}

/** Small is fixed and capped at the maximum, Medium is the platform default and Large the maximum. */
export function presetsFor(sizes: EnvironmentSizes): Record<Exclude<Preset, 'custom'>, Amounts> {
  const max = amounts(sizes.max);
  return {
    small: {
      cpu: Math.min(1, max.cpu),
      memory: Math.min(2, max.memory),
      disk: Math.min(5, max.disk),
    },
    medium: amounts(sizes.default),
    large: max,
  };
}

/**
 * The preset a saved size matches exactly, or Custom holding its values. A new
 * environment starts on Medium. A saved quantity the wizard cannot read shows as
 * an empty field and is sent back unchanged.
 */
export function sizeChoiceFor(
  size: EnvironmentSize | undefined,
  sizes: EnvironmentSizes,
): SizeChoice {
  const values = amounts(size ?? sizes.default);
  const shown = (n: number) => (Number.isFinite(n) ? String(round(n)) : '');
  const custom: CustomSize = {
    cpu: shown(values.cpu),
    memory: shown(values.memory),
    disk: shown(values.disk),
  };
  if (!size) return { preset: 'medium', custom };
  const presets = presetsFor(sizes);
  const match = (Object.keys(presets) as (keyof typeof presets)[]).find((preset) =>
    SIZE_FIELDS.every(({ key }) => same(presets[preset][key], values[key])),
  );
  return { preset: match ?? 'custom', custom, saved: { size, custom: { ...custom } } };
}

/** Whether a Custom field still holds the saved value. */
const untouched = (choice: SizeChoice, key: SizeKey) =>
  choice.saved !== undefined && choice.custom[key] === choice.saved.custom[key];

/** The field error for one Custom value, or undefined. */
export function customError(choice: SizeChoice, sizes: EnvironmentSizes, key: SizeKey) {
  if (choice.preset !== 'custom') return undefined;
  const raw = choice.custom[key].trim();
  if (!raw) return undefined;
  const kept = untouched(choice, key);
  const n = kept ? toAmount(key, choice.saved!.size[key]) : Number(raw);
  // The agents service checks a saved quantity the wizard cannot read.
  if (kept && !Number.isFinite(n)) return undefined;
  if (!Number.isFinite(n) || n <= 0) return 'Enter a number greater than 0.';
  const max = toAmount(key, sizes.max[key]);
  const label = SIZE_FIELDS.find((f) => f.key === key)?.label ?? key;
  if (n > max) return `${label} can be at most ${withUnit(key, max)}.`;
  return undefined;
}

export function sizeInvalid(choice: SizeChoice, sizes: EnvironmentSizes) {
  return SIZE_FIELDS.some(({ key }) => customError(choice, sizes, key) !== undefined);
}

function chosenAmounts(choice: SizeChoice, sizes: EnvironmentSizes): Amounts {
  if (choice.preset !== 'custom') return presetsFor(sizes)[choice.preset];
  // A kept field is its saved quantity, and an empty one takes the platform default.
  const amount = (key: SizeKey) => {
    if (untouched(choice, key)) return toAmount(key, choice.saved!.size[key]);
    const raw = choice.custom[key].trim();
    return raw ? Number(raw) : toAmount(key, sizes.default[key]);
  };
  return { cpu: amount('cpu'), memory: amount('memory'), disk: amount('disk') };
}

/** A readable summary of the chosen size, e.g. "Medium: 2 cores · 4 GiB · 10 GiB". */
export function sizeSummary(choice: SizeChoice, sizes: EnvironmentSizes) {
  const values = chosenAmounts(choice, sizes);
  const label = PRESET_OPTIONS.find((o) => o.value === choice.preset)?.label ?? 'Custom';
  return `${label}: ${sizeLine(values)}`;
}

/** The size to send. Medium and Large send the platform's own quantities, and an empty Custom field takes the default. */
export function sizeRequest(choice: SizeChoice, sizes: EnvironmentSizes): EnvironmentSize {
  if (choice.preset === 'medium') return sizes.default;
  if (choice.preset === 'large') return sizes.max;
  const values: Record<SizeKey, string> =
    choice.preset === 'small'
      ? {
          cpu: String(presetsFor(sizes).small.cpu),
          memory: String(presetsFor(sizes).small.memory),
          disk: String(presetsFor(sizes).small.disk),
        }
      : {
          cpu: choice.custom.cpu.trim(),
          memory: choice.custom.memory.trim(),
          disk: choice.custom.disk.trim(),
        };
  const quantity = (key: SizeKey) => {
    if (choice.preset === 'custom' && untouched(choice, key)) return choice.saved!.size[key];
    if (key === 'cpu' || !values[key]) return values[key];
    return `${values[key]}Gi`;
  };
  return { cpu: quantity('cpu'), memory: quantity('memory'), disk: quantity('disk') };
}
