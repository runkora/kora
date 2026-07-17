import type { AnalyticsDays } from '../model-gateway/_types';
import type { PeriodValue } from './preferences.schema';

export type { AnalyticsDays };
export type PeriodPrefKey = 'analytics.defaultPeriod' | 'myUsage.defaultPeriod';

export function toDays(period: PeriodValue): AnalyticsDays {
  if (period === '30d') return 30;
  if (period === '90d') return 90;
  return 7;
}

export function toPeriod(days: AnalyticsDays): PeriodValue {
  if (days === 30) return '30d';
  if (days === 90) return '90d';
  return '7d';
}
