import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Line,
  LineChart,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
export type ChartDatum = { name: string; value: number };
type Props = { data: ChartDatum[]; label?: string };
const colors = [
  "var(--od-series-1)",
  "var(--od-series-2)",
  "var(--od-series-3)",
  "var(--od-series-4)",
  "var(--od-series-5)",
  "var(--od-series-6)",
  "var(--od-series-7)",
  "var(--od-series-8)",
];
function Frame({ children, label }: { children: React.ReactElement; label: string }) {
  return (
    <div role="img" aria-label={label} className="h-64 w-full">
      <ResponsiveContainer width="100%" height="100%">
        {children}
      </ResponsiveContainer>
    </div>
  );
}
export function LineChartView({ data, label = "Line chart" }: Props) {
  return (
    <Frame label={label}>
      <LineChart data={data}>
        <CartesianGrid stroke="var(--od-grid)" strokeDasharray="3 3" />
        <XAxis dataKey="name" stroke="var(--od-ink-muted)" />
        <YAxis stroke="var(--od-ink-muted)" />
        <Tooltip />
        <Line dataKey="value" stroke={colors[0]} />
      </LineChart>
    </Frame>
  );
}
export function BarChartView({ data, label = "Bar chart" }: Props) {
  return (
    <Frame label={label}>
      <BarChart data={data}>
        <CartesianGrid stroke="var(--od-grid)" strokeDasharray="3 3" />
        <XAxis dataKey="name" stroke="var(--od-ink-muted)" />
        <YAxis stroke="var(--od-ink-muted)" />
        <Tooltip />
        <Bar dataKey="value" fill={colors[0]} />
      </BarChart>
    </Frame>
  );
}
export function AreaChartView({ data, label = "Area chart" }: Props) {
  return (
    <Frame label={label}>
      <AreaChart data={data}>
        <XAxis dataKey="name" stroke="var(--od-ink-muted)" />
        <YAxis stroke="var(--od-ink-muted)" />
        <Tooltip />
        <Area
          dataKey="value"
          stroke={colors[0]}
          fill={colors[0]}
          style={{ fillOpacity: "var(--od-area-wash)" }}
        />
      </AreaChart>
    </Frame>
  );
}
export function PieChartView({ data, label = "Pie chart" }: Props) {
  return (
    <Frame label={label}>
      <PieChart>
        <Pie data={data} dataKey="value" nameKey="name" outerRadius="80%">
          {data.map((item, index) => (
            <Cell key={item.name} fill={colors[index % colors.length]} />
          ))}
        </Pie>
        <Tooltip />
      </PieChart>
    </Frame>
  );
}
