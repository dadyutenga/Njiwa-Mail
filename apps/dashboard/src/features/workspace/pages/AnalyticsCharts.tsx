import {
  Bar,
  BarChart,
  CartesianGrid,
  Line,
  LineChart,
  Rectangle,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  type TooltipProps,
} from "recharts";
import { usePhrase } from "@/features/i18n/i18n";

// The per-day charts on the Analytics page. Colours are the dashboard's own tokens,
// checked for colour-blind separation in light and dark mode:
//   messages: submitted = series-1, failed = bad-mark, other = muted (neutral rest)
//   bounces:  hard = bad-mark, soft = series-5
// Messages and bounces have very different sizes, so each has its own chart and axis.

export type DailyRow = Record<string, unknown>;

type Series = { key: string; label: string; color: string };

const num = (v: unknown) => {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
};

/** One row per day from start to end (inclusive, UTC); missing days count as zero. */
export function fillDays(rows: DailyRow[], start: Date, end: Date): DailyRow[] {
  const byDay = new Map(rows.map((r) => [String(r.day).slice(0, 10), r]));
  const out: DailyRow[] = [];
  for (let t = start.getTime(); t <= end.getTime(); t += 86400000) {
    const day = new Date(t).toISOString().slice(0, 10);
    const r = byDay.get(day) ?? {};
    const messages = num(r.messages);
    const submitted = num(r.submitted);
    const failed = num(r.failed);
    out.push({
      day,
      messages,
      submitted,
      failed,
      other: Math.max(0, messages - submitted - failed),
      hard_bounces: num(r.hard_bounces),
      soft_bounces: num(r.soft_bounces),
    });
  }
  return out;
}

type RectProps = React.ComponentProps<typeof Rectangle> & { payload: DailyRow };

/** The index of the highest non-empty segment in a stacked column, which gets the rounded end. */
function topSegment(row: DailyRow, series: Series[]) {
  for (let i = series.length - 1; i >= 0; i--) if (num(row[series[i].key]) > 0) return i;
  return -1;
}

const shortDay = (day: unknown) =>
  new Date(`${String(day)}T00:00:00Z`).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });

const longDay = (day: unknown) =>
  new Date(`${String(day)}T00:00:00Z`).toLocaleDateString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: "UTC",
  });

function Legend({ series }: { series: Series[] }) {
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
      {series.map((s) => (
        <li key={s.key} className="flex items-center gap-1.5">
          <span aria-hidden className="size-2.5 rounded-sm" style={{ background: s.color }} />
          {s.label}
        </li>
      ))}
    </ul>
  );
}

function ChartTooltip({
  active,
  payload,
  label,
  series,
  total,
}: TooltipProps<number, string> & { series: Series[]; total?: { label: string; key: string } }) {
  if (!active || !payload?.length) return null;
  const row = payload[0].payload as DailyRow;
  return (
    <div
      className="min-w-44 rounded-lg border px-3 py-2 text-xs shadow-md"
      style={{
        background: "var(--od-surface)",
        color: "var(--od-ink)",
        borderColor: "var(--od-border)",
      }}
    >
      <p className="mb-1.5 font-medium">{longDay(label)}</p>
      <dl className="grid gap-1">
        {[...series].reverse().map((s) => (
          <div key={s.key} className="flex items-center justify-between gap-4">
            <dt className="flex items-center gap-1.5 text-muted-foreground">
              <span aria-hidden className="size-2 rounded-sm" style={{ background: s.color }} />
              {s.label}
            </dt>
            <dd className="font-medium tabular-nums">{num(row[s.key]).toLocaleString()}</dd>
          </div>
        ))}
        {total && (
          <div className="mt-1 flex justify-between gap-4 border-t pt-1">
            <dt className="text-muted-foreground">{total.label}</dt>
            <dd className="font-semibold tabular-nums">{num(row[total.key]).toLocaleString()}</dd>
          </div>
        )}
      </dl>
    </div>
  );
}

const axis = {
  stroke: "var(--od-ink-muted)",
  tick: { fill: "var(--od-ink-muted)", fontSize: 11 },
  tickLine: false,
  axisLine: false,
} as const;

