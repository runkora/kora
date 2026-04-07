'use client';

import {
  AreaChart,
  Area,
  BarChart,
  Bar,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  Cell,
} from 'recharts';
import { Badge, Grid, Heading, Stack, Text } from '@orbit/terra';
import { GatewayCard } from './gateway-card';
import { Clock, DollarSign, Zap, Activity, AlertCircle, HardDrive } from 'lucide-react';
import { DAILY_SPEND, DAILY_REQUESTS, STATS } from '../_data/mock';
import { StatCards, UsageTabs } from '../usage/_components';

// ── Types & Helpers ───────────────────────────────────────────────────────────

function fmt(usd: number): string {
  return `$${usd.toFixed(2)}`;
}

function fmtTokens(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1)}B`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`;
  return n.toLocaleString();
}

function fmtReqs(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

function fmtLatency(ms: number): string {
  return `${ms.toFixed(0)}ms`;
}

function fmtPercent(n: number): string {
  return `${n.toFixed(1)}%`;
}

// ── Chart Colors (Terra tokens as hex) ────────────────────────────────────────

const COLORS = {
  accent: '#e8a07a',
  healthy: '#9ce37d',
  degraded: '#ffd046',
  error: '#ef4444',
  surfaceOverlay: '#34343d',
  borderSubtle: '#44444d',
  foreground: '#ededef',
  subtle: '#8a8a94',
};

// ── 1. KPI Stat Cards ─────────────────────────────────────────────────────────

interface KPICardProps {
  icon: React.ReactNode;
  label: string;
  value: string;
  deltaPercent: number;
  deltaText: string;
}

function KPICard({ icon, label, value, deltaPercent, deltaText }: KPICardProps) {
  const deltaColor = deltaPercent >= 0 ? 'status-degraded' : 'status-healthy';
  const deltaIcon = deltaPercent >= 0 ? '▲' : '▼';

  return (
    <GatewayCard title={label} icon={icon}>
      <Stack gap="snug">
        <Heading variant="metric">{value}</Heading>
        <Stack direction="horizontal" align="center" gap="tight">
          <Text size="caption" color={deltaColor}>
            {deltaIcon} {fmtPercent(Math.abs(deltaPercent))}
          </Text>
          <Text size="caption" color="subtle">
            {deltaText}
          </Text>
        </Stack>
      </Stack>
    </GatewayCard>
  );
}

// ── 2. Requests over time chart ───────────────────────────────────────────────

interface RequestsChartPoint {
  date: string;
  total: number;
  successful: number;
  errored: number;
}

function formatDateAxis(dateStr: string): string {
  try {
    const d = new Date(dateStr + 'T00:00:00Z');
    return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
  } catch {
    return dateStr;
  }
}

function formatRequestsAxis(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return n.toLocaleString();
}

function RequestsChart() {
  // Use last 14 days for the chart
  const data: RequestsChartPoint[] = DAILY_REQUESTS.slice(-14);

  return (
    <GatewayCard
      title="Requests over time"
      description="Total API calls across all models (last 14 days)"
    >
      <ResponsiveContainer width="100%" height={240}>
        <AreaChart data={data} margin={{ top: 4, right: 16, bottom: 0, left: 8 }}>
          <defs>
            <linearGradient id="colorTotal" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor={COLORS.accent} stopOpacity={0.3} />
              <stop offset="95%" stopColor={COLORS.accent} stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke={COLORS.borderSubtle} vertical={false} />
          <XAxis
            dataKey="date"
            tick={{ fill: COLORS.subtle, fontSize: 11 }}
            tickLine={false}
            axisLine={false}
            tickFormatter={formatDateAxis}
            minTickGap={40}
          />
          <YAxis
            tick={{ fill: COLORS.subtle, fontSize: 11 }}
            tickLine={false}
            axisLine={false}
            tickFormatter={formatRequestsAxis}
            width={52}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: COLORS.surfaceOverlay,
              border: `1px solid ${COLORS.borderSubtle}`,
              borderRadius: '6px',
              padding: '8px 12px',
            }}
            labelFormatter={(label: unknown) => formatDateAxis(String(label))}
            formatter={(value: unknown) => [formatRequestsAxis(Number(value)), '']}
            cursor={{ stroke: COLORS.borderSubtle, strokeWidth: 1 }}
          />
          <Area
            type="monotone"
            dataKey="total"
            stroke={COLORS.accent}
            fillOpacity={1}
            fill="url(#colorTotal)"
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </GatewayCard>
  );
}

// ── 3. Cost by model chart ────────────────────────────────────────────────────

interface ModelCostData {
  model: string;
  cost: number;
}