function Frame({
  title,
  label,
  series,
  children,
}: {
  title: string;
  label: string;
  series: Series[];
  children: React.ReactElement;
}) {
  return (
    <figure className="min-w-0">
      <figcaption className="mb-2 flex flex-wrap items-baseline justify-between gap-2">
        <span className="text-sm font-medium">{title}</span>
        <Legend series={series} />
      </figcaption>
      <div role="img" aria-label={label} className="h-56 w-full">
        <ResponsiveContainer width="100%" height="100%">
          {children}
        </ResponsiveContainer>
      </div>
    </figure>
  );
}

/** Messages per day by outcome (stacked columns) and bounces per day (lines). */
export function DailyCharts({ rows }: { rows: DailyRow[] }) {
  const phrase = usePhrase();
  const outcome: Series[] = [
    { key: "submitted", label: phrase("Submitted"), color: "var(--od-series-1)" },
    { key: "failed", label: phrase("Failed"), color: "var(--od-bad-mark)" },
    { key: "other", label: phrase("Other states"), color: "var(--od-muted-mark)" },
  ];
  const bounces: Series[] = [
    { key: "hard_bounces", label: phrase("Hard bounces"), color: "var(--od-bad-mark)" },
    { key: "soft_bounces", label: phrase("Soft bounces"), color: "var(--od-series-5)" },
  ];
  const sum = (k: string) => rows.reduce((a, r) => a + num(r[k]), 0);
  const many = rows.length > 31;
  return (
    <div className="grid gap-6 lg:grid-cols-[3fr_2fr]">
      <Frame
        title={phrase("Messages per day")}
        series={outcome}
        label={`${phrase("Messages per day")}: ${sum("messages")} ${phrase("Messages")}, ${sum("submitted")} ${phrase("Submitted")}, ${sum("failed")} ${phrase("Failed")}`}
      >
        <BarChart data={rows} barCategoryGap={many ? "10%" : "20%"}>
          <CartesianGrid stroke="var(--od-grid)" vertical={false} />
          <XAxis dataKey="day" tickFormatter={shortDay} minTickGap={24} {...axis} />
          <YAxis allowDecimals={false} width={40} {...axis} />
          <Tooltip
            cursor={{ fill: "var(--od-neutral-soft)" }}
            content={
              <ChartTooltip
                series={outcome}
                total={{ key: "messages", label: phrase("Messages") }}
              />
            }
          />
          {outcome.map((s, i) => (
            <Bar
              key={s.key}
              dataKey={s.key}
              name={s.label}
              stackId="outcome"
              fill={s.color}
              stroke="var(--od-surface)"
              strokeWidth={many ? 0 : 1}
              shape={(props: unknown) => {
                const p = props as RectProps;
                return (
                  <Rectangle
                    {...p}
                    radius={topSegment(p.payload, outcome) === i ? [4, 4, 0, 0] : 0}
                  />
                );
              }}
              isAnimationActive={false}
            />
          ))}
        </BarChart>
      </Frame>
      <Frame
        title={phrase("Bounces per day")}
        series={bounces}
        label={`${phrase("Bounces per day")}: ${sum("hard_bounces")} ${phrase("Hard bounces")}, ${sum("soft_bounces")} ${phrase("Soft bounces")}`}
      >
        <LineChart data={rows}>
          <CartesianGrid stroke="var(--od-grid)" vertical={false} />
          <XAxis dataKey="day" tickFormatter={shortDay} minTickGap={24} {...axis} />
          <YAxis allowDecimals={false} width={32} {...axis} />
          <Tooltip
            cursor={{ stroke: "var(--od-ink-muted)", strokeDasharray: "3 3" }}
            content={<ChartTooltip series={bounces} />}
          />
          {bounces.map((s) => (
            <Line
              key={s.key}
              dataKey={s.key}
              name={s.label}
              type="linear"
              stroke={s.color}
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4, stroke: "var(--od-surface)", strokeWidth: 2 }}
              isAnimationActive={false}
            />
          ))}
        </LineChart>
      </Frame>
    </div>
  );
}