function CostByModelChart() {
  // Derive 5 models from aggregated spend data
  const modelCosts: ModelCostData[] = [
    { model: 'Claude 3.5 Opus', cost: STATS.spend.month * 0.42 },
    { model: 'Claude 3.5 Sonnet', cost: STATS.spend.month * 0.31 },
    { model: 'Claude 3 Haiku', cost: STATS.spend.month * 0.18 },
    { model: 'GPT-4 Turbo', cost: STATS.spend.month * 0.06 },
    { model: 'Llama 2 70B', cost: STATS.spend.month * 0.03 },
  ].sort((a, b) => b.cost - a.cost);

  const modelColors = [
    COLORS.accent,
    COLORS.healthy,
    COLORS.degraded,
    COLORS.subtle,
    COLORS.borderSubtle,
  ];

  return (
    <GatewayCard title="Cost by model" description="Current billing period spend distribution">
      <ResponsiveContainer width="100%" height={240}>
        <BarChart
          data={modelCosts}
          layout="vertical"
          margin={{ top: 4, right: 16, bottom: 0, left: 140 }}
        >
          <CartesianGrid strokeDasharray="3 3" stroke={COLORS.borderSubtle} horizontal={false} />
          <XAxis type="number" tick={{ fill: COLORS.subtle, fontSize: 11 }} />
          <YAxis
            type="category"
            dataKey="model"
            tick={{ fill: COLORS.subtle, fontSize: 11 }}
            width={140}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: COLORS.surfaceOverlay,
              border: `1px solid ${COLORS.borderSubtle}`,
              borderRadius: '6px',
              padding: '8px 12px',
            }}
            formatter={(value: unknown) => fmt(Number(value))}
            cursor={{ fill: 'rgba(255,255,255,0.05)' }}
          />
          <Bar dataKey="cost" isAnimationActive={false}>
            {modelCosts.map((entry, index) => (
              <Cell key={`cell-${index}`} fill={modelColors[index % modelColors.length]} />
            ))}
          </Bar>
        </BarChart>
      </ResponsiveContainer>
    </GatewayCard>
  );
}

// ── 4. Latency distribution chart ─────────────────────────────────────────────

interface LatencyData {
  date: string;
  p50: number;
  p95: number;
  p99: number;
}

function generateMockLatencyData(): LatencyData[] {
  const data: LatencyData[] = [];
  const baseDate = new Date('2025-06-17');
  for (let i = 0; i < 14; i++) {
    const d = new Date(baseDate);
    d.setDate(d.getDate() + i);
    const dateStr = d.toISOString().split('T')[0];
    // Generate realistic latency patterns with some variance
    const baseLatency = 120 + Math.sin(i / 3) * 30;
    data.push({
      date: dateStr,
      p50: Math.max(80, baseLatency + (Math.random() - 0.5) * 20),
      p95: Math.max(150, baseLatency + 70 + (Math.random() - 0.5) * 30),
      p99: Math.max(250, baseLatency + 150 + (Math.random() - 0.5) * 50),
    });
  }
  return data;
}

function LatencyChart() {
  const data = generateMockLatencyData();

  return (
    <GatewayCard
      title="Latency distribution"
      description="P50, P95, and P99 request latency (last 14 days)"
    >
      <ResponsiveContainer width="100%" height={240}>
        <LineChart data={data} margin={{ top: 4, right: 16, bottom: 0, left: 8 }}>
          <CartesianGrid strokeDasharray="3 3" stroke={COLORS.borderSubtle} vertical={false} />
          <XAxis
            dataKey="date"
            tick={{ fill: COLORS.subtle, fontSize: 11 }}
            tickLine={false}
            axisLine={false}
            tickFormatter={formatDateAxis}
            minTickGap={40}
          />
          <YAxis
            tick={{ fill: COLORS.subtle, fontSize: 11 }}
            tickLine={false}
            axisLine={false}
            tickFormatter={(n) => `${n}ms`}
            width={48}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: COLORS.surfaceOverlay,
              border: `1px solid ${COLORS.borderSubtle}`,
              borderRadius: '6px',
              padding: '8px 12px',
            }}
            labelFormatter={(label: unknown) => formatDateAxis(String(label))}
            formatter={(value: unknown) => fmtLatency(Number(value))}
            cursor={{ stroke: COLORS.borderSubtle, strokeWidth: 1 }}
          />
          <Legend wrapperStyle={{ paddingTop: '16px' }} iconType="line" />
          <Line
            type="monotone"
            dataKey="p50"
            stroke={COLORS.healthy}
            strokeWidth={2}
            dot={false}
            name="P50"
            isAnimationActive={false}
          />
          <Line
            type="monotone"
            dataKey="p95"
            stroke={COLORS.degraded}
            strokeWidth={2}
            dot={false}
            name="P95"
            isAnimationActive={false}
          />
          <Line
            type="monotone"
            dataKey="p99"
            stroke={COLORS.error}
            strokeWidth={2}
            dot={false}
            name="P99"
            isAnimationActive={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </GatewayCard>
  );
}

// ── Main Component ────────────────────────────────────────────────────────────

export function OverviewTab() {
  // Derive DAILY_TOKENS for calculations
  const DAILY_TOKENS = DAILY_SPEND.map((d) => ({
    date: d.date,
    promptTokens: Math.round(d.promptUsd * 140_000),
    outputTokens: Math.round(d.outputUsd * 14_000),
    totalTokens: Math.round(d.promptUsd * 140_000) + Math.round(d.outputUsd * 14_000),
  }));

  // Calculate 7-day deltas (comparing current to previous 7 days)
  const currentWeek = DAILY_REQUESTS.slice(-7);
  const previousWeek = DAILY_REQUESTS.slice(-14, -7);

  const currentWeekTotal = currentWeek.reduce((s, r) => s + r.total, 0);
  const previousWeekTotal = previousWeek.reduce((s, r) => s + r.total, 0);
  const requestsDelta =
    previousWeekTotal > 0 ? ((currentWeekTotal - previousWeekTotal) / previousWeekTotal) * 100 : 0;

  const currentWeekTokens = DAILY_TOKENS.slice(-7).reduce((s, r) => s + r.totalTokens, 0);
  const previousWeekTokens = DAILY_TOKENS.slice(-14, -7).reduce((s, r) => s + r.totalTokens, 0);
  const tokensDelta =
    previousWeekTokens > 0
      ? ((currentWeekTokens - previousWeekTokens) / previousWeekTokens) * 100
      : 0;

  const currentWeekSpend = DAILY_SPEND.slice(-7).reduce((s, r) => s + r.totalUsd, 0);
  const previousWeekSpend = DAILY_SPEND.slice(-14, -7).reduce((s, r) => s + r.totalUsd, 0);
  const spendDelta =
    previousWeekSpend > 0 ? ((currentWeekSpend - previousWeekSpend) / previousWeekSpend) * 100 : 0;

  return (
    <Stack direction="vertical" gap="comfortable" asChild>
      <section className="pb-8">
        {/* ── Page header ── */}
        <Stack gap="snug">
          <Stack direction="horizontal" align="center" gap="snug">
            <Heading variant="title">Model Gateway</Heading>
            <Badge variant="healthy" dot size="sm">
              Operational
            </Badge>
          </Stack>
          <Text size="supporting" color="muted">
            Unified inference proxy — route, observe, and govern all LLM traffic from a single
            control plane.
          </Text>
        </Stack>

        {/* KPI Cards */}
        <Grid cols={{ base: 1, sm: 2, lg: 3 }} gap="comfortable">
          <KPICard
            icon={<Activity className="size-4" />}
            label="Total Requests"
            value={fmtReqs(STATS.requests.total)}
            deltaPercent={requestsDelta}
            deltaText="vs last 7 days"
          />
          <KPICard
            icon={<Zap className="size-4" />}
            label="Total Tokens"
            value={fmtTokens(STATS.tokens.period)}
            deltaPercent={tokensDelta}
            deltaText="vs last 7 days"
          />
          <KPICard
            icon={<DollarSign className="size-4" />}
            label="Total Spend"
            value={fmt(STATS.spend.period)}
            deltaPercent={spendDelta}
            deltaText="vs last 7 days"
          />
          <KPICard
            icon={<Clock className="size-4" />}
            label="Avg Latency (p50)"
            value="142ms"
            deltaPercent={2.3}
            deltaText="vs last 7 days"
          />
          <KPICard
            icon={<AlertCircle className="size-4" />}
            label="Error Rate"
            value={fmtPercent((STATS.requests.errored / STATS.requests.total) * 100)}
            deltaPercent={-0.8}
            deltaText="vs last 7 days"
          />
          <KPICard
            icon={<HardDrive className="size-4" />}
            label="Cache Hit Rate"
            value="68.2%"
            deltaPercent={5.1}
            deltaText="vs last 7 days"
          />
        </Grid>

        {/* Charts */}
        <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
          <RequestsChart />
          <LatencyChart />
        </Grid>

        <CostByModelChart />

        {/* Detailed usage analytics */}
        <StatCards stats={STATS} />
        <UsageTabs />
      </section>
    </Stack>
  );
}
